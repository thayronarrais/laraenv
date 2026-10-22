package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/paths"
)

func validateMCPExec(kind string, args []string, confirm bool) error {
	if kind != "artisan" && kind != "composer" && kind != "npm" {
		return errors.New("exec action must be artisan, composer or npm")
	}
	if len(args) == 0 || len(args) > 128 {
		return errors.New("args must contain 1..128 literal command arguments")
	}
	total := 0
	for _, arg := range args {
		total += len(arg)
		if strings.ContainsRune(arg, 0) || len(arg) > 8192 || total > 65536 {
			return errors.New("invalid or oversized command arguments")
		}
		lower := strings.ToLower(arg)
		// Composer accepts -dDIR and combined short options such as -vdDIR.
		// npm's -C is a shorthand for --prefix and -g can be combined.
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && len(arg) > 1 {
			short := arg[1:]
			if kind == "composer" && strings.Contains(short, "d") {
				return errors.New("command arguments must keep the selected project scope")
			}
			if kind == "npm" && (strings.Contains(short, "C") || strings.Contains(short, "g")) {
				return errors.New("command arguments must keep the selected project scope")
			}
		}
		if kind == "composer" && strings.HasPrefix(lower, "--") {
			option, _, _ := strings.Cut(lower[2:], "=")
			if option != "" && strings.HasPrefix("working-dir", option) {
				return errors.New("command arguments must keep the selected project scope")
			}
		}
		if kind == "npm" && (strings.HasPrefix(lower, "--location") || strings.HasPrefix(lower, "--global")) {
			return errors.New("command arguments must keep the selected project scope")
		}
		// These options redirect tools outside the selected project's workspace.
		for _, flag := range []string{"--working-dir", "--prefix", "--cwd", "--global", "-g", "-d"} {
			if lower == flag || strings.HasPrefix(lower, flag+"=") {
				return errors.New("command arguments must keep the selected project scope")
			}
		}
	}
	if confirm {
		return nil
	}
	// Scripts can perform destructive work indirectly. Treat all commands other
	// than known inspection commands as requiring explicit confirmation.
	allowed := map[string]bool{}
	switch kind {
	case "artisan":
		for _, s := range []string{"list", "help", "about", "route:list", "event:list", "schedule:list", "migrate:status"} {
			allowed[s] = true
		}
	case "composer":
		for _, s := range []string{"list", "help", "about", "show", "validate", "why", "why-not", "licenses", "outdated"} {
			allowed[s] = true
		}
	case "npm":
		for _, s := range []string{"list", "ls", "help", "view", "outdated", "--version", "-v"} {
			allowed[s] = true
		}
	}
	if !allowed[strings.ToLower(args[0])] {
		return errors.New("confirm:true is required for this command or project script")
	}
	return nil
}

type mcpExecSpec struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
	Stdin      io.Reader
}

func mcpEnvironment(dirs []string) []string {
	env := os.Environ()
	prefix := strings.Join(dirs, string(os.PathListSeparator))
	for i, s := range env {
		if strings.HasPrefix(strings.ToUpper(s), "PATH=") {
			env[i] = "PATH=" + prefix + string(os.PathListSeparator) + s[5:]
			return env
		}
	}
	return append(env, "PATH="+prefix)
}
func (a *App) mcpExecSpec(in mcpbridge.Args) (mcpExecSpec, error) {
	if in.Command != "" {
		return mcpExecSpec{}, errors.New("use a literal args array; shell command strings are not accepted")
	}
	if err := validateMCPExec(in.Action, in.Args, in.Confirm); err != nil {
		return mcpExecSpec{}, err
	}
	p, err := a.mcpProject(in.Project)
	if err != nil {
		return mcpExecSpec{}, err
	}
	if p.Paused {
		return mcpExecSpec{}, errors.New("resume the project before executing commands")
	}
	phpRuntime, nodeRuntime := a.projectRuntimeSelection(p.Name, p.Path)
	if in.Action == "artisan" || in.Action == "composer" {
		if err := runtimeRequirementError(p.Name, "PHP", phpRuntime); err != nil {
			return mcpExecSpec{}, err
		}
	} else if in.Action == "npm" {
		if err := runtimeRequirementError(p.Name, "Node", nodeRuntime); err != nil {
			return mcpExecSpec{}, err
		}
	}
	phpVersion := phpRuntime.Version
	phpDir := ""
	if phpVersion != "" {
		phpDir = a.php.VersionDir(phpVersion)
	}
	nodeDir := ""
	if nodeRuntime.Version != "" {
		nodeDir = a.nodejs.VersionDir(nodeRuntime.Version)
	}
	dirs := []string{}
	if phpDir != "" {
		dirs = append(dirs, phpDir)
	}
	if nodeDir != "" {
		dirs = append(dirs, nodeDir)
	}
	dirs = append(dirs, paths.ComposerRoot())
	spec := mcpExecSpec{Dir: p.Path, Env: mcpEnvironment(dirs)}
	spec.Env, err = a.localProjectTLSEnvironment(p.Name, p.Path, spec.Env)
	if err != nil {
		return spec, err
	}
	switch in.Action {
	case "artisan", "composer":
		spec.Executable = a.php.ExeFor(phpVersion)
		if spec.Executable == "" {
			return spec, errors.New("install/select PHP for this project before executing commands")
		}
		script := paths.ComposerPhar()
		if in.Action == "artisan" {
			script = filepath.Join(p.Path, "artisan")
		}
		if st, e := os.Stat(script); e != nil || st.IsDir() {
			return spec, errors.New("requested project command entry point is unavailable")
		}
		spec.Args = append([]string{script}, in.Args...)
	case "npm":
		if nodeDir == "" {
			return spec, errors.New("install/select Node for this project before executing npm")
		}
		spec.Executable = filepath.Join(nodeDir, "node.exe")
		script := filepath.Join(nodeDir, "node_modules", "npm", "bin", "npm-cli.js")
		if _, e := os.Stat(script); e != nil {
			return spec, errors.New("npm is not installed with the selected Node runtime")
		}
		spec.Args = append([]string{script}, in.Args...)
	}
	if _, err := os.Stat(spec.Executable); err != nil {
		return spec, errors.New("selected runtime executable is unavailable")
	}
	return spec, nil
}

type mcpOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
	progress  func(string)
}

const mcpOutputLimit = 256 * 1024

func (w *mcpOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	remaining := mcpOutputLimit - len(w.data)
	if len(p) > remaining {
		p = p[:remaining]
		w.truncated = true
	}
	w.data = append(w.data, p...)
	if w.progress != nil {
		for len(p) > 0 {
			n := len(p)
			if n > 4096 {
				n = 4096
			}
			w.progress(string(p[:n]))
			p = p[n:]
		}
	}
	return n, nil
}

type MCPExecResult struct {
	Output     string `json:"output"`
	ExitCode   int    `json:"exitCode"`
	Truncated  bool   `json:"truncated"`
	DurationMS int64  `json:"durationMs"`
}

func runMCPExec(ctx context.Context, spec mcpExecSpec, progress func(string)) (MCPExecResult, error) {
	start := time.Now()
	w := &mcpOutput{progress: progress}
	cmd := exec.CommandContext(ctx, spec.Executable, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = spec.Stdin
	cmd.Stdout = w
	cmd.Stderr = w
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = 3 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		killCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		kill := exec.CommandContext(killCtx, "taskkill.exe", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		kill.Stdout = io.Discard
		kill.Stderr = io.Discard
		_ = kill.Run()
		_ = cmd.Process.Kill()
		return nil
	}
	err := cmd.Run()
	w.mu.Lock()
	result := MCPExecResult{Output: string(w.data), ExitCode: 0, Truncated: w.truncated, DurationMS: time.Since(start).Milliseconds()}
	w.mu.Unlock()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			result.ExitCode = exit.ExitCode()
		} else {
			result.ExitCode = -1
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("project command failed; inspect the returned output and exit code")
	}
	return result, nil
}
func (a *App) mcpExec(ctx context.Context, in mcpbridge.Args, progress func(string)) (any, error) {
	spec, err := a.mcpExecSpec(in)
	if err != nil {
		return nil, err
	}
	return runMCPExec(ctx, spec, progress)
}

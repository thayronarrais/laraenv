// Package terminal launches an external terminal window scoped to a project
// folder, with an optional PATH prefix (so the project's PHP / Node bins win
// over the global ones). Supports PowerShell, cmd, Git Bash and Cmder.
package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"lara4ever/internal/i18n"
)

type Kind string

const (
	KindPowerShell Kind = "powershell"
	KindCmd        Kind = "cmd"
	KindGitBash    Kind = "gitbash"
	KindCmder      Kind = "cmder"
)

type Profile struct {
	Kind      Kind   `json:"kind"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
	Path      string `json:"path"`
}

// Detect probes the well-known install locations for each terminal and reports
// availability. cmderRoot is the directory we install Cmder into; pass "" to
// skip Cmder detection.
func Detect(cmderRoot string) []Profile {
	profs := []Profile{
		{Kind: KindPowerShell, Label: "PowerShell"},
		{Kind: KindCmd, Label: "Command Prompt"},
		{Kind: KindGitBash, Label: "Git Bash"},
		{Kind: KindCmder, Label: "Cmder"},
	}
	for i := range profs {
		if p, ok := locate(profs[i].Kind, cmderRoot); ok {
			profs[i].Available = true
			profs[i].Path = p
		}
	}
	return profs
}

func locate(k Kind, cmderRoot string) (string, bool) {
	switch k {
	case KindPowerShell:
		if p, err := exec.LookPath("powershell.exe"); err == nil {
			return p, true
		}
		if sys := os.Getenv("SystemRoot"); sys != "" {
			p := filepath.Join(sys, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
			if _, err := os.Stat(p); err == nil {
				return p, true
			}
		}
	case KindCmd:
		if p, err := exec.LookPath("cmd.exe"); err == nil {
			return p, true
		}
		if sys := os.Getenv("SystemRoot"); sys != "" {
			p := filepath.Join(sys, "System32", "cmd.exe")
			if _, err := os.Stat(p); err == nil {
				return p, true
			}
		}
	case KindGitBash:
		candidates := []string{
			`C:\Program Files\Git\bin\bash.exe`,
			`C:\Program Files (x86)\Git\bin\bash.exe`,
			filepath.Join(os.Getenv("LOCALAPPDATA"), `Programs\Git\bin\bash.exe`),
			filepath.Join(os.Getenv("ProgramW6432"), `Git\bin\bash.exe`),
		}
		for _, c := range candidates {
			if c == "" || c == `\bin\bash.exe` {
				continue
			}
			if _, err := os.Stat(c); err == nil {
				return c, true
			}
		}
		// Fall back to git in PATH and derive ../bin/bash.exe.
		if p, err := exec.LookPath("git.exe"); err == nil {
			bash := filepath.Join(filepath.Dir(filepath.Dir(p)), "bin", "bash.exe")
			if _, err := os.Stat(bash); err == nil {
				return bash, true
			}
		}
	case KindCmder:
		if cmderRoot == "" {
			return "", false
		}
		p := filepath.Join(cmderRoot, "Cmder.exe")
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// Open launches the chosen terminal at projectDir. pathPrefix entries are
// prepended to PATH (e.g. the project's PHP and the active Node). The banner
// text is purely cosmetic — shown once on the first line of the new window.
func Open(p Profile, projectDir, projectName, banner string, pathPrefix []string, environmentResolvers ...func(string, []string) ([]string, error)) error {
	return OpenWithCommand(p, projectDir, projectName, banner, "", pathPrefix, environmentResolvers...)
}

// OpenWithCommand is Open plus an optional initial command run once in the new
// window, after which the shell stays open (so the user sees output and can keep
// typing). When initialCmd is empty it behaves exactly like Open.
func OpenWithCommand(p Profile, projectDir, projectName, banner, initialCmd string, pathPrefix []string, environmentResolvers ...func(string, []string) ([]string, error)) error {
	if !p.Available || p.Path == "" {
		return i18n.Errorf("errors.terminal.notAvailable", map[string]any{"kind": p.Kind})
	}
	title := "lara4ever - " + projectName
	env := envWithPathPrefix(os.Environ(), pathPrefix)
	if len(environmentResolvers) > 0 && environmentResolvers[0] != nil {
		var err error
		env, err = environmentResolvers[0](projectDir, env)
		if err != nil {
			return err
		}
	}

	var args []string
	switch p.Kind {
	case KindPowerShell:
		// Banner is wrapped in single quotes inside a -Command string. We
		// double up any literal single quotes in the project name. The user
		// command (already a valid command line) is appended raw after a ";".
		ps := fmt.Sprintf("Write-Host '%s' -ForegroundColor Cyan", escapePsSingle(banner))
		if initialCmd != "" {
			ps += "; " + initialCmd
		}
		args = []string{"/C", "start", title, "/D", projectDir, p.Path, "-NoExit", "-NoLogo", "-Command", ps}
	case KindCmd:
		kcmd := "echo " + banner
		if initialCmd != "" {
			kcmd += " & " + initialCmd
		}
		args = []string{"/C", "start", title, "/D", projectDir, p.Path, "/K", kcmd}
	case KindGitBash:
		if initialCmd != "" {
			// Run the command, then drop into an interactive shell so the
			// window stays open with the command's output visible.
			args = []string{"/C", "start", title, "/D", projectDir, p.Path, "--login", "-i", "-c", initialCmd + "; exec bash"}
		} else {
			args = []string{"/C", "start", title, "/D", projectDir, p.Path, "--login", "-i"}
		}
	case KindCmder:
		// Cmder's launcher: /start sets initial directory; omit /single so
		// each click opens a fresh window instead of stealing tabs. Cmder has
		// no clean "run a command then stay open" launcher flag, so an
		// initialCmd is not supported here — we just open it at the dir.
		args = []string{"/C", "start", title, p.Path, "/start", projectDir}
	default:
		return i18n.Errorf("errors.terminal.unsupported", map[string]any{"kind": p.Kind})
	}

	cmd := exec.Command("cmd.exe", args...)
	cmd.Dir = projectDir
	cmd.Env = env
	// Hide the intermediary cmd.exe window — only the new terminal should
	// be visible to the user. The `start` builtin detaches the real child
	// into its own console.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func escapePsSingle(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func envWithPathPrefix(env []string, prefix []string) []string {
	if len(prefix) == 0 {
		return env
	}
	sep := string(os.PathListSeparator)
	pre := strings.Join(prefix, sep)
	out := make([]string, 0, len(env))
	replaced := false
	for _, e := range env {
		if eq := strings.IndexByte(e, '='); eq > 0 && strings.EqualFold(e[:eq], "PATH") {
			out = append(out, e[:eq+1]+pre+sep+e[eq+1:])
			replaced = true
			continue
		}
		out = append(out, e)
	}
	if !replaced {
		out = append(out, "PATH="+pre)
	}
	return out
}

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lara4ever/internal/config"
	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/nodejs"
	"lara4ever/internal/paths"
	"lara4ever/internal/php"
	"lara4ever/internal/projects"
)

func TestMCPExecSelectsProjectRuntimesWithoutShell(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "projects")
	os.MkdirAll(filepath.Join(projectRoot, "demo"), 0700)
	os.WriteFile(filepath.Join(projectRoot, "demo", "artisan"), []byte("<?php"), 0600)
	phpRoot := filepath.Join(root, "php")
	nodeRoot := filepath.Join(root, "node")
	phpDir := filepath.Join(phpRoot, "8.3")
	nodeDir := filepath.Join(nodeRoot, "22.0.0")
	os.MkdirAll(phpDir, 0700)
	os.MkdirAll(filepath.Join(nodeDir, "node_modules", "npm", "bin"), 0700)
	os.WriteFile(filepath.Join(phpDir, "php.exe"), []byte("fake"), 0600)
	os.WriteFile(filepath.Join(phpDir, "php.ini"), []byte(""), 0600)
	os.WriteFile(filepath.Join(nodeDir, "node.exe"), []byte("fake"), 0600)
	os.WriteFile(filepath.Join(nodeDir, "node_modules", "npm", "bin", "npm-cli.js"), []byte("fake"), 0600)
	cfg, e := config.NewStore(filepath.Join(root, "config.json"))
	if e != nil {
		t.Fatal(e)
	}
	p := cfg.Get().Projects
	p.Root = projectRoot
	cfg.SetProjects(p)
	cfg.SetProjectPhp("demo", "8.3")
	cfg.SetProjectNode("demo", "22.0.0")
	a := &App{cfg: cfg, projects: projects.NewScanner(cfg), php: php.NewManager(phpRoot), nodejs: nodejs.NewManager(nodeRoot)}
	in := mcpbridge.Args{Action: "npm", Project: "demo", Args: []string{"run", "build", "--", "literal & value"}, Confirm: true}
	s, e := a.mcpExecSpec(in)
	if os.Getenv("LARAENV_MCP_TLS_FIXTURE") == "1" {
		if e == nil || !strings.Contains(e.Error(), "local public CA") {
			t.Fatalf("MCP exec bypassed TLS validation: %v", e)
		}
		return
	}
	if e != nil {
		t.Fatal(e)
	}
	if s.Executable != filepath.Join(nodeDir, "node.exe") || len(s.Args) == 0 || s.Args[len(s.Args)-1] != "literal & value" {
		t.Fatalf("incorrect invocation: executable=%q dir=%q args=%q", s.Executable, s.Dir, s.Args)
	}
	assertSameFilesystemPath(t, s.Dir, filepath.Join(projectRoot, "demo"))
	pathOK := false
	for _, kv := range s.Env {
		if strings.HasPrefix(strings.ToUpper(kv), "PATH=") {
			pathOK = strings.HasPrefix(kv[5:], phpDir+string(os.PathListSeparator)+nodeDir)
		}
	}
	if !pathOK {
		t.Fatal("PATH does not use project runtimes")
	}
	in.Action = "artisan"
	in.Args = []string{"route:list"}
	s, e = a.mcpExecSpec(in)
	if e != nil || s.Executable != filepath.Join(phpDir, "php.exe") || len(s.Args) == 0 {
		t.Fatalf("artisan invocation: executable=%q dir=%q args=%q error=%v", s.Executable, s.Dir, s.Args, e)
	}
	assertSameFilesystemPath(t, s.Args[0], filepath.Join(projectRoot, "demo", "artisan"))
}

func TestMCPExecUsesLocalTLSResolver(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nginx", "certs", "lara4ever-rootCA.crt")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid CA fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestMCPExecSelectsProjectRuntimesWithoutShell$")
	cmd.Env = append(os.Environ(), paths.EnvRoot+"="+root, "LARAENV_MCP_TLS_FIXTURE=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("MCP TLS integration fixture: %v\n%s", err, output)
	}
}

func TestMCPExecHelper(t *testing.T) {
	switch os.Getenv("LARAENV_MCP_EXEC_TEST") {
	case "stdin":
		_, _ = io.Copy(os.Stdout, os.Stdin)
		os.Exit(0)
	case "output":
		os.Stdout.WriteString(strings.Repeat("a", mcpOutputLimit+100))
		os.Stderr.WriteString("stderr")
		os.Exit(7)
	case "wait":
		os.Stdout.WriteString("started\n")
		time.Sleep(20 * time.Second)
		os.Exit(0)
	}
}
func TestMCPExecSuppliesPrivateInputOverStdin(t *testing.T) {
	exe, _ := os.Executable()
	spec := mcpExecSpec{Executable: exe, Args: []string{"-test.run=^TestMCPExecHelper$"}, Dir: t.TempDir(), Env: append(os.Environ(), "LARAENV_MCP_EXEC_TEST=stdin"), Stdin: strings.NewReader("literal stdin fixture")}
	result, err := runMCPExec(context.Background(), spec, nil)
	if err != nil || result.Output != "literal stdin fixture" {
		t.Fatalf("stdin was lost: %+v %v", result, err)
	}
}
func TestMCPExecStreamsBoundedOutputAndReturnsExitCode(t *testing.T) {
	exe, _ := os.Executable()
	var streamed strings.Builder
	s := mcpExecSpec{Executable: exe, Args: []string{"-test.run=^TestMCPExecHelper$"}, Dir: t.TempDir(), Env: append(os.Environ(), "LARAENV_MCP_EXEC_TEST=output")}
	out, e := runMCPExec(context.Background(), s, func(chunk string) { streamed.WriteString(chunk) })
	if e == nil || out.ExitCode != 7 || !out.Truncated || len(out.Output) != mcpOutputLimit || streamed.String() != out.Output {
		t.Fatalf("exit=%d err=%v truncated=%v output=%d streamed=%d", out.ExitCode, e, out.Truncated, len(out.Output), streamed.Len())
	}
}
func TestMCPExecCancellationEndsProcess(t *testing.T) {
	exe, _ := os.Executable()
	s := mcpExecSpec{Executable: exe, Args: []string{"-test.run=^TestMCPExecHelper$"}, Dir: t.TempDir(), Env: append(os.Environ(), "LARAENV_MCP_EXEC_TEST=wait")}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, e := runMCPExec(ctx, s, nil)
	if e == nil || ctx.Err() == nil || time.Since(start) > 5*time.Second || !strings.Contains(out.Output, "started") {
		t.Fatalf("cancel out=%+v err=%v took=%v", out, e, time.Since(start))
	}
}

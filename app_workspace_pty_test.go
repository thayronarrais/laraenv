package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/config"
	"lara4ever/internal/terminal"
	"lara4ever/internal/workspacefiles"
)

func TestProjectCommandPtyLeavesTerminalPreferencesUnchanged(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "demo")
	if err := os.Mkdir(projectPath, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	store, err := config.NewStore(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetProjectTerminal("demo", "gitbash"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	id, err := startProjectPty("demo", "cmd", root, 90, 30,
		func(name, kind, cwd string, cols, rows int) (string, error) {
			if name != "demo" || kind != "cmd" || cwd != projectPath || cols != 90 || rows != 30 {
				t.Fatalf("unexpected shell launch: %q %q %q %d %d", name, kind, cwd, cols, rows)
			}
			return "command-shell", nil
		}, nil)
	if err != nil || id != "command-shell" {
		t.Fatalf("command terminal id=%q err=%v", id, err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || store.Get().Projects.Terminals["demo"] != "gitbash" {
		t.Fatal("running a project command changed saved terminal preferences")
	}
}

func TestExplicitProjectTerminalSelectionStillPersistsAfterSuccessfulLaunch(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "demo"), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetProjectTerminal("demo", "gitbash"); err != nil {
		t.Fatal(err)
	}
	spawnFailure := errors.New("PTY unavailable")
	if _, err := startProjectPty("demo", "cmd", root, 80, 24,
		func(string, string, string, int, int) (string, error) { return "", spawnFailure }, store.SetProjectTerminal); !errors.Is(err, spawnFailure) {
		t.Fatalf("failed launch: %v", err)
	}
	if got := store.Get().Projects.Terminals["demo"]; got != "gitbash" {
		t.Fatalf("failed launch changed terminal preference to %q", got)
	}
	if _, err := startProjectPty("demo", "cmd", root, 80, 24,
		func(string, string, string, int, int) (string, error) { return "selected-shell", nil }, store.SetProjectTerminal); err != nil {
		t.Fatal(err)
	}
	reloaded, err := config.NewStore(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get().Projects.Terminals["demo"]; got != "cmd" {
		t.Fatalf("explicit shell selection was not persisted: %q", got)
	}
}

func TestAgentWorkspacePtyUsesEffectivePathAndRuntime(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	worktree := filepath.Join(root, "worktree")
	for _, path := range []string{main, worktree} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	session := pkgagents.AgentSession{ID: "a1", ProjectName: "demo", ProjectPath: worktree, WorktreePath: worktree}
	service := workspacefiles.NewService(func(ref workspacefiles.ContextRef) (string, string, error) {
		return session.ProjectPath, session.ProjectName, nil
	})
	var gotCwd, gotCommand string
	var gotPath []string
	spawn := func(cmd, cwd string, path []string, cols, rows int) (string, error) {
		gotCommand, gotCwd, gotPath = cmd, cwd, path
		return "shell-1", nil
	}
	result, err := startAgentWorkspacePty("a1", "cmd", 80, 24, "", service.Context,
		func(id string) (pkgagents.AgentSession, bool) { return session, true },
		func(name, kind, cwd string, cols, rows int) (string, error) {
			return startProjectShell(name, kind, cwd, cols, rows,
				func(string, string) (terminal.Profile, error) {
					return terminal.Profile{Kind: terminal.KindCmd, Path: "cmd.exe"}, nil
				},
				func(string) string { return "php-demo" }, func(string) string { return "node-demo" }, spawn)
		})
	if err != nil {
		t.Fatal(err)
	}
	if result.TermID != "shell-1" || result.RootKey == "" || result.Label != worktree {
		t.Fatalf("result=%+v", result)
	}
	if gotCwd != worktree || gotCommand != "cmd.exe" || strings.Join(gotPath, ",") != "php-demo,node-demo" {
		t.Fatalf("shell cwd=%q cmd=%q path=%v", gotCwd, gotCommand, gotPath)
	}
}

func TestAgentWorkspacePtyRejectsChangedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "worktree")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	session := pkgagents.AgentSession{ID: "a1", ProjectName: "demo", ProjectPath: root}
	service := workspacefiles.NewService(func(ref workspacefiles.ContextRef) (string, string, error) {
		return session.ProjectPath, session.ProjectName, nil
	})
	key := service.Context(workspacefiles.ContextRef{Kind: "agent", SessionID: "a1"}).Context.RootKey
	spawnCount := 0
	spawn := func(string, string, string, int, int) (string, error) { spawnCount++; return "shell", nil }
	get := func(id string) (pkgagents.AgentSession, bool) { return session, id == "a1" }
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if _, err := startAgentWorkspacePty("a1", "cmd", 80, 24, key, service.Context, get, spawn); err == nil {
		t.Fatal("deleted root accepted")
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := startAgentWorkspacePty("a1", "cmd", 80, 24, key, service.Context, get, spawn); err == nil {
		t.Fatal("recreated root accepted")
	}
	if _, err := startAgentWorkspacePty("missing", "cmd", 80, 24, "", service.Context, get, spawn); err == nil {
		t.Fatal("missing session accepted")
	}
	if spawnCount != 0 {
		t.Fatalf("started %d shells", spawnCount)
	}
}

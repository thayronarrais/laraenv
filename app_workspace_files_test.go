package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/projects"
	"lara4ever/internal/workspacefiles"
)

func TestWorkspaceDirectoryFacadeStaleCursor(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 501; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%03d.txt", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{workspaceFiles: workspacefiles.NewService(func(workspacefiles.ContextRef) (string, string, error) { return root, "fixture", nil })}
	ref := workspacefiles.ContextRef{Kind: "project", ProjectName: "fixture"}
	context := app.GetWorkspaceFileContext(ref)
	if context.Code != "ok" {
		t.Fatalf("context: %#v", context)
	}
	first := app.ListWorkspaceDirectory(ref, context.Context.RootKey, "", "")
	if first.Code != "ok" || first.Directory == nil || len(first.Directory.Entries) != 500 || first.Directory.NextCursor == "" {
		t.Fatalf("first: %#v", first)
	}
	before, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	changed := before.ModTime().Add(-10 * time.Second)
	if err := os.Chtimes(root, changed, changed); err != nil {
		t.Fatal(err)
	}
	second := app.ListWorkspaceDirectory(ref, context.Context.RootKey, "", first.Directory.NextCursor)
	raw, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native facade JSON: %s", raw)
	if second.Code != "stale_cursor" || second.Directory != nil {
		t.Fatalf("stale continuation: %s", raw)
	}
	if got := app.ListWorkspaceDirectory(ref, context.Context.RootKey, "", "not-a-cursor"); got.Code != "invalid_path" {
		t.Fatalf("malformed: %#v", got)
	}
	if got := app.ListWorkspaceDirectory(ref, context.Context.RootKey, "", ""); got.Code != "ok" || len(got.Directory.Entries) != 500 {
		t.Fatalf("restart: %#v", got)
	}
}

func TestWorkspaceContextOnlyKnownSources(t *testing.T) {
	base := t.TempDir()
	projectPath := filepath.Join(base, "project")
	agentPath := filepath.Join(base, "worktree")
	for _, path := range []string{projectPath, agentPath} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(projectPath, "README.md"), []byte("main"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentPath, "README.md"), []byte("agent"), 0600); err != nil {
		t.Fatal(err)
	}
	resolve := newWorkspaceResolveSource(
		func() ([]projects.Project, error) { return []projects.Project{{Name: "demo", Path: projectPath}}, nil },
		func(id string) (pkgagents.AgentSession, bool) {
			if id == "live" {
				return pkgagents.AgentSession{ID: "live", ProjectName: "demo", ProjectPath: agentPath}, true
			}
			return pkgagents.AgentSession{}, false
		},
	)
	app := &App{workspaceFiles: workspacefiles.NewService(resolve)}
	project := app.GetWorkspaceFileContext(workspacefiles.ContextRef{Kind: "project", ProjectName: "demo"})
	agent := app.GetWorkspaceFileContext(workspacefiles.ContextRef{Kind: "agent", SessionID: "live"})
	if project.Code != "ok" || agent.Code != "ok" || project.Context.RootKey == agent.Context.RootKey {
		t.Fatalf("contexts project=%#v agent=%#v", project, agent)
	}
	read := app.ReadWorkspaceDocument(workspacefiles.ContextRef{Kind: "agent", SessionID: "live"}, agent.Context.RootKey, "README.md")
	if read.Code != "ok" || read.Document == nil || read.Document.Text != "agent" {
		t.Fatalf("agent read=%#v", read)
	}
	if got := app.GetWorkspaceFileContext(workspacefiles.ContextRef{Kind: "agent", SessionID: "missing"}); got.Code != "unavailable" {
		t.Fatalf("missing agent=%#v", got)
	}
	if got := app.GetWorkspaceFileContext(workspacefiles.ContextRef{Kind: "project", ProjectName: agentPath}); got.Code != "unavailable" {
		t.Fatalf("arbitrary path became project=%#v", got)
	}
	if got := app.GetWorkspaceFileContext(workspacefiles.ContextRef{Kind: "project", ProjectName: "demo", SessionID: "live"}); got.Code != "invalid_path" {
		t.Fatalf("ambiguous source=%#v", got)
	}
}

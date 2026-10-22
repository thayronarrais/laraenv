package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lara4ever/internal/applicationcontext"
	"lara4ever/internal/config"
	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/projects"
)

func contextAppFixture(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "demo")
	if err := os.MkdirAll(filepath.Join(path, "storage", "logs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "artisan"), []byte("<?php"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Get().Projects
	settings.Root = root
	if err := store.SetProjects(settings); err != nil {
		t.Fatal(err)
	}
	return &App{cfg: store, projects: projects.NewScanner(store)}, path
}
func TestMCPContextSummaryReportsUnavailableWithoutRunningProject(t *testing.T) {
	a, path := contextAppFixture(t)
	if err := os.WriteFile(filepath.Join(path, "storage", "logs", "laravel-2026-10-19.log"), []byte("[2026-10-19 12:00:00] local.ERROR: Broken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	value, err := (appMCPBackend{a}).Call(context.Background(), "context", mcpbridge.Args{Action: "summary", Project: "demo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := value.(MCPApplicationContext)
	if got.Logs.Status != "available" || len(got.Logs.Errors) != 1 || got.Capture.Status != "unavailable" || got.Mail.Status != "unavailable" {
		t.Fatalf("%+v", got)
	}
	if _, err = os.Stat(filepath.Join(path, "app")); !os.IsNotExist(err) {
		t.Fatal("summary installed integration")
	}
	for _, name := range []string{"../demo", "missing"} {
		if _, err = (appMCPBackend{a}).Call(context.Background(), "context", mcpbridge.Args{Action: "summary", Project: name}, nil); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
}
func TestMCPContextWritesNeedConfirmationAndPreserveUserSource(t *testing.T) {
	a, path := contextAppFixture(t)
	for _, action := range []string{"install_integration", "write_guidelines"} {
		if _, err := (appMCPBackend{a}).Call(context.Background(), "context", mcpbridge.Args{Action: action, Project: "demo"}, nil); err == nil {
			t.Errorf("accepted unconfirmed %s", action)
		}
		if _, err := (appMCPBackend{a}).Call(context.Background(), "context", mcpbridge.Args{Action: action, Project: "demo", Confirm: true}, nil); err != nil {
			t.Fatal(err)
		}
	}
	got, err := applicationcontext.ReadCapture(context.Background(), path, "demo", 10)
	if err != nil || got.Status != "unavailable" {
		t.Fatalf("installation claimed live data: %+v %v", got, err)
	}
}

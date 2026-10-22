package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"lara4ever/internal/config"
	"lara4ever/internal/projects"
)

func projectScriptsApp(t *testing.T, manifest string) (*App, string) {
	t.Helper()
	root := t.TempDir()
	projectRoot := filepath.Join(root, "projects")
	projectPath := filepath.Join(projectRoot, "demo")
	if err := os.MkdirAll(projectPath, 0700); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(projectPath, "package.json"), []byte(manifest), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := config.NewStore(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	prefs := store.Get().Projects
	prefs.Root = projectRoot
	if err := store.SetProjects(prefs); err != nil {
		t.Fatal(err)
	}
	return &App{cfg: store, projects: projects.NewScanner(store)}, projectPath
}

func TestListProjectScriptsReadsActualSortedScriptsWithoutArtisan(t *testing.T) {
	app, projectPath := projectScriptsApp(t, `{"scripts":{"watch":"vite build --watch","build":"vite build","dev":"concurrently server client","start":"node server.js"}}`)
	// A Laravel checkout must remain metadata-only: no installed PHP is needed.
	if err := os.WriteFile(filepath.Join(projectPath, "artisan"), []byte("<?php exit(1);"), 0600); err != nil {
		t.Fatal(err)
	}
	scripts, err := app.ListProjectScripts("demo")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"build", "dev", "start", "watch"}; !reflect.DeepEqual(scripts, want) {
		t.Fatalf("scripts=%q, want %q", scripts, want)
	}
	if _, err := os.Stat(filepath.Join(projectPath, "node_modules")); !os.IsNotExist(err) {
		t.Fatal("script discovery must not install dependencies")
	}
}

func TestListProjectScriptsMissingManifestAndEmptyScriptsReturnEmptyArray(t *testing.T) {
	for _, manifest := range []string{"", `{}`, `{"scripts":{}}`} {
		app, _ := projectScriptsApp(t, manifest)
		scripts, err := app.ListProjectScripts("demo")
		if err != nil || scripts == nil || len(scripts) != 0 {
			t.Errorf("manifest=%q scripts=%v err=%v", manifest, scripts, err)
		}
	}
}

func TestListProjectScriptsRejectsInvalidManifestAndUnknownProject(t *testing.T) {
	for _, manifest := range []string{`{"scripts":`, `{"scripts":{"dev":42}}`} {
		app, _ := projectScriptsApp(t, manifest)
		if scripts, err := app.ListProjectScripts("demo"); err == nil {
			t.Errorf("invalid manifest %q returned scripts %q", manifest, scripts)
		}
	}
	app, _ := projectScriptsApp(t, `{"scripts":{"dev":"vite"}}`)
	for _, name := range []string{"", ".", "..", "../demo", `..\demo`, `C:\projects\demo`, "missing"} {
		if scripts, err := app.ListProjectScripts(name); err == nil {
			t.Errorf("invalid project %q returned scripts %q", name, scripts)
		}
	}
}

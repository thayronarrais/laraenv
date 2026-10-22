package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/config"
	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/nodejs"
	"lara4ever/internal/php"
	"lara4ever/internal/projects"
	"lara4ever/internal/services"
	"lara4ever/internal/vhost"
)

func TestMCPExecDoesNotFallbackForMissingDeclaredPHP(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "projects")
	projectDir := filepath.Join(projectRoot, "demo")
	phpRoot := filepath.Join(root, "php")
	phpDir := filepath.Join(phpRoot, "8.4")
	for _, dir := range []string{projectDir, phpDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for file, contents := range map[string]string{
		filepath.Join(projectDir, "artisan"):       "<?php",
		filepath.Join(projectDir, "composer.json"): `{"require":{"php":"^99.0"}}`,
		filepath.Join(phpDir, "php.exe"):           "fake executable",
		filepath.Join(phpDir, "php.ini"):           "",
	} {
		if err := os.WriteFile(file, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.NewStore(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	prefs := cfg.Get().Projects
	prefs.Root = projectRoot
	if err := cfg.SetProjects(prefs); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: cfg, projects: projects.NewScanner(cfg), php: php.NewManager(phpRoot), nodejs: nodejs.NewManager(filepath.Join(root, "node"))}
	if spec, err := app.mcpExecSpec(mcpbridge.Args{Project: "demo", Action: "artisan", Args: []string{"about"}}); err == nil {
		t.Fatalf("missing declared PHP silently selected %q", spec.Executable)
	}
}

func TestProjectRuntimeSelectionUsesCheckoutManifestAndPreservesManualPreferences(t *testing.T) {
	main, worktree := filepath.Join(t.TempDir(), "main"), filepath.Join(t.TempDir(), "worktree")
	for path, contents := range map[string]string{main: `{"require":{"php":"8.3.*"}}`, worktree: `{"require":{"php":"8.4.*"}}`} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "composer.json"), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	prefs := config.Projects{PhpVersions: map[string]string{}, NodeVersions: map[string]string{}}
	inventory := runtimeSelectionInventory{PHP: []projects.RuntimeCandidate{{Version: "8.3", FullVersion: "8.3.15"}, {Version: "8.4", FullVersion: "8.4.2"}}, DefaultPHP: "8.3"}
	mainPHP, _ := resolveRuntimeSelection("demo", main, prefs, inventory)
	worktreePHP, _ := resolveRuntimeSelection("demo", worktree, prefs, inventory)
	if mainPHP.Version != "8.3" || worktreePHP.Version != "8.4" {
		t.Fatalf("main=%+v worktree=%+v", mainPHP, worktreePHP)
	}
	if len(prefs.PhpVersions) != 0 {
		t.Fatal("detection persisted as a manual override")
	}
	prefs.PhpVersions["demo"] = "8.3"
	manual, _ := resolveRuntimeSelection("demo", worktree, prefs, inventory)
	if manual.Version != "8.3" || manual.Source != "manual" || manual.Status != "incompatible" {
		t.Fatalf("manual choice = %+v", manual)
	}
}

func TestRuntimeServingChangeIgnoresNodeAndPackageManagerChanges(t *testing.T) {
	before := projectRuntimeSnapshot{"checkout": {Serving: true, PHP: projects.RuntimeResolution{Version: "8.3"}, Node: projects.RuntimeResolution{Version: "22"}, Detection: projects.Detection{Kind: projects.KindLaravel, DocRoot: "public", PackageManager: "npm"}}}
	after := projectRuntimeSnapshot{"checkout": {Serving: true, PHP: projects.RuntimeResolution{Version: "8.3"}, Node: projects.RuntimeResolution{Version: "24"}, Detection: projects.Detection{Kind: projects.KindLaravel, DocRoot: "public", PackageManager: "pnpm"}}}
	if runtimeServingChanged(before, after) {
		t.Fatal("Node/package manager edits would restart PHP serving")
	}
	current := after["checkout"]
	current.PHP.Version = "8.4"
	after["checkout"] = current
	if !runtimeServingChanged(before, after) {
		t.Fatal("PHP pool change would leave old serving config")
	}
	after["unserved-worktree"] = runtimeCheckoutSnapshot{PHP: projects.RuntimeResolution{Version: "8.1"}}
	current.PHP.Version = "8.3"
	after["checkout"] = current
	if runtimeServingChanged(before, after) {
		t.Fatal("unserved agent worktree would restart web server")
	}
}

func TestBackgroundCronBlocksMissingRequirementInActualWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	worktree := filepath.Join(root, "worktree")
	if err := os.Mkdir(worktree, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".nvmrc"), []byte("99"), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	document := map[string]any{"projects": map[string]any{"root": filepath.Join(root, "projects")}, "crons": []map[string]any{{"id": "test-runtime", "projectName": "demo", "workingDir": worktree, "schedule": "* * * * *", "enabled": true, "backgroundMode": true, "command": "echo wrong > should-not-run.txt"}}}
	data, _ := json.Marshal(document)
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := runProjectCron(configPath, "test-runtime"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(worktree, "should-not-run.txt")); !os.IsNotExist(err) {
		t.Fatal("background cron executed with missing declared Node")
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "missing_runtime") {
		t.Fatalf("background cron omitted runtime diagnostic: %s", updated)
	}
}

func TestRuntimeApplyRetriesFailedServingWithoutChangingDetection(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "projects")
	if err := os.MkdirAll(filepath.Join(projectRoot, "demo"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	prefs := cfg.Get().Projects
	prefs.Root = projectRoot
	if err := cfg.SetProjects(prefs); err != nil {
		t.Fatal(err)
	}
	blockedSites := filepath.Join(root, "sites")
	if err := os.WriteFile(blockedSites, []byte("blocked directory"), 0600); err != nil {
		t.Fatal(err)
	}
	hosts := filepath.Join(root, "hosts")
	if err := os.WriteFile(hosts, []byte("127.0.0.1 localhost\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: cfg, projects: projects.NewScanner(cfg), php: php.NewManager(filepath.Join(root, "php")), nodejs: nodejs.NewManager(filepath.Join(root, "node")), services: services.NewManager(cfg), nginx: vhost.NewNginxConfigurator(blockedSites), apache: vhost.NewApacheConfigurator(filepath.Join(root, "apache")), caddy: vhost.NewCaddyConfigurator(filepath.Join(root, "caddy")), hosts: vhost.NewHostsEditorAt(hosts)}
	app.rememberProjectRuntimes()
	if len(app.projectRuntimeApplied) != 0 {
		t.Fatal("initial detection falsely marked serving applied")
	}
	app.projectRuntimeChanged("demo")
	if len(app.projectRuntimeApplied) != 0 {
		t.Fatal("failed config write falsely marked serving applied")
	}
	if err := os.Remove(blockedSites); err != nil {
		t.Fatal(err)
	}
	app.projectRuntimeChanged("demo")
	if _, err := os.Stat(app.nginx.VhostPath("demo")); err != nil {
		t.Fatalf("identical detection did not retry failed apply: %v", err)
	}
	if len(app.projectRuntimeApplied) != 1 {
		t.Fatal("successful retry not recorded")
	}
	if len(app.services.RunningNames()) != 0 {
		t.Fatal("manifest detection started stopped services")
	}
}

func TestNodeLTSMetadataRequiresPromotedRelease(t *testing.T) {
	for _, tc := range []struct {
		version, firstLTS string
		want              bool
	}{
		{"22.1.0", "22.11.0", false}, {"22.10.9", "22.11.0", false}, {"22.11.0", "22.11.0", true}, {"v22.22.2", "22.11.0", true},
		{"24.1.0", "24.11.0", false}, {"24.11.0", "24.11.0", true}, {"24.11.0-beta.1", "24.11.0", false}, {"25.0.0", "24.11.0", false}, {"24", "24.11.0", false},
	} {
		if got := nodeLTSVersion(tc.version, tc.firstLTS); got != tc.want {
			t.Errorf("LTS %s from %s = %v; want%v", tc.version, tc.firstLTS, got, tc.want)
		}
	}
}

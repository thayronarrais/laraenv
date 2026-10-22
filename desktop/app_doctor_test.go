package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/config"
	"lara4ever/internal/nodejs"
	"lara4ever/internal/php"
	"lara4ever/internal/projectroutes"
	"lara4ever/internal/projects"
	"lara4ever/internal/sitedoctor"
	"lara4ever/internal/vhost"
)

func doctorAppFixture(t *testing.T) (*App, string, string, string) {
	t.Helper()
	root := t.TempDir()
	projectRoot := filepath.Join(root, "projects")
	projectDir := filepath.Join(projectRoot, "demo")
	phpDir := filepath.Join(root, "php", "8.3")
	nodeDir := filepath.Join(root, "node", "22.0.0")
	for _, dir := range []string{projectDir, phpDir, filepath.Join(nodeDir, "node_modules", "npm", "bin")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for file, content := range map[string]string{
		filepath.Join(projectDir, "artisan"):                               "<?php",
		filepath.Join(projectDir, ".env.example"):                          "APP_KEY=\nDB_CONNECTION=sqlite\n",
		filepath.Join(projectDir, "composer.json"):                         `{"require":{"laravel/framework":"^12.0"}}`,
		filepath.Join(projectDir, "package.json"):                          `{"packageManager":"npm@10.0.0"}`,
		filepath.Join(phpDir, "php.exe"):                                   "fixture",
		filepath.Join(nodeDir, "node.exe"):                                 "fixture",
		filepath.Join(nodeDir, "node_modules", "npm", "bin", "npm-cli.js"): "fixture",
		filepath.Join(root, "hosts"):                                       "127.0.0.1 localhost\n",
	} {
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := config.NewStore(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	prefs := store.Get().Projects
	prefs.Root, prefs.TLD = projectRoot, "test"
	if err := store.SetProjects(prefs); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProjectPhp("demo", "8.3"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProjectNode("demo", "22.0.0"); err != nil {
		t.Fatal(err)
	}
	return &App{cfg: store, projects: projects.NewScanner(store), php: php.NewManager(filepath.Dir(phpDir)), nodejs: nodejs.NewManager(filepath.Dir(nodeDir)),
		nginx: vhost.NewNginxConfigurator(filepath.Join(root, "sites")), apache: vhost.NewApacheConfigurator(filepath.Join(root, "apache-sites")), caddy: vhost.NewCaddyConfigurator(filepath.Join(root, "caddy-sites")), hosts: vhost.NewHostsEditorAt(filepath.Join(root, "hosts"))}, projectDir, phpDir, nodeDir
}

func doctorFinding(report sitedoctor.Report, id string) sitedoctor.Finding {
	for _, finding := range report.Findings {
		if finding.ID == id {
			return finding
		}
	}
	return sitedoctor.Finding{}
}

func TestSiteDoctorDiagnosisDoesNotRepairFiles(t *testing.T) {
	app, dir, phpDir, _ := doctorAppFixture(t)
	report, err := app.DiagnoseProject("demo")
	if err != nil {
		t.Fatal(err)
	}
	if report.Project != "demo" || doctorFinding(report, "env").FixID != "env" || report.PhpRuntime.Version != "8.3" {
		t.Fatalf("unexpected report: %+v", report)
	}
	for _, path := range []string{filepath.Join(dir, ".env"), filepath.Join(phpDir, "php.ini"), app.nginx.VhostPath("demo")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("diagnosis mutated %s: %v", path, err)
		}
	}
}

func TestSiteDoctorEnvFixRechecksFindingAndNeverOverwrites(t *testing.T) {
	app, dir, _, _ := doctorAppFixture(t)
	request := DoctorFixRequest{Project: "demo", FixID: "env", OperationID: "env-fixture"}
	result, err := app.ApplyDoctorFix(request)
	if err != nil || !result.Success {
		t.Fatalf("fix %+v %v", result, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_KEY=keep-this-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	request.OperationID = "stale-env-fixture"
	if _, err := app.ApplyDoctorFix(request); err == nil {
		t.Fatal("stale env fix was accepted")
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if string(data) != "APP_KEY=keep-this-key\n" {
		t.Fatal("stale fix overwrote user settings")
	}
}

func TestSiteDoctorMigrationRequiresConfirmationBeforeInspection(t *testing.T) {
	app, _, _, _ := doctorAppFixture(t)
	_, err := app.ApplyDoctorFix(DoctorFixRequest{Project: "demo", FixID: "migrations", OperationID: "migration-fixture"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "confirm") {
		t.Fatalf("migration ran without confirmation: %v", err)
	}
}

func TestSiteDoctorProjectScopeDoesNotCreateMissingRoot(t *testing.T) {
	app, _, _, _ := doctorAppFixture(t)
	prefs := app.cfg.Get().Projects
	prefs.Root = filepath.Join(t.TempDir(), "absent")
	if err := app.cfg.SetProjects(prefs); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DiagnoseAllProjects(); err == nil {
		t.Fatal("missing root reported healthy")
	}
	if _, err := os.Stat(prefs.Root); !os.IsNotExist(err) {
		t.Fatal("doctor created missing projects root")
	}
}

func TestSiteDoctorNodeSpecPinsRuntimeAndDirectory(t *testing.T) {
	app, projectDir, phpDir, nodeDir := doctorAppFixture(t)
	project, err := app.mcpProject("demo")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := app.doctorNodeInstallSpec(project)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Executable != filepath.Join(nodeDir, "node.exe") || len(spec.Args) < 2 || spec.Args[0] != filepath.Join(nodeDir, "node_modules", "npm", "bin", "npm-cli.js") || spec.Args[1] != "install" {
		t.Fatalf("wrong node invocation: executable=%q dir=%q args=%q", spec.Executable, spec.Dir, spec.Args)
	}
	assertSameFilesystemPath(t, spec.Dir, projectDir)
	for _, variable := range spec.Env {
		if strings.HasPrefix(strings.ToUpper(variable), "PATH=") && !strings.HasPrefix(variable[5:], phpDir+string(os.PathListSeparator)+nodeDir+string(os.PathListSeparator)) {
			t.Fatal("selected PHP and Node directories are not first in PATH")
		}
	}
}

func TestSiteDoctorNodeSpecRejectsMissingRequirement(t *testing.T) {
	app, dir, _, _ := doctorAppFixture(t)
	if err := app.cfg.SetProjectNode("demo", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".nvmrc"), []byte("99"), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := app.mcpProject("demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.doctorNodeInstallSpec(project); err == nil {
		t.Fatal("missing requirement used another Node")
	}
}

func TestSiteDoctorMigrationOutputDoesNotGuessUnknownResults(t *testing.T) {
	for _, tc := range []struct {
		output  string
		pending int
		valid   bool
	}{
		{"2025_01_01_000001_users ................. [1] Ran\n2025_01_02_000001_posts ................. Pending\n", 1, true},
		{"| Ran? | Migration | Batch |\n| Yes | create_users | 1 |\n| No | create_posts | |\n", 1, true},
		{"INFO No migrations found.\n", 0, true},
		{"Exception: failed to connect with password=private", 0, false},
		{"", 0, false},
	} {
		state, err := doctorParseMigrations(tc.output)
		if (err == nil) != tc.valid || (tc.valid && state.Pending != tc.pending) {
			t.Errorf("parsed state=%+v err=%v, want pending=%d valid=%v", state, err, tc.pending, tc.valid)
		}
	}
}

func TestSiteDoctorCorepackInstallUsesSelectedNode(t *testing.T) {
	app, dir, _, nodeDir := doctorAppFixture(t)
	for file, content := range map[string]string{filepath.Join(dir, "package.json"): `{"packageManager":"pnpm@9.0.0"}`, filepath.Join(nodeDir, "pnpm.cmd"): "fixture", filepath.Join(nodeDir, "node_modules", "corepack", "dist", "pnpm.js"): "fixture"} {
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := app.mcpProject("demo")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := app.doctorNodeInstallSpec(p)
	if err != nil || spec.Executable != filepath.Join(nodeDir, "node.exe") || len(spec.Args) == 0 || spec.Args[0] != filepath.Join(nodeDir, "node_modules", "corepack", "dist", "pnpm.js") {
		t.Fatalf("Corepack selected wrong Node: executable=%q args=%q error=%v", spec.Executable, spec.Args, err)
	}
}

func TestSiteDoctorHostsRepairPreservesOtherAliasesAndLoopback(t *testing.T) {
	input := []byte("127.0.0.2 keep.test\n10.1.2.3 demo.test other.test # lara4ever\n# ignored demo.test\n")
	output, err := doctorRepairHostsData(input, "demo.test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "10.1.2.3 other.test # lara4ever") || !strings.Contains(string(output), "127.0.0.1 demo.test # lara4ever") || !strings.Contains(string(output), "127.0.0.2 keep.test") {
		t.Fatalf("aliases lost: %s", output)
	}
	if _, err := doctorRepairHostsData(output, "keep.test"); err != nil {
		t.Fatalf("valid loopback treated as conflicting user mapping: %v", err)
	}
	if _, err := doctorRepairHostsData([]byte("10.1.2.3 demo.test # personal\n"), "demo.test"); err == nil {
		t.Fatal("unmanaged hosts mapping overwritten")
	}
}

func TestSiteDoctorOperationIsolation(t *testing.T) {
	app, dir, _, _ := doctorAppFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	other, otherCancel := context.WithCancel(context.Background())
	defer otherCancel()
	app.doctorOperations = map[string]doctorOperation{"active": {project: "demo", cancel: cancel}, "other": {project: "other", cancel: otherCancel}}
	if _, err := app.ApplyDoctorFix(DoctorFixRequest{Project: "demo", FixID: "env", OperationID: "second"}); err == nil {
		t.Fatal("concurrent repair accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Fatal("rejected repair mutated project")
	}
	if err := app.CancelDoctorFix("active"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil || other.Err() != nil {
		t.Fatal("canceled a different operation")
	}
}

func TestSiteDoctorDiagnosisDoesNotCreateRouteMetadata(t *testing.T) {
	app, dir, _, _ := doctorAppFixture(t)
	path := filepath.Join(filepath.Dir(dir), "routes.json")
	store, err := projectroutes.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	app.routes = projectroutes.NewManager(store, app.resolveRouteProject, nil, nil)
	if _, err := app.DiagnoseProject("demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read-only diagnosis wrote route metadata")
	}
}

func TestSiteDoctorUnresolvedPreviewCannotOfferVhostRepair(t *testing.T) {
	app, dir, _, _ := doctorAppFixture(t)
	if err := app.cfg.SetProjectPhp("demo", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".php-version"), []byte("99"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err := app.DiagnoseProject("demo")
	if err != nil {
		t.Fatal(err)
	}
	if finding := doctorFinding(report, "vhost"); finding.FixID != "" || finding.Code != "vhost_unavailable" {
		t.Fatalf("unresolved PHP offered repair: %+v", finding)
	}
}

func TestSiteDoctorEarlyCancellationPreventsRepair(t *testing.T) {
	app, dir, _, _ := doctorAppFixture(t)
	if err := app.CancelDoctorFix("early"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyDoctorFix(DoctorFixRequest{Project: "demo", FixID: "env", OperationID: "early"}); err != context.Canceled {
		t.Fatal("early cancellation did not prevent repair")
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Fatal("canceled repair wrote environment")
	}
}

func TestSiteDoctorRejectsChangedRuntimeAndExistingKey(t *testing.T) {
	app, dir, _, _ := doctorAppFixture(t)
	checked, err := app.doctorProject("demo")
	if err != nil {
		t.Fatal(err)
	}
	current := checked
	current.NodeRuntime.Version = "24.0.0"
	if doctorValidateRepairSnapshot(checked, current) == nil {
		t.Fatal("repair accepted a changed runtime")
	}
	current = checked
	current.Paused = true
	if doctorValidateRepairSnapshot(checked, current) == nil {
		t.Fatal("repair accepted a paused project")
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_KEY=base64:fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if doctorValidateEmptyKey(dir) == nil {
		t.Fatal("key generation accepted an existing APP_KEY")
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_KEY=\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if doctorValidateEmptyKey(dir) != nil {
		t.Fatal("empty APP_KEY rejected")
	}
}

func TestSiteDoctorVhostRepairPreservesAliasesAndRejectsUserMapping(t *testing.T) {
	app, _, _, _ := doctorAppFixture(t)
	p, err := app.doctorProject("demo")
	if err != nil {
		t.Fatal(err)
	}
	initial := []byte("127.0.0.1 demo.test alias.test # lara4ever\n127.0.0.1 demo.test custom.test # lara4ever-custom\n")
	if err := app.hosts.Restore(initial); err != nil {
		t.Fatal(err)
	}
	if err := app.doctorWithPreservedHosts(p, func() error { return app.hosts.RemoveEntry("demo.test") }); err != nil {
		t.Fatal(err)
	}
	after, err := app.hosts.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "alias.test") || !strings.Contains(string(after), "custom.test") {
		t.Fatal("vhost repair lost unrelated hosts aliases")
	}
	if err := app.hosts.Restore([]byte("10.0.0.1 demo.test # personal\n")); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := app.doctorWithPreservedHosts(p, func() error { called = true; return nil }); err == nil || called {
		t.Fatal("vhost repair mutated configuration with conflicting user mapping")
	}
}

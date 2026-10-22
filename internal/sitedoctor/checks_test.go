package sitedoctor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/projects"
)

func doctorFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func doctorTarget(t *testing.T) Target {
	t.Helper()
	path := t.TempDir()
	if err := os.Mkdir(filepath.Join(path, "public"), 0700); err != nil {
		t.Fatal(err)
	}
	php := filepath.Join(t.TempDir(), "php.exe")
	doctorFile(t, php, "fixture")
	doctorFile(t, filepath.Join(filepath.Dir(php), "php-cgi.exe"), "fixture")
	return Target{Project: projects.Project{Name: "demo", Path: path, DocRoot: filepath.Join(path, "public"), Kind: "laravel", URL: "http://demo.test", PhpRuntime: projects.RuntimeResolution{Version: "8.4", FullVersion: "8.4.20", Status: "resolved", Source: "global"}, NodeRuntime: projects.RuntimeResolution{Status: "unmanaged", Source: "global"}}, PHPExe: php, Hosts: []byte("127.0.0.1 demo.test # owned\n"), PHPFPMStatus: "running"}
}

func doctorFinding(t *testing.T, report Report, id string) Finding {
	t.Helper()
	for _, f := range report.Findings {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("finding %s absent: %#v", id, report)
	return Finding{}
}

func doctorReadyLaravel(t *testing.T, target Target) Dependencies {
	t.Helper()
	doctorFile(t, filepath.Join(target.Project.Path, ".env"), "APP_KEY=secret-key\nDB_CONNECTION=mysql\nDB_DATABASE=demo\nDB_USERNAME=root\nDB_PASSWORD=private-password\n")
	doctorFile(t, filepath.Join(target.Project.Path, "vendor", "autoload.php"), "<?php")
	return Dependencies{Database: func(context.Context, DatabaseConnection) (DatabaseState, error) {
		return DatabaseState{Status: "exists"}, nil
	}, Migrations: func(context.Context, projects.Project) (MigrationState, error) { return MigrationState{}, nil }}
}

func TestCheckMissingEnvironmentOnlyOffersCopyWhenExampleExists(t *testing.T) {
	target := doctorTarget(t)
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "env"); f.Status != "fail" || f.FixID != "" {
		t.Fatalf("unsafe copy action: %#v", f)
	}
	doctorFile(t, filepath.Join(target.Project.Path, ".env.example"), "APP_KEY=\n")
	report, err = Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "env"); f.FixID != "env" {
		t.Fatalf("copy action absent: %#v", f)
	}
	if f := doctorFinding(t, report, "app_key"); f.FixID != "" || f.Status != "skipped" {
		t.Fatalf("key fix bypassed env: %#v", f)
	}
	if _, err := os.Stat(filepath.Join(target.Project.Path, ".env")); !os.IsNotExist(err) {
		t.Fatal("diagnosis created .env")
	}
}

func TestCheckAppKeyEmptyNeedsExecutableAndVendor(t *testing.T) {
	target := doctorTarget(t)
	doctorFile(t, filepath.Join(target.Project.Path, ".env"), "APP_KEY=\"\"\n")
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "app_key"); f.Status != "fail" || f.FixID != "" {
		t.Fatalf("key repair bypassed vendor: %#v", f)
	}
	doctorFile(t, filepath.Join(target.Project.Path, "vendor", "autoload.php"), "<?php")
	report, err = Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "app_key"); f.FixID != "app_key" {
		t.Fatalf("key fix absent: %#v", f)
	}
}

func TestCheckCachedApplicationKeyRequiresManualReview(t *testing.T) {
	target := doctorTarget(t)
	doctorFile(t, filepath.Join(target.Project.Path, ".env"), "APP_KEY=\n")
	doctorFile(t, filepath.Join(target.Project.Path, "vendor", "autoload.php"), "<?php")
	doctorFile(t, filepath.Join(target.Project.Path, "bootstrap", "cache", "config.php"), "<?php return ['app'=>['key'=>'cached-fixture']];")
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	f := doctorFinding(t, report, "app_key")
	if f.Code != "app_key_unsupported" || f.FixID != "" {
		t.Fatal("cached application key offered unsafe generation")
	}
}

func TestCheckMigrationsGateDatabaseAndRequireConfirmation(t *testing.T) {
	target := doctorTarget(t)
	deps := doctorReadyLaravel(t, target)
	deps.Migrations = func(context.Context, projects.Project) (MigrationState, error) {
		return MigrationState{Pending: 3}, nil
	}
	report, err := Check(context.Background(), target, deps)
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "migrations"); f.Status != "warning" || f.FixID != "migrations" || !f.RequiresConfirmation {
		t.Fatalf("pending repair invalid: %#v", f)
	}
	deps.Database = func(context.Context, DatabaseConnection) (DatabaseState, error) {
		return DatabaseState{Status: "missing"}, nil
	}
	deps.Migrations = func(context.Context, projects.Project) (MigrationState, error) {
		t.Fatal("migrations ran against missing database")
		return MigrationState{}, nil
	}
	report, err = Check(context.Background(), target, deps)
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "migrations"); f.Status != "skipped" || f.FixID != "" {
		t.Fatalf("missing DB ignored: %#v", f)
	}
}

func TestCheckProbeErrorsNeverExposeSecretsOrOfferRepair(t *testing.T) {
	target := doctorTarget(t)
	deps := doctorReadyLaravel(t, target)
	deps.Database = func(context.Context, DatabaseConnection) (DatabaseState, error) {
		return DatabaseState{}, errors.New("private-password secret-key leaked")
	}
	report, err := Check(context.Background(), target, deps)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-password", "secret-key"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("secret %q exposed", secret)
		}
	}
	if f := doctorFinding(t, report, "database"); f.Status != "skipped" || f.Code != "database_probe_failed" || f.FixID != "" {
		t.Fatalf("probe error guessed absence: %#v", f)
	}
}

func TestCheckRuntimeUsesResolutionAndDownloadSuggestion(t *testing.T) {
	target := doctorTarget(t)
	target.Project.PhpRuntime = projects.RuntimeResolution{Status: "missing", Source: "composer.json", Constraint: "^8.5", Problem: "missing_runtime", InstallVersion: "8.5.1"}
	target.Project.NodeRuntime = projects.RuntimeResolution{Status: "incompatible", Source: "manual", Version: "22.1", FullVersion: "22.1.0", Constraint: ">=24", Problem: "incompatible_override"}
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "php_runtime"); f.Status != "fail" || f.FixID != "download" || f.RuntimeKind != "php" || f.InstallVersion != "8.5.1" {
		t.Fatalf("missing requirement lost: %#v", f)
	}
	if f := doctorFinding(t, report, "node_runtime"); f.Status != "warning" {
		t.Fatalf("override mismatch lost: %#v", f)
	}
}

func TestCheckHostsRejectsCommentedAndWrongIPEntries(t *testing.T) {
	for _, hosts := range []string{"# 127.0.0.1 demo.test\n", "10.0.0.1 demo.test\n", "127.0.0.1 other.test # demo.test\n"} {
		target := doctorTarget(t)
		target.Hosts = []byte(hosts)
		report, err := Check(context.Background(), target, Dependencies{})
		if err != nil {
			t.Fatal(err)
		}
		if f := doctorFinding(t, report, "hosts"); f.Status != "fail" {
			t.Fatalf("invalid host accepted %q: %#v", hosts, f)
		}
	}
	target := doctorTarget(t)
	target.Hosts = []byte("::1 other.test DEMO.test # extra\n")
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "hosts"); f.Status != "pass" {
		t.Fatalf("IPv6 alias ignored: %#v", f)
	}
}

func TestCheckVhostMismatchRequiresConfirmationButNormalizesLineEndings(t *testing.T) {
	target := doctorTarget(t)
	path := filepath.Join(t.TempDir(), "demo.conf")
	target.Vhosts = []VhostCheck{{Path: path, Expected: []byte("server {\n root /demo;\n}\n")}}
	doctorFile(t, path, "server {\r\n root /demo;\r\n}\r\n")
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "vhost"); f.Status != "pass" {
		t.Fatalf("CRLF mismatch: %#v", f)
	}
	doctorFile(t, path, "server { manual changes; }")
	report, err = Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "vhost"); f.FixID != "vhost" || !f.RequiresConfirmation {
		t.Fatalf("manual edits unprotected: %#v", f)
	}
}

func TestCheckStorageDirectoryIsNeverOfferedDestructiveRepair(t *testing.T) {
	target := doctorTarget(t)
	deps := doctorReadyLaravel(t, target)
	if err := os.MkdirAll(filepath.Join(target.Project.Path, "public", "storage"), 0700); err != nil {
		t.Fatal(err)
	}
	report, err := Check(context.Background(), target, deps)
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "storage_link"); f.Status != "optional" || f.FixID != "" {
		t.Fatalf("user directory would be overwritten: %#v", f)
	}
}

func TestCheckSQLiteIsReadOnlyAndCannotOfferCreateDatabase(t *testing.T) {
	target := doctorTarget(t)
	doctorFile(t, filepath.Join(target.Project.Path, ".env"), "APP_KEY=key\nDB_CONNECTION=sqlite\nDB_DATABASE=database/database.sqlite\n")
	doctorFile(t, filepath.Join(target.Project.Path, "vendor", "autoload.php"), "<?php")
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "database"); f.Status != "fail" || f.FixID != "" {
		t.Fatalf("sqlite create offered: %#v", f)
	}
	if _, err := os.Stat(filepath.Join(target.Project.Path, "database", "database.sqlite")); !os.IsNotExist(err) {
		t.Fatal("diagnosis created sqlite DB")
	}
}

func TestCheckPausedDisablesEveryFixAndMigrationProbe(t *testing.T) {
	target := doctorTarget(t)
	target.Project.Paused = true
	deps := doctorReadyLaravel(t, target)
	deps.Migrations = func(context.Context, projects.Project) (MigrationState, error) {
		t.Fatal("paused migrations ran")
		return MigrationState{}, nil
	}
	report, err := Check(context.Background(), target, deps)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.FixID != "" || f.RequiresConfirmation {
			t.Fatalf("paused repair available: %#v", f)
		}
	}
	if f := doctorFinding(t, report, "php_fpm"); f.Status != "skipped" {
		t.Fatalf("paused FPM checked: %#v", f)
	}
}

func TestCheckWordPressConfigMissingAndDynamicAreDistinct(t *testing.T) {
	target := doctorTarget(t)
	target.Project.Kind = "wordpress"
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "wp_config"); f.Status != "fail" {
		t.Fatalf("missing config not reported: %#v", f)
	}
	doctorFile(t, filepath.Join(target.Project.Path, "wp-config.php"), "<?php define('DB_NAME', getenv('DB_NAME'));")
	report, err = Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "database"); f.Status != "skipped" || f.FixID != "" {
		t.Fatalf("dynamic config guessed: %#v", f)
	}
}

func TestCheckHonorsCancellationBeforeInspecting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Check(ctx, doctorTarget(t), Dependencies{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}

func TestCheckHappyLaravelResolvesStorageLinkWithoutChangingFiles(t *testing.T) {
	target := doctorTarget(t)
	deps := doctorReadyLaravel(t, target)
	storage := filepath.Join(target.Project.Path, "storage", "app", "public")
	if err := os.MkdirAll(storage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(storage, filepath.Join(target.Project.Path, "public", "storage")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	report, err := Check(context.Background(), target, deps)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"env", "app_key", "composer_dependencies", "storage_link", "database", "migrations", "php_runtime", "hosts", "php_fpm", "document_root"} {
		if f := doctorFinding(t, report, id); f.Status != "pass" || f.FixID != "" {
			t.Fatalf("healthy check %s invalid: %#v", id, f)
		}
	}
	before, err := os.ReadFile(filepath.Join(target.Project.Path, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != "APP_KEY=secret-key\nDB_CONNECTION=mysql\nDB_DATABASE=demo\nDB_USERNAME=root\nDB_PASSWORD=private-password\n" {
		t.Fatal("diagnosis modified environment")
	}
}

func TestCheckStaticProjectSkipsPHPFPMAndFrameworkChecks(t *testing.T) {
	target := doctorTarget(t)
	target.Project.Kind = "plain"
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"php_fpm", "env", "app_key", "migrations", "database", "wp_config"} {
		if f := doctorFinding(t, report, id); f.Status != "skipped" {
			t.Fatalf("static %s invalid: %#v", id, f)
		}
	}
}

func TestCheckProbeCancellationStopsReport(t *testing.T) {
	target := doctorTarget(t)
	deps := doctorReadyLaravel(t, target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps.Database = func(context.Context, DatabaseConnection) (DatabaseState, error) {
		cancel()
		return DatabaseState{}, context.Canceled
	}
	if _, err := Check(ctx, target, deps); !errors.Is(err, context.Canceled) {
		t.Fatalf("probe cancellation ignored: %v", err)
	}
}

func TestCheckHostsCoversEveryPreviewRoute(t *testing.T) {
	target := doctorTarget(t)
	target.Hostnames = []string{"demo.test", "preview.demo.test"}
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "hosts"); f.Status != "fail" {
		t.Fatalf("missing preview hostname ignored: %#v", f)
	}
	target.Hosts = []byte("127.0.0.1 DEMO.test preview.demo.test # owned\n")
	report, err = Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "hosts"); f.Status != "pass" {
		t.Fatalf("preview mapping rejected: %#v", f)
	}
}

func TestCheckPHPFPMCoversServedRoutesForStaticMainCheckout(t *testing.T) {
	target := doctorTarget(t)
	target.Project.Kind = "plain"
	target.PHPFPMStatuses = map[string]string{"8.3": "running", "8.4": "stopped"}
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "php_fpm"); f.Status != "warning" || f.FixID != "php_fpm" {
		t.Fatalf("served PHP route ignored: %#v", f)
	}
	target.PHPFPMStatuses["8.4"] = "running"
	report, err = Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "php_fpm"); f.Status != "pass" {
		t.Fatalf("running route pool ignored: %#v", f)
	}
}

func TestCheckMissingCGIDisablesPHPFPMRepair(t *testing.T) {
	target := doctorTarget(t)
	target.PHPFPMStatus = "stopped"
	if err := os.Remove(filepath.Join(filepath.Dir(target.PHPExe), "php-cgi.exe")); err != nil {
		t.Fatal(err)
	}
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "php_fpm"); f.Status != "warning" || f.Code != "php_fpm_unavailable" || f.FixID != "" {
		t.Fatalf("missing CGI repair available: %#v", f)
	}
}

func TestCheckUnavailableServedPoolDisablesPHPFPMRepair(t *testing.T) {
	target := doctorTarget(t)
	target.PHPFPMStatuses = map[string]string{"8.3": "stopped", "8.4": "unavailable"}
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "php_fpm"); f.FixID != "" || f.Code != "php_fpm_unavailable" {
		t.Fatalf("unavailable pool repair offered: %#v", f)
	}
}

func TestCheckHostsConflictingUnmanagedMappingCannotBeRepaired(t *testing.T) {
	target := doctorTarget(t)
	target.Hosts = []byte("10.0.0.7 demo.test # user-owned\n127.0.0.1 demo.test # lara4ever\n")
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "hosts"); f.Status != "fail" || f.FixID != "" {
		t.Fatalf("conflicting user mapping accepted: %#v", f)
	}
}

func TestCheckPausedProjectsSkipDatabaseProbes(t *testing.T) {
	for _, kind := range []string{"laravel", "wordpress"} {
		t.Run(kind, func(t *testing.T) {
			target := doctorTarget(t)
			target.Project.Kind, target.Project.Paused = kind, true
			deps := doctorReadyLaravel(t, target)
			if kind == "wordpress" {
				doctorFile(t, filepath.Join(target.Project.Path, "wp-config.php"), "<?php define('DB_NAME', 'demo'); define('DB_USER', 'root'); define('DB_PASSWORD', ''); define('DB_HOST', '127.0.0.1');")
			}
			deps.Database = func(context.Context, DatabaseConnection) (DatabaseState, error) {
				t.Fatal("paused project database probe ran")
				return DatabaseState{}, nil
			}
			deps.Migrations = func(context.Context, projects.Project) (MigrationState, error) {
				t.Fatal("paused project migration probe ran")
				return MigrationState{}, nil
			}
			report, err := Check(context.Background(), target, deps)
			if err != nil {
				t.Fatal(err)
			}
			if f := doctorFinding(t, report, "database"); f.Status != "skipped" || f.Code != "project_paused" || f.FixID != "" {
				t.Fatalf("paused database evidence invalid: %#v", f)
			}
		})
	}
}

func TestCheckUnresolvedServingRuntimeDisablesMissingVhostRepair(t *testing.T) {
	target := doctorTarget(t)
	target.Project.Kind = "plain"
	target.Vhosts = []VhostCheck{{Path: filepath.Join(t.TempDir(), "preview.conf"), Expected: nil}}
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "vhost"); f.Code != "vhost_unavailable" || f.Status != "skipped" || f.FixID != "" {
		t.Fatalf("impossible vhost repair offered: %#v", f)
	}
}

func TestCheckExplicitEmptyServedPoolsSkipPHPFPMDespiteCLIRequirement(t *testing.T) {
	target := doctorTarget(t)
	target.Project.Kind = "plain"
	doctorFile(t, filepath.Join(target.Project.Path, "composer.json"), "{\"require\":{\"php\":\"^8.4\"}}")
	target.PHPFPMStatus = "stopped"
	target.PHPFPMStatuses = map[string]string{}
	report, err := Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "php_fpm"); f.Status != "skipped" || f.FixID != "" {
		t.Fatalf("CLI PHP created a serving pool: %#v", f)
	}
	target.PHPFPMStatuses = nil
	report, err = Check(context.Background(), target, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if f := doctorFinding(t, report, "php_fpm"); f.Status != "warning" || f.FixID != "php_fpm" {
		t.Fatalf("legacy single-pool fallback lost: %#v", f)
	}
}

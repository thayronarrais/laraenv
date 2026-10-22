package sitedoctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"lara4ever/internal/projects"
)

func healthyDoctorTarget(t *testing.T) (Target, Dependencies) {
	t.Helper()
	target := doctorTarget(t)
	deps := doctorReadyLaravel(t, target)
	vhost := filepath.Join(t.TempDir(), "demo.conf")
	doctorFile(t, vhost, "server { root /demo; }")
	target.Vhosts = []VhostCheck{{Path: vhost, Expected: []byte("server { root /demo; }")}}
	return target, deps
}

func TestCheckOptionalStorageDoesNotRequireAttention(t *testing.T) {
	for _, occupied := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "custom-directory"}[occupied], func(t *testing.T) {
			target, deps := healthyDoctorTarget(t)
			if occupied {
				doctorFile(t, filepath.Join(target.Project.Path, "public", "storage", "keep.txt"), "user data")
			}
			report, err := Check(context.Background(), target, deps)
			if err != nil {
				t.Fatal(err)
			}
			f := doctorFinding(t, report, "storage_link")
			if f.Status != "optional" || f.Severity != "info" || !report.Healthy {
				t.Fatalf("optional storage requires attention: finding=%+v healthy=%v", f, report.Healthy)
			}
			if occupied && f.FixID != "" {
				t.Fatal("existing storage directory offered replacement")
			}
			if !occupied && f.FixID != "storage_link" {
				t.Fatal("optional storage creation is unavailable despite ready prerequisites")
			}
		})
	}
}

func TestCheckInspectionLimitsAreSkippedRatherThanProblems(t *testing.T) {
	for _, tc := range []struct {
		name, id, code string
		setup          func(*testing.T, *Target, *Dependencies)
	}{
		{"cached-key", "app_key", "app_key_unsupported", func(t *testing.T, target *Target, _ *Dependencies) {
			doctorFile(t, filepath.Join(target.Project.Path, "bootstrap", "cache", "config.php"), "<?php return [];")
		}},
		{"cached-database", "database", "database_config_unsupported", func(t *testing.T, target *Target, _ *Dependencies) {
			doctorFile(t, filepath.Join(target.Project.Path, "bootstrap", "cache", "config.php"), "<?php return [];")
		}},
		{"interpolated-key", "app_key", "app_key_unsupported", func(t *testing.T, target *Target, _ *Dependencies) {
			doctorFile(t, filepath.Join(target.Project.Path, ".env"), "APP_KEY=${KEY}\nDB_CONNECTION=sqlite\nDB_DATABASE=:memory:\n")
		}},
		{"external-database", "database", "database_external", func(_ *testing.T, _ *Target, deps *Dependencies) {
			deps.Database = func(context.Context, DatabaseConnection) (DatabaseState, error) {
				return DatabaseState{Status: "unsupported", Code: "database_external"}, nil
			}
		}},
		{"unsupported-database", "database", "database_unsupported", func(_ *testing.T, _ *Target, deps *Dependencies) {
			deps.Database = func(context.Context, DatabaseConnection) (DatabaseState, error) {
				return DatabaseState{Status: "unsupported", Code: "database_unsupported"}, nil
			}
		}},
		{"unsupported-database-name", "database", "database_name_unsupported", func(_ *testing.T, _ *Target, deps *Dependencies) {
			deps.Database = func(context.Context, DatabaseConnection) (DatabaseState, error) {
				return DatabaseState{Status: "unsupported", Code: "database_name_unsupported"}, nil
			}
		}},
		{"migration-probe-absent", "migrations", "migrations_unavailable", func(_ *testing.T, _ *Target, deps *Dependencies) {
			deps.Migrations = nil
		}},
		{"migration-probe-failed", "migrations", "migrations_unavailable", func(_ *testing.T, _ *Target, deps *Dependencies) {
			deps.Migrations = func(context.Context, projects.Project) (MigrationState, error) {
				return MigrationState{}, errors.New("unavailable fixture")
			}
		}},
		{"system-node-runtime", "node_runtime", "runtime_unmanaged", func(t *testing.T, target *Target, _ *Dependencies) {
			doctorFile(t, filepath.Join(target.Project.Path, "package.json"), "{}")
			target.NodeDir = t.TempDir()
			doctorFile(t, filepath.Join(target.NodeDir, "node.exe"), "fixture")
			if err := os.Mkdir(filepath.Join(target.Project.Path, "node_modules"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{"vhost-comparison-unavailable", "vhost", "vhost_unavailable", func(_ *testing.T, target *Target, _ *Dependencies) {
			target.Vhosts = nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, deps := healthyDoctorTarget(t)
			tc.setup(t, &target, &deps)
			report, err := Check(context.Background(), target, deps)
			if err != nil {
				t.Fatal(err)
			}
			f := doctorFinding(t, report, tc.id)
			if f.Status != "skipped" || f.Code != tc.code || f.FixID != "" || !report.Healthy {
				t.Fatalf("inspection limit marked as problem: finding=%+v healthy=%v", f, report.Healthy)
			}
		})
	}
}

func TestCheckDatabaseProblemsNeedAttentionEvenWithoutAutomaticRepair(t *testing.T) {
	for _, state := range []DatabaseState{
		{Status: "unreachable", Code: "database_auth_failed"},
		{Status: "unreachable", Code: "database_unreachable"},
		{Status: "unsupported", Code: "database_extension_missing"},
	} {
		t.Run(state.Code, func(t *testing.T) {
			target, deps := healthyDoctorTarget(t)
			deps.Database = func(context.Context, DatabaseConnection) (DatabaseState, error) { return state, nil }
			report, err := Check(context.Background(), target, deps)
			if err != nil {
				t.Fatal(err)
			}
			f := doctorFinding(t, report, "database")
			if f.Status != "warning" || f.FixID != "" || report.Healthy {
				t.Fatalf("database problem hidden: finding=%+v healthy=%v", f, report.Healthy)
			}
		})
	}
}

func TestCheckInconclusiveDatabaseProbeDoesNotMarkProjectBroken(t *testing.T) {
	for _, scenario := range []string{"inconclusive", "legacy-probe-failed", "executor-error", "probe-unavailable", "unknown-result"} {
		t.Run(scenario, func(t *testing.T) {
			target, deps := healthyDoctorTarget(t)
			deps.Database = func(context.Context, DatabaseConnection) (DatabaseState, error) {
				switch scenario {
				case "executor-error":
					return DatabaseState{}, errors.New("private-password")
				case "legacy-probe-failed":
					return DatabaseState{Status: "unreachable", Code: "database_probe_failed"}, nil
				case "unknown-result":
					return DatabaseState{}, nil
				default:
					return DatabaseState{Status: "inconclusive", Code: "database_probe_failed"}, nil
				}
			}
			if scenario == "probe-unavailable" {
				deps.Database = nil
			}
			deps.Migrations = func(context.Context, projects.Project) (MigrationState, error) {
				t.Fatal("migrations probed without a confirmed database connection")
				return MigrationState{}, nil
			}
			report, err := Check(context.Background(), target, deps)
			if err != nil {
				t.Fatal(err)
			}
			f := doctorFinding(t, report, "database")
			if f.Status != "skipped" || f.Code != "database_probe_failed" || f.FixID != "" || f.Detail != "" || !report.Healthy {
				t.Fatalf("inconclusive probe treated as confirmed failure: finding=%+v healthy=%v", f, report.Healthy)
			}
			if doctorFinding(t, report, "migrations").Status != "skipped" {
				t.Fatal("migrations reported as verified without a database connection")
			}
		})
	}
}

func TestCheckUnavailableVhostComparisonDoesNotHideKnownRouteProblems(t *testing.T) {
	for _, problem := range []string{"missing", "mismatch"} {
		for _, unavailableFirst := range []bool{false, true} {
			name := problem + map[bool]string{false: "-unavailable-last", true: "-unavailable-first"}[unavailableFirst]
			t.Run(name, func(t *testing.T) {
				target, deps := healthyDoctorTarget(t)
				path := filepath.Join(t.TempDir(), "known.conf")
				known := VhostCheck{Path: path, Expected: []byte("server { root /demo; }")}
				status, code := "fail", "vhost_missing"
				if problem == "mismatch" {
					doctorFile(t, path, "server { root /custom; }")
					status, code = "warning", "vhost_mismatch"
				}
				unavailable := VhostCheck{Path: filepath.Join(t.TempDir(), "unknown.conf"), Expected: nil}
				target.Vhosts = []VhostCheck{known, unavailable}
				if unavailableFirst {
					target.Vhosts = []VhostCheck{unavailable, known}
				}
				report, err := Check(context.Background(), target, deps)
				if err != nil {
					t.Fatal(err)
				}
				f := doctorFinding(t, report, "vhost")
				if f.Status != status || f.Code != code || f.FixID != "" || f.RequiresConfirmation || report.Healthy {
					t.Fatalf("unavailable comparison hid route problem or offered unsafe regeneration: finding=%+v healthy=%v", f, report.Healthy)
				}
			})
		}
	}
}

func TestCheckMissingRuntimeCommandOnPATHRequiresAttention(t *testing.T) {
	for _, kind := range []string{"php", "nodejs"} {
		t.Run(kind, func(t *testing.T) {
			target, deps := healthyDoctorTarget(t)
			id := "php_runtime"
			if kind == "php" {
				target.Project.PhpRuntime = projects.RuntimeResolution{Status: "unmanaged", Source: "global"}
				target.PHPExe = ""
				target.PHPFPMStatuses = map[string]string{}
			} else {
				id = "node_runtime"
				doctorFile(t, filepath.Join(target.Project.Path, "package.json"), "{}")
				if err := os.Mkdir(filepath.Join(target.Project.Path, "node_modules"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			report, err := Check(context.Background(), target, deps)
			if err != nil {
				t.Fatal(err)
			}
			f := doctorFinding(t, report, id)
			if f.Status != "fail" || f.Code != "runtime_path_missing" || report.Healthy {
				t.Fatalf("missing required runtime treated as inspection limit: finding=%+v healthy=%v", f, report.Healthy)
			}
		})
	}
}

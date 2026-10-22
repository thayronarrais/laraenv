package sitedoctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func localDatabase() DatabaseConnection {
	return DatabaseConnection{Driver: "mysql", Host: "localhost", Port: 3306, Database: "project_app", Username: "project_user", Password: "sensitive-password"}
}

func TestProbeDatabaseClassifiesSanitizedResults(t *testing.T) {
	for _, tc := range []struct{ raw, status, code string }{
		{`{"status":"exists","code":"database_ok"}`, "exists", "database_ok"},
		{`{"status":"missing","code":"database_missing"}`, "missing", "database_missing"},
		{`{"status":"unreachable","code":"database_auth_failed"}`, "unreachable", "database_auth_failed"},
		{`{"status":"unreachable","code":"database_unreachable"}`, "unreachable", "database_unreachable"},
		{`{"status":"inconclusive","code":"database_probe_failed"}`, "inconclusive", "database_probe_failed"},
		{`{"status":"unsupported","code":"database_extension_missing"}`, "unsupported", "database_extension_missing"},
		{`{"status":"unreachable","code":"sensitive-password"}`, "inconclusive", "database_probe_failed"},
		{`{"status":"unreachable","code":"database_probe_failed"}`, "inconclusive", "database_probe_failed"},
		{"sensitive-password", "inconclusive", "database_probe_failed"},
	} {
		t.Run(tc.code+tc.status, func(t *testing.T) {
			got, err := ProbeDatabase(context.Background(), localDatabase(), func(_ context.Context, script string, input []byte) ([]byte, error) {
				var conn DatabaseConnection
				if err := json.Unmarshal(input, &conn); err != nil || conn != localDatabase() {
					t.Fatal("executor did not receive the expected connection settings")
				}
				if strings.Contains(script, "sensitive-password") {
					t.Fatal("credentials embedded in command script")
				}
				return []byte(tc.raw), nil
			})
			if err != nil || got.Status != tc.status || got.Code != tc.code {
				t.Fatal("database probe result was not classified safely")
			}
		})
	}
}

func TestDatabaseRejectsExternalTargetsAndUnsafeNamesBeforeExecution(t *testing.T) {
	for _, change := range []func(*DatabaseConnection){
		func(c *DatabaseConnection) { c.Host = "production.example" },
		func(c *DatabaseConnection) { c.Host = "127.0.0.2" },
		func(c *DatabaseConnection) { c.Port = 3307 },
		func(c *DatabaseConnection) { c.Database = "app`;DROP DATABASE mysql;--" },
		func(c *DatabaseConnection) { c.Database = "mysql" },
		func(c *DatabaseConnection) { c.Database = "" },
		func(c *DatabaseConnection) { c.Driver = "sqlsrv" },
	} {
		conn := localDatabase()
		change(&conn)
		execute := func(context.Context, string, []byte) ([]byte, error) {
			t.Fatal("unsafe connection executed")
			return nil, nil
		}
		state, err := ProbeDatabase(context.Background(), conn, execute)
		if err != nil || state.Status != "unsupported" {
			t.Fatal("unsafe probe was not rejected")
		}
		if err := CreateDatabase(context.Background(), conn, execute); err == nil {
			t.Fatal("unsafe database creation accepted")
		}
	}
}

func TestDatabaseExecutorErrorsNeverExposeSecrets(t *testing.T) {
	execute := func(context.Context, string, []byte) ([]byte, error) {
		return []byte("sensitive-password"), errors.New("connection failed for sensitive-password")
	}
	state, err := ProbeDatabase(context.Background(), localDatabase(), execute)
	if err != nil || state.Status != "inconclusive" || state.Code != "database_probe_failed" {
		t.Fatal("executor failure did not produce a generic probe result")
	}
	if err := CreateDatabase(context.Background(), localDatabase(), execute); err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatal("creation error leaked credentials or was absent")
	}
}

func TestProbeDatabaseUnavailableOrInvalidEvidenceIsInconclusive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		execute DBExecutor
	}{
		{name: "missing executor"},
		{name: "oversized output", execute: func(context.Context, string, []byte) ([]byte, error) {
			return []byte(strings.Repeat("sensitive-password", 100)), nil
		}},
		{name: "mismatched state", execute: func(context.Context, string, []byte) ([]byte, error) {
			return []byte(`{"status":"inconclusive","code":"database_unreachable"}`), nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, err := ProbeDatabase(context.Background(), localDatabase(), tc.execute)
			if err != nil || state != (DatabaseState{Status: "inconclusive", Code: "database_probe_failed"}) {
				t.Fatal("unavailable evidence claimed a database failure")
			}
		})
	}
}

func TestDatabasePDOProbeClassifiesOnlyKnownFailures(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("PHP runtime unavailable")
	}
	for _, tc := range []struct {
		name, driver, phase, sqlstate string
		native                        int
		throwable                     bool
		status, code                  string
	}{
		{name: "unknown client error", driver: "mysql", phase: "connect", sqlstate: "HY000", native: 2054, status: "inconclusive", code: "database_probe_failed"},
		{name: "TLS error", driver: "mysql", phase: "connect", sqlstate: "HY000", native: 2026, status: "inconclusive", code: "database_probe_failed"},
		{name: "unsupported PDO option", driver: "mysql", phase: "connect", sqlstate: "IM001", status: "inconclusive", code: "database_probe_failed"},
		{name: "query error after connection", driver: "mysql", phase: "query", sqlstate: "42000", native: 1064, status: "inconclusive", code: "database_probe_failed"},
		{name: "connect code after successful connection", driver: "mysql", phase: "query", sqlstate: "HY000", native: 2002, status: "inconclusive", code: "database_probe_failed"},
		{name: "missing code after successful connection", driver: "mysql", phase: "query", sqlstate: "HY000", native: 1049, status: "inconclusive", code: "database_probe_failed"},
		{name: "unexpected query throwable", driver: "mysql", phase: "query", throwable: true, status: "inconclusive", code: "database_probe_failed"},
		{name: "unexpected connect throwable", driver: "mysql", phase: "connect", throwable: true, status: "inconclusive", code: "database_probe_failed"},
		{name: "connection refused", driver: "mysql", phase: "connect", sqlstate: "HY000", native: 2002, status: "unreachable", code: "database_unreachable"},
		{name: "cannot connect", driver: "mysql", phase: "connect", sqlstate: "HY000", native: 2003, status: "unreachable", code: "database_unreachable"},
		{name: "unknown host", driver: "mysql", phase: "connect", sqlstate: "HY000", native: 2005, status: "unreachable", code: "database_unreachable"},
		{name: "query connection lost", driver: "mysql", phase: "query", sqlstate: "HY000", native: 2006, status: "unreachable", code: "database_unreachable"},
		{name: "query server connection lost", driver: "mysql", phase: "query", sqlstate: "HY000", native: 2013, status: "unreachable", code: "database_unreachable"},
		{name: "mysql authentication", driver: "mysql", phase: "connect", sqlstate: "28000", native: 1045, status: "unreachable", code: "database_auth_failed"},
		{name: "mysql database missing", driver: "mysql", phase: "connect", sqlstate: "HY000", native: 1049, status: "missing", code: "database_missing"},
		{name: "postgres authentication", driver: "pgsql", phase: "connect", sqlstate: "28P01", native: 7, status: "unreachable", code: "database_auth_failed"},
		{name: "postgres database missing", driver: "pgsql", phase: "connect", sqlstate: "3D000", native: 7, status: "missing", code: "database_missing"},
		{name: "postgres uncertain connection", driver: "pgsql", phase: "connect", sqlstate: "08006", native: 7, status: "inconclusive", code: "database_probe_failed"},
		{name: "postgres query connection failure", driver: "pgsql", phase: "query", sqlstate: "08006", native: 7, status: "unreachable", code: "database_unreachable"},
		{name: "postgres query connection does not exist", driver: "pgsql", phase: "query", sqlstate: "08003", native: 7, status: "unreachable", code: "database_unreachable"},
		{name: "successful connection and SELECT 1", driver: "mysql", phase: "none", status: "exists", code: "database_ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := localDatabase()
			conn.Driver = tc.driver
			if conn.Driver == "pgsql" {
				conn.Port = 5432
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			fixture := fmt.Sprintf(`namespace SiteDoctorFixture; use \Throwable;
class PDOException extends \Exception {
    public $errorInfo;
    public function __construct() { parent::__construct('private fixture detail sensitive-password project_user project_app'); $this->code='%s'; $this->errorInfo=['%s',%d]; }
}
class PDO {
    const ATTR_ERRMODE=3; const ERRMODE_EXCEPTION=2; const ATTR_TIMEOUT=2;
    public static function getAvailableDrivers() { return ['mysql','pgsql']; }
    private function fail($phase) { if ($phase !== '%s') return; if (%t) throw new \RuntimeException('private fixture detail sensitive-password project_user project_app'); throw new PDOException(); }
    public function __construct($dsn,$user,$password,$options=[]) { $this->fail('connect'); }
    public function query($sql) { if ($sql !== 'SELECT 1') throw new \RuntimeException('unexpected SQL'); $this->fail('query'); return true; }
} `, tc.sqlstate, tc.sqlstate, tc.native, tc.phase, tc.throwable)
			state, err := ProbeDatabase(ctx, conn, func(ctx context.Context, script string, input []byte) ([]byte, error) {
				cmd := exec.CommandContext(ctx, php, "-n", "-r", fixture+script)
				cmd.Stdin = strings.NewReader(string(input))
				out, err := cmd.Output()
				if strings.Contains(string(out), "private fixture") || strings.Contains(string(out), "sensitive-password") || strings.Contains(string(out), "project_user") || strings.Contains(string(out), "project_app") {
					t.Fatal("PDO probe exposed exception details")
				}
				if err != nil {
					t.Fatal("standalone PDO fixture failed to execute")
				}
				return out, err
			})
			if err != nil || state.Status != tc.status || state.Code != tc.code {
				t.Fatalf("probe classification: got %s/%s; want %s/%s", state.Status, state.Code, tc.status, tc.code)
			}
		})
	}
}

func TestCreateDatabasePreservesProjectCredentialsAndClassifiesPrivileges(t *testing.T) {
	for _, tc := range []struct {
		output    string
		wantError bool
	}{
		{`{"status":"exists","code":"database_created"}`, false},
		{`{"status":"unreachable","code":"database_create_denied"}`, true},
		{`{"status":"exists","code":"sensitive-password"}`, true},
	} {
		err := CreateDatabase(context.Background(), localDatabase(), func(_ context.Context, script string, input []byte) ([]byte, error) {
			var conn DatabaseConnection
			if err := json.Unmarshal(input, &conn); err != nil || conn.Username != "project_user" || conn.Password != "sensitive-password" || conn.Database != "project_app" {
				t.Fatal("creation did not preserve project connection settings")
			}
			return []byte(tc.output), nil
		})
		if (err != nil) != tc.wantError || (err != nil && strings.Contains(err.Error(), "sensitive")) {
			t.Fatal("database creation result was not classified safely")
		}
	}
}

func TestDatabaseCancellationStopsBeforeExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	execute := func(context.Context, string, []byte) ([]byte, error) {
		t.Fatal("canceled database work executed")
		return nil, nil
	}
	if _, err := ProbeDatabase(ctx, localDatabase(), execute); !errors.Is(err, context.Canceled) {
		t.Fatal("database probe did not preserve cancellation")
	}
	if err := CreateDatabase(ctx, localDatabase(), execute); !errors.Is(err, context.Canceled) {
		t.Fatal("database creation did not preserve cancellation")
	}
}

// A PHP runtime is optional in CI. This exercises the fixed scripts against a
// PDO fixture without a real database or executing any project PHP.
func TestDatabaseScriptsExecuteOnlyProbeAndQuotedCreateSQL(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("PHP runtime unavailable")
	}
	for _, action := range []string{"probe", "create"} {
		queries := []string{}
		execute := func(ctx context.Context, script string, input []byte) ([]byte, error) {
			fixture := `namespace SiteDoctorFixture; class PDO { const ATTR_ERRMODE=3; const ERRMODE_EXCEPTION=2; const ATTR_TIMEOUT=2; public static function getAvailableDrivers() { return ['mysql','pgsql']; } public function __construct($dsn,$user,$password,$options=[]) { if($user!=='project_user'||$password!=='sensitive-password') throw new \Exception('wrong credentials'); } public function query($sql) { fwrite(STDERR,$sql."\n"); return true; } public function exec($sql) { fwrite(STDERR,$sql."\n"); return true; } } `
			cmd := exec.CommandContext(ctx, php, "-n", "-r", fixture+script)
			cmd.Stdin = strings.NewReader(string(input))
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			queries = strings.Split(strings.TrimSpace(stderr.String()), "\n")
			return out, err
		}
		if action == "probe" {
			state, err := ProbeDatabase(context.Background(), localDatabase(), execute)
			if err != nil || state.Status != "exists" || len(queries) != 1 || queries[0] != "SELECT 1" {
				t.Fatal("PDO probe did not execute exactly SELECT 1")
			}
		} else {
			err := CreateDatabase(context.Background(), localDatabase(), execute)
			if err != nil || len(queries) != 1 || queries[0] != "CREATE DATABASE IF NOT EXISTS `project_app` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci" {
				t.Fatal("PDO creation did not execute the expected quoted statement")
			}
		}
	}
}

func TestPostgresPDOConnectionFailureCanIdentifyMissingDatabase(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("PHP runtime unavailable")
	}
	conn := localDatabase()
	conn.Driver, conn.Port = "pgsql", 5432
	state, err := ProbeDatabase(context.Background(), conn, func(ctx context.Context, script string, input []byte) ([]byte, error) {
		fixture := `namespace SiteDoctorFixture; class PDOException extends \Exception { public $errorInfo=['08006',7]; public function __construct() { parent::__construct('SQLSTATE[08006] [7] FATAL: database "project_app" does not exist'); $this->code='08006'; } } class PDO { const ATTR_ERRMODE=3; const ERRMODE_EXCEPTION=2; const ATTR_TIMEOUT=2; public static function getAvailableDrivers() { return ['pgsql']; } public function __construct($dsn,$user,$password,$options=[]) { throw new PDOException(); } } `
		cmd := exec.CommandContext(ctx, php, "-n", "-r", fixture+script)
		cmd.Stdin = strings.NewReader(string(input))
		return cmd.Output()
	})
	if err != nil || state.Status != "missing" || state.Code != "database_missing" {
		t.Fatal("PostgreSQL PDO missing database was not classified correctly")
	}
}

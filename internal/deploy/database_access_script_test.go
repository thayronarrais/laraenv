package deploy

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func bashTestPath(path string) string {
	path = filepath.ToSlash(path)
	if len(path) > 2 && path[1] == ':' {
		return "/" + strings.ToLower(path[:1]) + path[2:]
	}
	return path
}

type accessSandbox struct{ root, bash string }

func newAccessSandbox(t *testing.T) accessSandbox {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if runtime.GOOS == "windows" {
		bash = `C:\Program Files\Git\bin\bash.exe`
		_, err = os.Stat(bash)
	}
	if err != nil {
		t.Skip("bash required for remote script tests")
	}
	s := accessSandbox{root: t.TempDir(), bash: bash}
	for _, dir := range []string{"bin", "etc/mysql/conf.d", "etc/postgresql/16/main/conf.d", "var/www/my-app"} {
		if err := os.MkdirAll(filepath.Join(s.root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	s.write(t, "var/www/my-app/.env", "DB_CONNECTION=mysql\nDB_DATABASE=my_app_db\nDB_USERNAME=my_app\nDB_PASSWORD=existing-password\n")
	s.write(t, "etc/postgresql/16/main/pg_hba.conf", "local all all peer\nhost all all 0.0.0.0/0 trust\n")
	s.write(t, "etc/postgresql/16/main/postgresql.conf", "include_dir = 'conf.d'\n")
	commands := map[string]string{
		"systemctl": `printf '%s\n' "$*" >> "$MOCK_ROOT/services"`,
		"flock":     `exit 0`,
		"mysql": `printf '%s\n' "$*" >> "$MOCK_ROOT/sql"
if [[ "$*" == *"SHOW CREATE USER"* ]]; then
  printf "my_app\tCREATE USER 'my_app'@'localhost' IDENTIFIED WITH 'caching_sha2_password' AS '*hash'\n"
elif [[ "$*" == *"SELECT COUNT(*)"* ]]; then echo 0
elif [[ "$*" == *"@@partial_revokes"* ]]; then if [[ -f "$MOCK_ROOT/partial-revokes" ]]; then echo 1; else echo 0; fi
elif [[ "$*" != *" -e "* ]]; then cat >> "$MOCK_ROOT/sql"; fi`,
		"psql": `printf '%s\n' "$*" >> "$MOCK_ROOT/sql"
if [[ "$*" == *"SHOW hba_file"* ]]; then echo "$MOCK_ROOT/etc/postgresql/16/main/pg_hba.conf";
elif [[ "$*" == *"SHOW config_file"* ]]; then echo "$MOCK_ROOT/etc/postgresql/16/main/postgresql.conf";
elif [[ "$*" == *"pg_get_userbyid"* ]]; then echo my_app;
elif [[ "$*" == *"SELECT EXISTS"* ]]; then
  if [[ "$*" == *"pg_roles"* && -f "$MOCK_ROOT/pg-role-created" ]] || [[ "$*" == *"pg_database"* && -f "$MOCK_ROOT/pg-db-created" ]]; then echo t; else echo f; fi
elif [[ "$*" == *"rolsuper"* ]]; then if [[ -f "$MOCK_ROOT/role-admin" ]]; then echo t; else echo f; fi
elif [[ "$*" == *"SELECT 1"* ]]; then echo 1;
else
  input=$(cat); printf '%s\n' "$input" >> "$MOCK_ROOT/sql"
  if [[ "$input" == *"CREATE ROLE"* ]]; then touch "$MOCK_ROOT/pg-role-created"; fi
  if [[ "$input" == *"CREATE DATABASE"* ]]; then
    if [[ -f "$MOCK_ROOT/fail-create-db-once" ]]; then rm "$MOCK_ROOT/fail-create-db-once"; echo 'database creation failed' >&2; exit 1; fi
    touch "$MOCK_ROOT/pg-db-created"
  fi
  if [[ "$input" == *"pg_terminate_backend"* && "$input" == *"WHERE datname='my_app_db'"* ]]; then rm -f "$MOCK_ROOT/pg-active-sessions"; fi
  if [[ "$input" == *"DROP DATABASE"* ]]; then
    if [[ -f "$MOCK_ROOT/pg-active-sessions" ]]; then echo 'database is being accessed by other users' >&2; exit 1; fi
    rm -f "$MOCK_ROOT/pg-db-created"
  fi
  if [[ "$input" == *"DROP ROLE"* ]]; then rm -f "$MOCK_ROOT/pg-role-created"; fi
fi`,
		"runuser": `shift 3; exec "$@"`,
		"ufw": `if [[ "$1" == status ]]; then
  if [[ -f "$MOCK_ROOT/inactive" ]]; then echo 'Status: inactive'; else echo 'Status: active'; fi
  [[ "$2" != numbered ]] || cat "$MOCK_ROOT/rules" 2>/dev/null || true
else printf '%s\n' "$*" >> "$MOCK_ROOT/firewall"; fi`,
	}
	for name, body := range commands {
		s.write(t, "bin/"+name, "#!/usr/bin/env bash\n"+body+"\n")
	}
	return s
}

func (s accessSandbox) write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.root, filepath.FromSlash(path)), []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
}

func (s accessSandbox) read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (s accessSandbox) run(t *testing.T, step Step) (string, error) {
	t.Helper()
	root := bashTestPath(s.root)
	script := strings.NewReplacer("/etc/", root+"/etc/", "/var/www/", root+"/var/www/").Replace(step.Cmd)
	s.write(t, "run.sh", "export MOCK_ROOT="+shQuote(root)+"\nexport PATH="+shQuote(root+"/bin")+":$PATH\n"+script)
	cmd := exec.Command(s.bash, bashTestPath(filepath.Join(s.root, "run.sh")))
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestDatabaseAccessScriptsEnableRestrictAndRevoke(t *testing.T) {
	for _, engine := range []string{"mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			s := newAccessSandbox(t)
			cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
			cfg.Database = engine
			if engine == "postgres" {
				s.write(t, "var/www/my-app/.env", "DB_CONNECTION=pgsql\nDB_DATABASE=my_app_db\nDB_USERNAME=my_app\nDB_PASSWORD=existing-password\n")
			}
			step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
			if out, err := s.run(t, step); err != nil {
				t.Fatalf("enable: %v\n%s", err, out)
			}
			if firewall := s.read(t, "firewall"); !strings.Contains(firewall, "from 203.0.113.42") || strings.Contains(firewall, "from any") {
				t.Fatalf("unrestricted firewall: %s", firewall)
			}
			if engine == "mysql" {
				queries := s.read(t, "sql")
				if !strings.Contains(queries, "'my_app'@'203.0.113.42'") || strings.Contains(queries, "@'%'") {
					t.Fatalf("incorrect remote grants: %s", queries)
				}
			} else {
				hba := s.read(t, "etc/postgresql/16/main/pg_hba.conf")
				allow, reject, existing := strings.Index(hba, "203.0.113.42/32 scram-sha-256"), strings.Index(hba, "0.0.0.0/0 reject"), strings.Index(hba, "0.0.0.0/0 trust")
				if allow < 0 || reject < allow || existing < reject {
					t.Fatalf("HBA rules do not restrict existing permissive access: %s", hba)
				}
			}
			s.write(t, "rules", "[ 2] 3306/tcp ALLOW IN 203.0.113.42 # laraenv-db-"+engine+"-my-app\n[ 3] 22/tcp ALLOW IN Anywhere # unrelated\n")
			cfg = accessConfig(t, `"database_remote_access":false`)
			cfg.Database = engine
			step = planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
			if out, err := s.run(t, step); err != nil {
				t.Fatalf("disable: %v\n%s", err, out)
			}
			if _, err := os.Stat(filepath.Join(s.root, "etc/laraenv/database-access", engine, "my-app.remote")); !os.IsNotExist(err) {
				t.Fatal("remote-access marker was not removed")
			}
			firewall := s.read(t, "firewall")
			if !strings.Contains(firewall, "--force delete 2") || strings.Contains(firewall, "--force delete 3") {
				t.Fatalf("did not revoke only this project's firewall rule: %s", firewall)
			}
			if engine == "mysql" {
				if !strings.Contains(s.read(t, "sql"), "DROP USER IF EXISTS 'my_app'@'203.0.113.42'") {
					t.Fatal("remote MySQL account not revoked")
				}
				if !strings.Contains(s.read(t, "etc/mysql/conf.d/zz-laraenv-access.cnf"), "bind-address = 127.0.0.1") {
					t.Fatal("MySQL was not returned to local access")
				}
			} else {
				if strings.Contains(s.read(t, "etc/postgresql/16/main/pg_hba.conf"), "203.0.113.42/32") {
					t.Fatal("remote PostgreSQL rule not revoked")
				}
			}
		})
	}
}

func TestDatabaseAccessRequiresActiveFirewallBeforeOpeningListener(t *testing.T) {
	s := newAccessSandbox(t)
	s.write(t, "inactive", "")
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
	if _, err := s.run(t, step); err == nil {
		t.Fatal("remote access enabled without an active firewall")
	}
	if _, err := os.Stat(filepath.Join(s.root, "etc/mysql/conf.d/zz-laraenv-access.cnf")); !os.IsNotExist(err) {
		t.Fatal("listener changed before firewall prerequisite passed")
	}
}

func TestDatabaseAccessKeepsOtherDeploymentsRemote(t *testing.T) {
	s := newAccessSandbox(t)
	dir := filepath.Join(s.root, "etc/laraenv/database-access/mysql")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s.write(t, "etc/laraenv/database-access/mysql/other-app.remote", "")
	cfg := accessConfig(t, `"database_remote_access":false`)
	step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
	if out, err := s.run(t, step); err != nil {
		t.Fatalf("disable with another remote project: %v\n%s", err, out)
	}
	if !strings.Contains(s.read(t, "etc/mysql/conf.d/zz-laraenv-access.cnf"), "bind-address = *") {
		t.Fatal("another project's listener was closed")
	}
}

func TestDatabaseAccessRevokesPreviousEngineWhenDatabaseIsRemovedOrChanged(t *testing.T) {
	for _, selected := range []string{"", "postgres"} {
		t.Run(selected, func(t *testing.T) {
			s := newAccessSandbox(t)
			cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
			step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
			if out, err := s.run(t, step); err != nil {
				t.Fatalf("enable: %v\n%s", err, out)
			}
			cfg = accessConfig(t, `"database_remote_access":false`)
			cfg.Database = selected
			cleanup := planStep(t, BuildPlan(cfg, nil, "", true), "Revoke previous mysql database access")
			if out, err := s.run(t, cleanup); err != nil {
				t.Fatalf("cleanup: %v\n%s", err, out)
			}
			if _, err := os.Stat(filepath.Join(s.root, "etc/laraenv/database-access/mysql/my-app.remote")); !os.IsNotExist(err) {
				t.Fatal("previous engine remains remotely accessible")
			}
			if !strings.Contains(s.read(t, "sql"), "DROP USER IF EXISTS 'my_app'@'203.0.113.42'") {
				t.Fatal("previous remote account remains")
			}
		})
	}
}

func TestDatabaseAccessDestroyRevokesBeforeDroppingDatabase(t *testing.T) {
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	steps := BuildDestroyPlan(cfg)
	revoke, drop := -1, -1
	for i, step := range steps {
		if strings.Contains(step.Title, "Revoke previous mysql database access") {
			revoke = i
		}
		if strings.Contains(step.Title, "Drop MySQL DB") {
			drop = i
		}
	}
	if revoke < 0 || drop < 0 || revoke > drop {
		t.Fatalf("destroy leaves remote permissions or revokes too late: %v", stepTitles(steps))
	}
}

func TestDatabaseAccessMySQLUsesFinalIncludeDirectory(t *testing.T) {
	s := newAccessSandbox(t)
	if err := os.MkdirAll(filepath.Join(s.root, "etc/mysql/mysql.conf.d"), 0700); err != nil {
		t.Fatal(err)
	}
	s.write(t, "etc/mysql/mysql.conf.d/mysqld.cnf", "[mysqld]\nbind-address=127.0.0.1\n")
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
	if out, err := s.run(t, step); err != nil {
		t.Fatalf("enable: %v\n%s", err, out)
	}
	if !strings.Contains(s.read(t, "etc/mysql/mysql.conf.d/zz-laraenv-access.cnf"), "bind-address = *") {
		t.Fatal("MySQL's later include overrides the remote listener")
	}
}

func TestDatabaseAccessScriptsEmitUnixLineEndingsFromWindowsResources(t *testing.T) {
	common, mysql := databaseAccessCommon, databaseAccessMySQL
	defer func() { databaseAccessCommon, databaseAccessMySQL = common, mysql }()
	for _, tc := range []struct{ name, ending string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			// Normalize the checkout first so CRLF inputs do not become CRCRLF.
			databaseAccessCommon = strings.ReplaceAll(strings.ReplaceAll(common, "\r\n", "\n"), "\n", tc.ending)
			databaseAccessMySQL = strings.ReplaceAll(strings.ReplaceAll(mysql, "\r\n", "\n"), "\n", tc.ending)
			cfg := accessConfig(t, `"database_remote_access":false`)
			step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
			if strings.Contains(step.Cmd, "\r") {
				t.Fatal("Windows resources emit carriage returns into the Linux SSH command")
			}
		})
	}
}

func TestDatabaseAccessPostgresBootstrapCreatesCredentialsForFreshLaravel(t *testing.T) {
	s := newAccessSandbox(t)
	s.write(t, "var/www/my-app/.env", "APP_KEY=preserve\nDB_CONNECTION=sqlite\n# DB_DATABASE=laravel\n# DB_USERNAME=root\n# DB_PASSWORD=\n")
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	cfg.Database = "postgres"
	bootstrap := planStep(t, BuildPlan(cfg, nil, "", true), "Bootstrap PostgreSQL DB + user")
	if out, err := s.run(t, bootstrap); err != nil {
		t.Fatalf("bootstrap: %v\n%s", err, out)
	}
	env := s.read(t, "var/www/my-app/.env")
	for _, want := range []string{"APP_KEY=preserve", "DB_CONNECTION=pgsql", "DB_HOST=127.0.0.1", "DB_PORT=5432", "DB_DATABASE=my_app_db", "DB_USERNAME=my_app"} {
		if !strings.Contains(env, want) {
			t.Fatalf("missing %s in .env: %s", want, env)
		}
	}
	queries := s.read(t, "sql")
	if !strings.Contains(queries, `CREATE ROLE "my_app" LOGIN PASSWORD`) || !strings.Contains(queries, `CREATE DATABASE "my_app_db" OWNER "my_app"`) {
		t.Fatalf("project role/database not created: %s", queries)
	}
}

func TestDatabaseAccessPostgresBootstrapPreservesWorkingCredentials(t *testing.T) {
	s := newAccessSandbox(t)
	previous := "APP_KEY=preserve\nDB_CONNECTION=pgsql\nDB_DATABASE=my_app_db\nDB_USERNAME=my_app\nDB_PASSWORD=working-secret\n"
	s.write(t, "var/www/my-app/.env", previous)
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	cfg.Database = "postgres"
	bootstrap := planStep(t, BuildPlan(cfg, nil, "", true), "Bootstrap PostgreSQL DB + user")
	if out, err := s.run(t, bootstrap); err != nil {
		t.Fatalf("reapply: %v\n%s", err, out)
	}
	if s.read(t, "var/www/my-app/.env") != previous {
		t.Fatal("reapplying overwrote existing credentials")
	}
	if strings.Contains(s.read(t, "sql"), "CREATE ROLE") {
		t.Fatal("reapplying recreated the project role")
	}
}

// Evaluate the first matching host record, independently of the script builder.
func postgresHBAAllows(t *testing.T, hba, database, user, address string) bool {
	t.Helper()
	for _, line := range strings.Split(hba, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "host" {
			continue
		}
		if fields[1] != "all" && strings.Trim(fields[1], "\"") != database {
			continue
		}
		if fields[2] != "all" && strings.Trim(fields[2], "\"") != user {
			continue
		}
		_, network, err := net.ParseCIDR(fields[3])
		if err != nil {
			t.Fatal(err)
		}
		if network.Contains(net.ParseIP(address)) {
			return fields[4] != "reject"
		}
	}
	return false
}

func TestDatabaseAccessPostgresRestrictsRoleToProjectDatabase(t *testing.T) {
	s := newAccessSandbox(t)
	s.write(t, "var/www/my-app/.env", "DB_CONNECTION=pgsql\nDB_DATABASE=my_app_db\nDB_USERNAME=my_app\nDB_PASSWORD=existing-password\n")
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	cfg.Database = "postgres"
	step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
	if out, err := s.run(t, step); err != nil {
		t.Fatalf("enable: %v\n%s", err, out)
	}
	hba := s.read(t, "etc/postgresql/16/main/pg_hba.conf")
	if !postgresHBAAllows(t, hba, "my_app_db", "my_app", "203.0.113.42") {
		t.Fatal("allowed client rejected")
	}
	if postgresHBAAllows(t, hba, "my_app_db", "my_app", "198.51.100.8") {
		t.Fatal("unlisted IP allowed")
	}
	if postgresHBAAllows(t, hba, "another_db", "my_app", "203.0.113.42") {
		t.Fatal("project role can remotely access another database")
	}
}

func TestDatabaseAccessPostgresDoesNotReopenPreviousIdentityOnCredentialChange(t *testing.T) {
	s := newAccessSandbox(t)
	s.write(t, "var/www/my-app/.env", "DB_CONNECTION=pgsql\nDB_DATABASE=my_app_db\nDB_USERNAME=my_app\nDB_PASSWORD=existing-password\n")
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	cfg.Database = "postgres"
	step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
	if out, err := s.run(t, step); err != nil {
		t.Fatalf("first apply: %v\n%s", err, out)
	}
	s.write(t, "var/www/my-app/.env", "DB_CONNECTION=pgsql\nDB_DATABASE=another_db\nDB_USERNAME=another_user\nDB_PASSWORD=existing-password\n")
	if out, err := s.run(t, step); err != nil {
		t.Fatalf("reapply: %v\n%s", err, out)
	}
	hba := s.read(t, "etc/postgresql/16/main/pg_hba.conf")
	if postgresHBAAllows(t, hba, "my_app_db", "my_app", "203.0.113.42") {
		t.Fatal("old identity is still remotely accessible after the credentials changed")
	}
	if !postgresHBAAllows(t, hba, "another_db", "another_user", "203.0.113.42") {
		t.Fatal("new identity is not remotely accessible")
	}
}

func TestDatabaseAccessRejectsSystemDatabaseGrants(t *testing.T) {
	s := newAccessSandbox(t)
	s.write(t, "var/www/my-app/.env", "DB_DATABASE=mysql\nDB_USERNAME=my_app\nDB_PASSWORD=secret\n")
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
	if out, err := s.run(t, step); err == nil {
		t.Fatalf("remote grants to system database allowed: %s", out)
	}
}

func TestDatabaseAccessPostgresRejectsPrivilegedRole(t *testing.T) {
	s := newAccessSandbox(t)
	s.write(t, "role-admin", "")
	s.write(t, "var/www/my-app/.env", "DB_CONNECTION=pgsql\nDB_DATABASE=my_app_db\nDB_USERNAME=my_app\nDB_PASSWORD=secret\n")
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	cfg.Database = "postgres"
	step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
	if out, err := s.run(t, step); err == nil {
		t.Fatalf("privileged role exposed remotely: %s", out)
	}
	if hba := s.read(t, "etc/postgresql/16/main/pg_hba.conf"); strings.Contains(hba, "LARAENV DATABASE ACCESS") {
		t.Fatal("HBA changed before role validation")
	}
}

func TestDatabaseAccessMySQLGrantsOnlyTheExactProjectSchema(t *testing.T) {
	for _, literal := range []bool{false, true} {
		t.Run(map[bool]string{false: "wildcard-mode", true: "literal-mode"}[literal], func(t *testing.T) {
			s := newAccessSandbox(t)
			if literal {
				s.write(t, "partial-revokes", "")
			}
			cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
			step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
			if out, err := s.run(t, step); err != nil {
				t.Fatalf("enable: %v\n%s", err, out)
			}
			want := "GRANT ALL ON `my\\_app\\_db`.*"
			if literal {
				want = "GRANT ALL ON `my_app_db`.*"
			}
			if queries := s.read(t, "sql"); !strings.Contains(queries, want) {
				t.Fatalf("grant can match another schema or fails in literal mode: %s", queries)
			}
		})
	}
}

func TestDatabaseAccessPostgresTreatsKeywordNamesAsLiteralIdentifiers(t *testing.T) {
	for _, identity := range []struct{ database, user string }{{"all", "my_app"}, {"my_app_db", "all"}} {
		t.Run(identity.database+"-"+identity.user, func(t *testing.T) {
			s := newAccessSandbox(t)
			s.write(t, "var/www/my-app/.env", "DB_CONNECTION=pgsql\nDB_DATABASE="+identity.database+"\nDB_USERNAME="+identity.user+"\nDB_PASSWORD=secret\n")
			cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
			cfg.Database = "postgres"
			step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
			if out, err := s.run(t, step); err != nil {
				t.Fatalf("enable: %v\n%s", err, out)
			}
			hba := s.read(t, "etc/postgresql/16/main/pg_hba.conf")
			if !postgresHBAAllows(t, hba, identity.database, identity.user, "203.0.113.42") {
				t.Fatal("target identity rejected")
			}
			if postgresHBAAllows(t, hba, "another_db", identity.user, "203.0.113.42") {
				t.Fatal("all database name was treated as a wildcard")
			}
			if !postgresHBAAllows(t, hba, "another_db", "other_user", "198.51.100.8") {
				t.Fatal("all role name created global rejection for unrelated roles")
			}
		})
	}
}

func TestDatabaseAccessPostgresRecoversInterruptedBootstrap(t *testing.T) {
	s := newAccessSandbox(t)
	s.write(t, "var/www/my-app/.env", "APP_KEY=preserve\nDB_CONNECTION=sqlite\n")
	s.write(t, "fail-create-db-once", "")
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	cfg.Database = "postgres"
	step := planStep(t, BuildPlan(cfg, nil, "", true), "Bootstrap PostgreSQL DB + user")
	if _, err := s.run(t, step); err == nil {
		t.Fatal("fixture must fail database creation after creating role")
	}
	if out, err := s.run(t, step); err != nil {
		t.Fatalf("cannot resume bootstrap without lost credentials: %v\n%s", err, out)
	}
	if !strings.Contains(s.read(t, "var/www/my-app/.env"), "DB_CONNECTION=pgsql") {
		t.Fatal("recovered credentials were not saved")
	}
}

func TestDatabaseAccessDestroyRemovesOnlyOwnedPostgresResources(t *testing.T) {
	s := newAccessSandbox(t)
	s.write(t, "var/www/my-app/.env", "APP_KEY=preserve\nDB_CONNECTION=sqlite\n")
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	cfg.Database = "postgres"
	bootstrap := planStep(t, BuildPlan(cfg, nil, "", true), "Bootstrap PostgreSQL DB + user")
	if out, err := s.run(t, bootstrap); err != nil {
		t.Fatalf("bootstrap: %v\n%s", err, out)
	}
	drop := planStep(t, BuildDestroyPlan(cfg), "Drop managed PostgreSQL DB + user")
	s.write(t, "pg-active-sessions", "")
	if out, err := s.run(t, drop); err != nil {
		t.Fatalf("destroy: %v\n%s", err, out)
	}
	for _, resource := range []string{"pg-role-created", "pg-db-created"} {
		if _, err := os.Stat(filepath.Join(s.root, resource)); !os.IsNotExist(err) {
			t.Fatalf("destroy left %s", resource)
		}
	}
	queries := s.read(t, "sql")
	if out, err := s.run(t, drop); err != nil {
		t.Fatalf("repeat destroy: %v\n%s", err, out)
	}
	if s.read(t, "sql") != queries {
		t.Fatal("destroy dropped a database without an ownership record")
	}
}

func TestDatabaseAccessPostgresRevokesExistingRemoteSessions(t *testing.T) {
	s := newAccessSandbox(t)
	s.write(t, "var/www/my-app/.env", "DB_CONNECTION=pgsql\nDB_DATABASE=my_app_db\nDB_USERNAME=my_app\nDB_PASSWORD=secret\n")
	cfg := accessConfig(t, `"database_remote_access":true,"database_allowed_ips":["203.0.113.42"]`)
	cfg.Database = "postgres"
	step := planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
	if out, err := s.run(t, step); err != nil {
		t.Fatalf("enable: %v\n%s", err, out)
	}
	s.write(t, "etc/laraenv/database-access/postgres/other-app.remote", "")
	s.write(t, "sql", "")
	cfg = accessConfig(t, `"database_remote_access":false`)
	cfg.Database = "postgres"
	step = planStep(t, BuildPlan(cfg, nil, "", true), "Configure database access")
	if out, err := s.run(t, step); err != nil {
		t.Fatalf("disable: %v\n%s", err, out)
	}
	queries := s.read(t, "sql")
	for _, predicate := range []string{"pg_terminate_backend(pid)", "usename IN ('my_app')", "client_addr IS NOT NULL", "127.0.0.0/8", "::1"} {
		if !strings.Contains(queries, predicate) {
			t.Fatalf("revocation can leave sessions active or affect other projects: missing %s in %s", predicate, queries)
		}
	}
}

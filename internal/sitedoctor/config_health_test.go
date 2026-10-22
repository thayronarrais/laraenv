package sitedoctor

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLaravelDatabaseLiteralInvalidPortHasSafeCode(t *testing.T) {
	for _, port := range []string{"invalid", "0", "-1", "65536", "999999999999999999999999"} {
		t.Run(port, func(t *testing.T) {
			path := envFixture(t, ".env", "DB_CONNECTION=mysql\nDB_DATABASE=demo\nDB_PORT="+port+"\nDB_PASSWORD=private-password\n")
			conn, err := LaravelDatabase(filepath.Dir(path))
			if err == nil || err.Error() != "database_port_invalid" || conn != (DatabaseConnection{}) {
				t.Fatal("invalid literal port was not rejected with a safe stable code")
			}
		})
	}
}

func TestWordPressDatabaseLiteralInvalidPortHasSafeCode(t *testing.T) {
	for _, host := range []string{"localhost:invalid", "localhost:0", "localhost:-1", "localhost:65536", "[::1]:invalid", "[::1]:65536"} {
		t.Run(host, func(t *testing.T) {
			contents := "<?php define('DB_NAME','demo'); define('DB_USER','site_user'); define('DB_PASSWORD','private-password'); define('DB_HOST','" + host + "');"
			conn, err := WordPressDatabase(filepath.Dir(envFixture(t, "wp-config.php", contents)))
			if err == nil || err.Error() != "database_port_invalid" || conn != (DatabaseConnection{}) {
				t.Fatal("invalid literal WordPress port was not rejected with a safe stable code")
			}
		})
	}
}

func TestDatabaseLiteralValidPortBoundariesArePreserved(t *testing.T) {
	for _, tc := range []struct {
		port string
		want int
	}{{"1", 1}, {"65535", 65535}} {
		t.Run(tc.port, func(t *testing.T) {
			root := filepath.Dir(envFixture(t, ".env", "DB_CONNECTION=mysql\nDB_DATABASE=demo\nDB_PORT="+tc.port+"\n"))
			conn, err := LaravelDatabase(root)
			if err != nil || conn.Port != tc.want {
				t.Fatal("valid Laravel port was rejected or replaced")
			}
			contents := "<?php define('DB_NAME','demo'); define('DB_USER','site_user'); define('DB_PASSWORD',''); define('DB_HOST','localhost:" + tc.port + "');"
			root = filepath.Dir(envFixture(t, "wp-config.php", contents))
			conn, err = WordPressDatabase(root)
			if err != nil || conn.Port != tc.want {
				t.Fatal("valid WordPress port was rejected or replaced")
			}
		})
	}
}

func TestCheckLiteralInvalidDatabasePortNeedsManualAttentionWithoutProbe(t *testing.T) {
	for _, tc := range []struct {
		name, kind, configFile, contents string
	}{
		{"laravel-malformed", "laravel", ".env", "APP_KEY=key\nDB_CONNECTION=mysql\nDB_DATABASE=demo\nDB_PORT=invalid\n"},
		{"laravel-zero", "laravel", ".env", "APP_KEY=key\nDB_CONNECTION=mysql\nDB_DATABASE=demo\nDB_PORT=0\n"},
		{"laravel-out-of-range", "laravel", ".env", "APP_KEY=key\nDB_CONNECTION=mysql\nDB_DATABASE=demo\nDB_PORT=65536\n"},
		{"wordpress-malformed", "wordpress", "wp-config.php", "<?php define('DB_NAME','demo'); define('DB_USER','site_user'); define('DB_PASSWORD','private-password'); define('DB_HOST','localhost:invalid');"},
		{"wordpress-out-of-range", "wordpress", "wp-config.php", "<?php define('DB_NAME','demo'); define('DB_USER','site_user'); define('DB_PASSWORD','private-password'); define('DB_HOST','localhost:65536');"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, deps := healthyDoctorTarget(t)
			target.Project.Kind = tc.kind
			doctorFile(t, filepath.Join(target.Project.Path, tc.configFile), tc.contents)
			deps.Database = func(context.Context, DatabaseConnection) (DatabaseState, error) {
				t.Fatal("invalid literal port reached the database probe")
				return DatabaseState{}, nil
			}
			report, err := Check(context.Background(), target, deps)
			if err != nil {
				t.Fatal(err)
			}
			f := doctorFinding(t, report, "database")
			if f.Code != "database_port_invalid" || f.Status != "warning" || f.Severity != "warning" || f.FixID != "" || report.Healthy {
				t.Fatalf("invalid literal database port hidden: finding=%+v healthy=%v", f, report.Healthy)
			}
		})
	}
}

func TestDatabaseOpaqueConfigurationIsNotMisclassifiedAsInvalidPort(t *testing.T) {
	for _, contents := range []string{
		"APP_KEY=key\nDB_CONNECTION=mysql\nDB_DATABASE=demo\nDB_PORT=${DATABASE_PORT}\n",
		"APP_KEY=key\nDB_CONNECTION=mysql\nDB_DATABASE=demo\nDB_PORT=3306\nDB_SOCKET=/private/mysql.sock\n",
	} {
		root := filepath.Dir(envFixture(t, ".env", contents))
		if _, err := LaravelDatabase(root); err == nil || err.Error() != "database_config_unsupported" {
			t.Fatal("opaque Laravel configuration was classified as a proven port error")
		}
	}
	root := filepath.Dir(envFixture(t, ".env", "DB_CONNECTION=mysql\nDB_DATABASE=demo\nDB_PORT=invalid\n"))
	doctorFile(t, filepath.Join(root, "bootstrap", "cache", "config.php"), "<?php return [];")
	if _, err := LaravelDatabase(root); err == nil || err.Error() != "database_config_unsupported" {
		t.Fatal("cached configuration was treated as an authoritative .env port error")
	}
	for _, host := range []string{"localhost:", "localhost:/private/mysql.sock", "2001:db8::1", "localhost:3306:ambiguous"} {
		contents := "<?php define('DB_NAME','demo'); define('DB_USER','site_user'); define('DB_PASSWORD','private-password'); define('DB_HOST','" + host + "');"
		root := filepath.Dir(envFixture(t, "wp-config.php", contents))
		if _, err := WordPressDatabase(root); err == nil || err.Error() != "wordpress_config_unsupported" {
			t.Fatal("opaque WordPress host was classified as a proven port error")
		}
	}
}

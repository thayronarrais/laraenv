package sitedoctor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func envFixture(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadEnvFileLiteralSyntax(t *testing.T) {
	path := envFixture(t, ".env", "\ufeff# ignored\r\n export APP_KEY = \"base64:abc==\" # key\r\nDB_DATABASE=first\nDB_DATABASE='my#app'\nDB_PASSWORD=\"p\\\"ass # word\"\nEMPTY=\nNULL=null\nQUOTED_NULL='null'\nMAIL_FROM_NAME=\"${APP_NAME}\"\nATTACHED=hello#world\n")
	got, err := ReadEnvFile(path)
	want := map[string]string{"APP_KEY": "base64:abc==", "DB_DATABASE": "my#app", "DB_PASSWORD": "p\"ass # word", "EMPTY": "", "NULL": "", "QUOTED_NULL": "null", "MAIL_FROM_NAME": "${APP_NAME}", "ATTACHED": "hello#world"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("literal env parsing did not preserve expected values")
	}
}

func TestReadEnvFileRejectsAmbiguityWithoutLeakingValues(t *testing.T) {
	for _, contents := range []string{"PASSWORD=\"sensitive-value\n", "PASSWORD='sensitive-value' unexpected\n", "not an assignment sensitive-value\n"} {
		_, err := ReadEnvFile(envFixture(t, ".env", contents))
		if err == nil || strings.Contains(err.Error(), "sensitive-value") {
			t.Fatal("ambiguous env error must be generic")
		}
	}
	_, err := ReadEnvFile(envFixture(t, ".env", strings.Repeat("A", 1024*1024+1)))
	if err == nil {
		t.Fatal("oversized env accepted")
	}
}

func TestLaravelDatabasePreservesConnectionAndCredentials(t *testing.T) {
	path := envFixture(t, ".env", "DB_CONNECTION=pgsql\nDB_HOST=localhost\nDB_PORT=5432\nDB_DATABASE=existing_app\nDB_USERNAME=project_user\nDB_PASSWORD='do not log # me'\nMAIL_FROM_NAME=\"${APP_NAME}\"\n")
	got, err := LaravelDatabase(filepath.Dir(path))
	want := DatabaseConnection{Driver: "pgsql", Host: "localhost", Port: 5432, Database: "existing_app", Username: "project_user", Password: "do not log # me"}
	if err != nil || got != want {
		t.Fatal("database parsing did not preserve expected connection settings")
	}
}

func TestLaravelDatabaseDoesNotReplaceExplicitEmptySettings(t *testing.T) {
	path := envFixture(t, ".env", "DB_CONNECTION=mysql\nDB_DATABASE=project_app\nDB_USERNAME=\nDB_PASSWORD=\nDB_HOST=\nDB_PORT=\n")
	conn, err := LaravelDatabase(filepath.Dir(path))
	if err != nil || conn.Username != "" || conn.Password != "" || conn.Host != "" || conn.Port != 0 || CanCreateDatabase(conn) {
		t.Fatal("explicit empty settings changed or repair offered")
	}
}

func TestLaravelDatabaseRejectsDynamicOrAmbiguousSettings(t *testing.T) {
	for _, contents := range []string{
		"DB_CONNECTION=mysql\nDB_DATABASE=${APP_NAME}\n",
		"DB_CONNECTION=mysql\nDB_URL=mysql://user:sensitive-value@host/db\n",
		"DB_CONNECTION=mysql\nDATABASE_URL=mysql://user:sensitive-value@host/db\n",
		"DB_CONNECTION=mysql\nDB_DATABASE=app\nDB_PORT=invalid\n",
		"DB_CONNECTION=mysql\nDB_DATABASE=app\nDB_SOCKET=/private/mysql.sock\n",
		"DB_CONNECTION=unknown\nDB_DATABASE=app\n",
	} {
		conn, err := LaravelDatabase(filepath.Dir(envFixture(t, ".env", contents)))
		if err == nil || conn != (DatabaseConnection{}) || strings.Contains(err.Error(), "sensitive-value") {
			t.Fatal("dynamic database must be rejected generically")
		}
	}
}

func TestLaravelDatabaseSQLiteDefault(t *testing.T) {
	path := envFixture(t, ".env", "APP_KEY=\nDB_CONNECTION=sqlite\n")
	got, err := LaravelDatabase(filepath.Dir(path))
	if err != nil || got.Driver != "sqlite" || got.Database != filepath.Join(filepath.Dir(path), "database", "database.sqlite") {
		t.Fatal("SQLite default path or driver was incorrect")
	}
}

func TestWordPressDatabaseReadsLiteralsWithoutExecutingPHP(t *testing.T) {
	path := envFixture(t, "wp-config.php", "<?php\n// define('DB_NAME', 'wrong');\ndefine( 'DB_NAME', 'wp_site' );\ndefine('DB_USER', 'site_user');\ndefine('DB_PASSWORD', 'secret\\'value');\ndefine('DB_HOST', 'localhost:3306');\nfile_put_contents('should-never-exist', 'bad');\n")
	got, err := WordPressDatabase(filepath.Dir(path))
	want := DatabaseConnection{Driver: "mysql", Database: "wp_site", Host: "localhost", Port: 3306, Username: "site_user", Password: "secret'value"}
	if err != nil || got != want {
		t.Fatal("WordPress parsing did not preserve expected connection settings")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "should-never-exist")); !os.IsNotExist(err) {
		t.Fatal("PHP configuration was executed")
	}
}

func TestWordPressDatabaseRejectsDynamicAndDuplicateValues(t *testing.T) {
	for _, password := range []string{"getenv('SECRET')", "'prefix' . 'sensitive-value'", "\"$sensitive_value\""} {
		contents := "<?php define('DB_NAME','app'); define('DB_USER','root'); define('DB_PASSWORD'," + password + "); define('DB_HOST','localhost');"
		conn, err := WordPressDatabase(filepath.Dir(envFixture(t, "wp-config.php", contents)))
		if err == nil || conn != (DatabaseConnection{}) || strings.Contains(err.Error(), "sensitive") {
			t.Fatal("dynamic wp config accepted or diagnostic leaked values")
		}
	}
	contents := "<?php define('DB_NAME','first'); define('DB_NAME','second'); define('DB_USER','root'); define('DB_PASSWORD',''); define('DB_HOST','localhost');"
	if _, err := WordPressDatabase(filepath.Dir(envFixture(t, "wp-config.php", contents))); err == nil {
		t.Fatal("duplicate DB definition accepted")
	}
}

func TestCopyEnvironmentExampleNeverOverwrites(t *testing.T) {
	path := envFixture(t, ".env.example", "APP_KEY=\n# keep exact bytes\r\n")
	root := filepath.Dir(path)
	if err := CopyEnvironmentExample(root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil || string(got) != "APP_KEY=\n# keep exact bytes\r\n" {
		t.Fatal("copy did not preserve exact example bytes")
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("APP_KEY=preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CopyEnvironmentExample(root); err == nil {
		t.Fatal("existing env must reject copy")
	}
	got, _ = os.ReadFile(filepath.Join(root, ".env"))
	if string(got) != "APP_KEY=preserve" {
		t.Fatal("existing env was overwritten")
	}
}

func TestWordPressDatabaseRejectsGuardedAndFunctionScopedTargets(t *testing.T) {
	for name, definition := range map[string]string{
		"conditional_block":     "if (false) { define('DB_NAME','wrong_target'); }",
		"conditional_statement": "if (false) define('DB_NAME','wrong_target');",
		"function":              "function unused() { define('DB_NAME','wrong_target'); }",
		"plain_block":           "{ define('DB_NAME','wrong_target'); }",
		"ternary":               "false ? define('DB_NAME','wrong_target') : null;",
	} {
		t.Run(name, func(t *testing.T) {
			contents := "<?php " + definition + " define('DB_USER','site_user'); define('DB_PASSWORD','private-password'); define('DB_HOST','localhost');"
			conn, err := WordPressDatabase(filepath.Dir(envFixture(t, "wp-config.php", contents)))
			if err == nil || conn != (DatabaseConnection{}) {
				t.Fatal("non-authoritative WordPress database target accepted")
			}
		})
	}
}

func TestWordPressDatabaseRejectsIncludesBeforeCompleteDatabaseDefinitions(t *testing.T) {
	for name, prefix := range map[string]string{
		"require":             "require __DIR__ . '/database-config.php';",
		"require_once":        "require_once __DIR__ . '/database-config.php';",
		"include":             "include '/database-config.php';",
		"include_once":        "include_once '/database-config.php';",
		"between_definitions": "define('DB_NAME','wrong_target'); require '/database-config.php';",
	} {
		t.Run(name, func(t *testing.T) {
			contents := "<?php " + prefix
			if name != "between_definitions" {
				contents += " define('DB_NAME','wrong_target');"
			}
			contents += " define('DB_USER','site_user'); define('DB_PASSWORD','private-password'); define('DB_HOST','localhost');"
			conn, err := WordPressDatabase(filepath.Dir(envFixture(t, "wp-config.php", contents)))
			if err == nil || conn != (DatabaseConnection{}) {
				t.Fatal("included configuration could override the selected database target")
			}
		})
	}
}

func TestWordPressDatabaseAcceptsStandardFinalBootstrap(t *testing.T) {
	contents := "<?php /* require 'ignored'; */ $note='include'; define('DB_NAME','wp_site'); define('DB_USER','site_user'); define('DB_PASSWORD','private-password'); define('DB_HOST','localhost'); $table_prefix='wp_'; if (!defined('ABSPATH')) { define('ABSPATH', __DIR__ . '/'); } require_once ABSPATH . 'wp-settings.php';"
	conn, err := WordPressDatabase(filepath.Dir(envFixture(t, "wp-config.php", contents)))
	if err != nil || conn.Database != "wp_site" || conn.Username != "site_user" || conn.Password != "private-password" {
		t.Fatal("standard WordPress literal configuration was rejected")
	}
}

func TestLaravelDatabaseRejectsCachedConfiguration(t *testing.T) {
	path := envFixture(t, ".env", "DB_CONNECTION=mysql\nDB_DATABASE=stale_target\nDB_USERNAME=site_user\nDB_PASSWORD=private-password\n")
	root := filepath.Dir(path)
	cache := filepath.Join(root, "bootstrap", "cache")
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "config.php"), []byte("<?php return ['database'=>['default'=>'production']];"), 0600); err != nil {
		t.Fatal(err)
	}
	conn, err := LaravelDatabase(root)
	if err == nil || err.Error() != "database_config_unsupported" || conn != (DatabaseConnection{}) {
		t.Fatal("cached Laravel connection must not be inferred from .env")
	}
}

func TestCopyEnvironmentExampleRejectsNonRegularAndLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".env.example"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := CopyEnvironmentExample(root); err == nil {
		t.Fatal("directory source accepted")
	}
	root = t.TempDir()
	target := envFixture(t, "outside", "secret")
	if err := os.Symlink(target, filepath.Join(root, ".env.example")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := CopyEnvironmentExample(root); err == nil {
		t.Fatal("linked source accepted")
	}
}

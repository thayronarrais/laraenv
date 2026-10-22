package projects

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"lara4ever/internal/i18n"
)

// PostSetupConfig carries everything the post-create hooks need.
type PostSetupConfig struct {
	ProjectsRoot string
	Database     string // "mysql" | "postgres" | "" (skip db)
	MySQLBin     string // e.g. C:\lara4ever\mysql\bin
	PostgresBin  string // e.g. C:\lara4ever\postgres\bin
	// NodeDir is the directory containing node.exe + npm.cmd for the selected
	// managed Node version (e.g. C:\lara4ever\nodejs\current). Empty falls
	// back to npm on PATH.
	NodeDir string
	// SkipFrontend prevents npm fallback when the project's Node requirement
	// cannot be satisfied. Database, .env and PHP setup still run.
	SkipFrontend bool
	// PHPExe is the absolute path to php.exe used to run artisan (migrate).
	// Empty skips the migrate step.
	PHPExe string
}

// PostSetupLaravel runs after composer has finished. It:
//  1. Creates the database matching the project name (best-effort).
//  2. Patches .env with stack-appropriate DB settings.
//  3. Runs `npm install` + `npm run build` if npm is on PATH.
//
// Each step emits log lines. Failures are logged but do not abort — the
// project still exists and can be fixed manually.
func PostSetupLaravel(ctx context.Context, cfg PostSetupConfig, projectName string, emit func(string)) {
	projectDir := filepath.Join(cfg.ProjectsRoot, projectName)

	// ---- Database ----
	dbName := strings.ReplaceAll(projectName, "-", "_")
	switch cfg.Database {
	case "mysql":
		emit(i18n.T("backend.projects.creatingMysqlDb", map[string]any{"name": dbName}))
		if err := createMySQLDatabase(ctx, cfg.MySQLBin, dbName, emit); err != nil {
			emit(i18n.T("backend.projects.warnGeneric", map[string]any{"error": err}))
		}
	case "postgres":
		emit(i18n.T("backend.projects.creatingPostgresDb", map[string]any{"name": dbName}))
		if err := createPostgresDatabase(ctx, cfg.PostgresBin, dbName, emit); err != nil {
			emit(i18n.T("backend.projects.warnGeneric", map[string]any{"error": err}))
		}
	default:
		emit(i18n.T("backend.projects.noDatabaseInStack", nil))
	}

	// ---- .env patch ----
	if err := patchLaravelEnv(projectDir, dbName, cfg.Database); err != nil {
		emit(i18n.T("backend.projects.envPatchFailedWarn", map[string]any{"error": err}))
	} else {
		emit(i18n.T("backend.projects.envUpdated", nil))
	}

	// ---- migrate + seed ----
	switch {
	case cfg.Database == "":
		emit(i18n.T("backend.projects.noDatabaseSkipMigrate", nil))
	case cfg.PHPExe == "":
		emit(i18n.T("backend.projects.noPhpForMigrations", nil))
	default:
		emit(i18n.T("backend.projects.runningMigrate", nil))
		if err := runStream(ctx, cfg.PHPExe,
			[]string{"artisan", "migrate", "--seed", "--force"},
			projectDir, emit); err != nil {
			emit(i18n.T("backend.projects.migrateFailedWarn", map[string]any{"error": err}))
		}
	}

	// ---- npm install + build ----
	if cfg.SkipFrontend {
		emit(i18n.T("backend.projects.skippingFrontendBuildNoNode", nil))
		return
	}
	npm := resolveNpm(cfg.NodeDir)
	if npm == "" {
		emit(i18n.T("backend.projects.skippingFrontendBuildNoNode", nil))
		return
	}
	emit(i18n.T("backend.projects.runningNpmInstall", map[string]any{"npm": npm}))
	if err := runStreamCommand(streamCommand(ctx, npm, []string{"install", "--no-fund", "--no-audit"}, projectDir, cfg.NodeDir), emit); err != nil {
		emit(i18n.T("backend.projects.npmInstallFailedWarn", map[string]any{"error": err}))
		return
	}
	emit(i18n.T("backend.projects.runningNpmBuild", nil))
	if err := runStreamCommand(streamCommand(ctx, npm, []string{"run", "build"}, projectDir, cfg.NodeDir), emit); err != nil {
		emit(i18n.T("backend.projects.npmBuildFailedWarn", map[string]any{"error": err}))
	}
}

// resolveNpm returns the absolute path to npm.cmd from the managed Node
// install if present, otherwise falls back to the first npm on PATH.
// Returns "" if no npm can be found.
func resolveNpm(nodeDir string) string {
	if nodeDir != "" {
		candidate := filepath.Join(nodeDir, "npm.cmd")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if p, err := exec.LookPath("npm"); err == nil {
		return p
	}
	return ""
}

// DropProjectDatabase best-effort drops the database a Laravel project used.
// The database engine + name are read from the project's .env (DB_CONNECTION,
// DB_DATABASE) for accuracy — this honors any name the user customized after
// creation. When .env is missing or silent, it falls back to stackEngine
// ("mysql"|"postgres"|"") and the creation-time derived name (project name
// with dashes → underscores). No-op if neither .env nor stackEngine yields a
// supported engine. Returns the dropped database name (empty if nothing was
// attempted) so the caller can report it.
func DropProjectDatabase(ctx context.Context, cfg PostSetupConfig, projectDir, projectName, stackEngine string, emit func(string)) (string, error) {
	conn, name := dbInfoFromEnv(projectDir)
	if name == "" {
		name = strings.ReplaceAll(projectName, "-", "_")
	}
	engine := stackEngine
	switch conn {
	case "mysql":
		engine = "mysql"
	case "pgsql":
		engine = "postgres"
	}
	switch engine {
	case "mysql":
		return name, dropMySQLDatabase(ctx, cfg.MySQLBin, name, emit)
	case "postgres":
		return name, dropPostgresDatabase(ctx, cfg.PostgresBin, name, emit)
	default:
		return "", nil
	}
}

// dbInfoFromEnv reads DB_CONNECTION and DB_DATABASE from <projectDir>/.env,
// honoring the last uncommented definition of each. Returns ("","") when the
// file is missing or the keys are unset.
func dbInfoFromEnv(projectDir string) (conn, name string) {
	b, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if v, ok := envValue(line, "DB_CONNECTION"); ok {
			conn = v
		}
		if v, ok := envValue(line, "DB_DATABASE"); ok {
			name = v
		}
	}
	return conn, name
}

// envValue parses `KEY=value` (value optionally quoted). Returns ok=false if
// the line isn't an assignment of key.
func envValue(line, key string) (string, bool) {
	if !strings.HasPrefix(line, key) {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, key))
	if !strings.HasPrefix(rest, "=") {
		return "", false // e.g. "DB_DATABASE_FOO=" — not our key
	}
	return strings.Trim(strings.TrimSpace(rest[1:]), `"'`), true
}

func dropMySQLDatabase(ctx context.Context, binDir, dbName string, emit func(string)) error {
	mysql := filepath.Join(binDir, "mysql.exe")
	if _, err := os.Stat(mysql); err != nil {
		return i18n.Errorf("errors.projects.mysqlExeNotFound", map[string]any{"path": mysql})
	}
	stmt := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", dbName)
	cmd := exec.CommandContext(ctx, mysql,
		"--protocol=TCP",
		"-h", "127.0.0.1",
		"-P", "3306",
		"-u", "root",
		"-e", stmt,
	)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 && emit != nil {
		emit(strings.TrimSpace(string(out)))
	}
	return err
}

func dropPostgresDatabase(ctx context.Context, binDir, dbName string, emit func(string)) error {
	dropdb := filepath.Join(binDir, "dropdb.exe")
	if _, err := os.Stat(dropdb); err != nil {
		return i18n.Errorf("errors.projects.dropdbExeNotFound", map[string]any{"path": dropdb})
	}
	cmd := exec.CommandContext(ctx, dropdb,
		"-h", "127.0.0.1",
		"-p", "5432",
		"-U", "postgres",
		"--if-exists",
		dbName,
	)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 && emit != nil {
		emit(strings.TrimSpace(string(out)))
	}
	return err
}

func createMySQLDatabase(ctx context.Context, binDir, dbName string, emit func(string)) error {
	mysql := filepath.Join(binDir, "mysql.exe")
	if _, err := os.Stat(mysql); err != nil {
		return i18n.Errorf("errors.projects.mysqlExeNotFound", map[string]any{"path": mysql})
	}
	stmt := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName)
	cmd := exec.CommandContext(ctx, mysql,
		"--protocol=TCP",
		"-h", "127.0.0.1",
		"-P", "3306",
		"-u", "root",
		"-e", stmt,
	)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		emit(strings.TrimSpace(string(out)))
	}
	return err
}

func createPostgresDatabase(ctx context.Context, binDir, dbName string, emit func(string)) error {
	createdb := filepath.Join(binDir, "createdb.exe")
	if _, err := os.Stat(createdb); err != nil {
		return i18n.Errorf("errors.projects.createdbExeNotFound", map[string]any{"path": createdb})
	}
	cmd := exec.CommandContext(ctx, createdb,
		"-h", "127.0.0.1",
		"-p", "5432",
		"-U", "postgres",
		dbName,
	)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		emit(strings.TrimSpace(string(out)))
	}
	// createdb returns non-zero if the database already exists; treat as ok.
	if err != nil && strings.Contains(string(out), "already exists") {
		return nil
	}
	return err
}

// patchLaravelEnv updates DB_* keys in <projectDir>/.env with stack-aware
// values. If .env doesn't exist it's created from .env.example.
func patchLaravelEnv(projectDir, dbName, database string) error {
	envPath := filepath.Join(projectDir, ".env")
	if _, err := os.Stat(envPath); err != nil {
		example := filepath.Join(projectDir, ".env.example")
		b, err := os.ReadFile(example)
		if err != nil {
			return i18n.Wrapf(err, "errors.projects.envFileMissing", nil)
		}
		if err := os.WriteFile(envPath, b, 0644); err != nil {
			return err
		}
	}
	b, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}
	content := string(b)

	updates := map[string]string{
		"DB_DATABASE": dbName,
		"DB_USERNAME": "root",
		"DB_PASSWORD": "",
		"DB_HOST":     "127.0.0.1",
	}
	switch database {
	case "mysql":
		updates["DB_CONNECTION"] = "mysql"
		updates["DB_PORT"] = "3306"
	case "postgres":
		updates["DB_CONNECTION"] = "pgsql"
		updates["DB_PORT"] = "5432"
		updates["DB_USERNAME"] = "postgres"
	}

	for k, v := range updates {
		content = setEnvVar(content, k, v)
	}
	return os.WriteFile(envPath, []byte(content), 0644)
}

func setEnvVar(content, key, val string) string {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=.*$`)
	line := key + "=" + val
	if re.MatchString(content) {
		return re.ReplaceAllString(content, line)
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + line + "\n"
}

// runStream executes a binary and streams stdout+stderr line-by-line via emit.
// Already defined in creator.go; re-declared here only if not present.
// (It IS in creator.go, so we reuse it via package scope.)
var _ = bufio.ScanLines

// Package paths centralizes all on-disk locations used by the app.
// Single point of change for the install root — flip Root and the whole
// app moves to a new directory.
package paths

import (
	"os"
	"path/filepath"
)

// legacyRoot is where every install created before the portable build lives.
// It stays the fallback forever: an existing install must keep resolving here
// after an update, or the user's projects, databases and config appear to
// vanish.
const legacyRoot = `C:\laraenv`

// portableMarker is shipped inside the portable ZIP, next to the executable.
// Its presence means "the folder holding the exe is the whole environment".
const portableMarker = ".laraenv-portable"

// EnvRoot overrides the install root. Escape hatch for tests and power users.
const EnvRoot = "LARAENV_ROOT"

// root is the install root. Everything the app manages lives under here.
// Resolved once at process start — see resolve.
var root = legacyRoot

// portable records whether this process is running from a portable
// distribution. Resolved once, alongside root.
var portable bool

// init runs before main, which touches paths.* on its very first statement.
// Resolving here rather than from main removes any initialisation-order trap.
func init() {
	dir := exeDir()
	root = resolve(dir, os.Getenv(EnvRoot))
	portable = IsPortableDir(dir)
}

// IsPortableDir reports whether the marker sits next to an executable in the
// given directory. It is deliberately independent of root: EnvRoot also moves
// the root, but it is a support escape hatch rather than a distribution, and
// still wants the installer-based update path.
//
// Exported so the updater can re-derive the answer at the moment it applies
// an update, instead of trusting a value carried since process start.
func IsPortableDir(exeDir string) bool {
	_, err := os.Stat(filepath.Join(exeDir, portableMarker))
	return err == nil
}

// IsPortable reports whether the app is running from a portable
// distribution — the environment lives in the folder holding the executable,
// so an update replaces that executable in place rather than running the
// installer.
func IsPortable() bool { return portable }

// resolve picks the install root. Kept free of process state so tests can
// drive every branch directly.
func resolve(exeDir, envRoot string) string {
	if envRoot != "" {
		return envRoot
	}
	if IsPortableDir(exeDir) {
		return exeDir
	}
	return legacyRoot
}

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// Root is the install root. Everything the app manages lives under here.
func Root() string { return root }

// Common subdirs.
func Cache() string                 { return filepath.Join(root, "cache") }
func Bin() string                   { return filepath.Join(root, "bin") }
func PHPRoot() string               { return filepath.Join(root, "php") }
func NodeRoot() string              { return filepath.Join(root, "nodejs") }
func PythonRoot() string            { return filepath.Join(root, "python") }
func NginxRoot() string             { return filepath.Join(root, "nginx") }
func ApacheRoot() string            { return filepath.Join(root, "apache") }
func MySQLRoot() string             { return filepath.Join(root, "mysql") }
func PostgresRoot() string          { return filepath.Join(root, "postgres") }
func MailpitRoot() string           { return filepath.Join(root, "mailpit") }
func RedisRoot() string             { return filepath.Join(root, "redis") }
func CmderRoot() string             { return filepath.Join(root, "cmder") }
func ComposerRoot() string          { return filepath.Join(root, "composer") }
func ComposerPhar() string          { return filepath.Join(ComposerRoot(), "composer.phar") }
func SSHRoot() string               { return filepath.Join(root, "ssh") }
func SSHHostDir(host string) string { return filepath.Join(SSHRoot(), host) }
func SSHKnownHosts() string         { return filepath.Join(SSHRoot(), "known_hosts") }
func ProjectsRoot() string          { return filepath.Join(root, "www") }
func ConfigFile() string            { return filepath.Join(root, "config.json") }
func StateRoot() string             { return filepath.Join(root, "state") }
func WorktreesRoot() string         { return filepath.Join(root, "worktrees") }
func AgentSessionsFile() string {
	return filepath.Join(StateRoot(), "agent-sessions.json")
}
func AgentEventsRoot() string   { return filepath.Join(StateRoot(), "agent-events") }
func AgentActivityRoot() string { return filepath.Join(StateRoot(), "agent-activity") }
func ProjectRoutesFile() string { return filepath.Join(StateRoot(), "project-routes.json") }
func NotificationsFile() string { return filepath.Join(StateRoot(), "notifications.json") }

// Nginx-specific.
func NginxConf() string  { return filepath.Join(NginxRoot(), "conf", "nginx.conf") }
func NginxSites() string { return filepath.Join(NginxRoot(), "conf", "sites") }
func NginxCerts() string { return filepath.Join(NginxRoot(), "certs") }

// MySQL-specific.
func MySQLBin() string  { return filepath.Join(MySQLRoot(), "bin") }
func MySQLData() string { return filepath.Join(MySQLRoot(), "data") }
func MySQLD() string    { return filepath.Join(MySQLBin(), "mysqld.exe") }

// PostgreSQL-specific.
func PostgresBin() string  { return filepath.Join(PostgresRoot(), "bin") }
func PostgresData() string { return filepath.Join(PostgresRoot(), "data") }
func PostgresExe() string  { return filepath.Join(PostgresBin(), "postgres.exe") }
func InitDB() string       { return filepath.Join(PostgresBin(), "initdb.exe") }

// Apache-specific.
func ApacheExe() string   { return filepath.Join(ApacheRoot(), "bin", "httpd.exe") }
func ApacheConf() string  { return filepath.Join(ApacheRoot(), "conf", "httpd.conf") }
func ApacheSites() string { return filepath.Join(ApacheRoot(), "conf", "sites") }
func ApacheCerts() string { return filepath.Join(ApacheRoot(), "certs") }
func ApacheLogs() string  { return filepath.Join(ApacheRoot(), "logs") }

// Caddy-specific.
func CaddyRoot() string  { return filepath.Join(root, "caddy") }
func CaddyExe() string   { return filepath.Join(CaddyRoot(), "caddy.exe") }
func CaddyFile() string  { return filepath.Join(CaddyRoot(), "Caddyfile") }
func CaddySites() string { return filepath.Join(CaddyRoot(), "sites") }
func CaddyLogs() string  { return filepath.Join(CaddyRoot(), "logs") }

// Nginx binary.
func NginxExe() string { return filepath.Join(NginxRoot(), "nginx.exe") }

// Mailpit binary.
func MailpitExe() string { return filepath.Join(MailpitRoot(), "mailpit.exe") }

// Redis-specific.
func RedisExe() string  { return filepath.Join(RedisRoot(), "redis-server.exe") }
func RedisConf() string { return filepath.Join(RedisRoot(), "redis.windows.conf") }
func RedisData() string { return filepath.Join(RedisRoot(), "data") }

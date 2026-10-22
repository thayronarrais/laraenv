package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/paths"
)

func (a *App) mcpDatabase(ctx context.Context, input mcpbridge.Args, progress func(string)) (any, error) {
	if a.services == nil {
		return nil, errors.New("local service manager is unavailable")
	}
	adapter := mcpDatabaseAdapter{stateRoot: paths.StateRoot(), mysqlBin: paths.MySQLBin(), postgresBin: paths.PostgresBin(), status: a.ServiceStatus}
	return adapter.call(ctx, mcpDatabaseRequest{action: input.Action, engine: input.Engine, database: input.Database, confirm: input.Confirm}, progress)
}

type mcpDatabaseRequest struct {
	action, engine, database string
	confirm                  bool
}
type mcpDatabaseEntry struct {
	Name   string `json:"name"`
	System bool   `json:"system"`
}
type mcpDatabaseList struct {
	Engine    string             `json:"engine"`
	Databases []mcpDatabaseEntry `json:"databases"`
}
type mcpDatabaseMutation struct {
	Engine   string `json:"engine"`
	Database string `json:"database"`
	Action   string `json:"action"`
}
type mcpDatabaseSnapshot struct {
	Engine   string `json:"engine"`
	Database string `json:"database"`
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
}
type mcpDatabaseAdapter struct {
	stateRoot, mysqlBin, postgresBin string
	status                           func(string) string
	run                              func(context.Context, string, []string, io.Writer) error
}

var mcpDatabaseIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

const (
	mcpDatabaseListLimit     = 1024 * 1024
	mcpDatabaseSnapshotLimit = 512 * 1024 * 1024
)

func mcpSystemDatabase(engine, database string) bool {
	name := strings.ToLower(database)
	if engine == "mysql" {
		return name == "mysql" || name == "information_schema" || name == "performance_schema" || name == "sys"
	}
	return name == "postgres" || name == "template0" || name == "template1"
}

func (d mcpDatabaseAdapter) call(ctx context.Context, input mcpDatabaseRequest, progress func(string)) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.engine != "mysql" && input.engine != "postgres" {
		return nil, errors.New("database engine must be mysql or postgres")
	}
	if input.action != "list" && input.action != "create" && input.action != "snapshot" && input.action != "drop" {
		return nil, errors.New("unsupported database action")
	}
	if input.action != "list" && !mcpDatabaseIdentifier.MatchString(input.database) {
		return nil, errors.New("database must be an ASCII identifier of at most 63 characters")
	}
	if input.action == "drop" {
		if !input.confirm {
			return nil, errors.New("database drop requires confirm=true")
		}
		if mcpSystemDatabase(input.engine, input.database) {
			return nil, errors.New("system databases cannot be dropped")
		}
	}
	if input.action == "create" && mcpSystemDatabase(input.engine, input.database) {
		return nil, errors.New("system database names are reserved")
	}
	if d.status == nil || d.status(input.engine) != "running" {
		return nil, errors.New("database engine must be running in LaraEnv")
	}
	if d.run == nil {
		d.run = mcpRunDatabaseCommand
	}
	if progress != nil {
		progress(input.engine + " database " + input.action + " started")
	}
	if input.action == "snapshot" {
		return d.snapshot(ctx, input)
	}
	executable, args := d.command(input)
	var output bytes.Buffer
	bounded := &mcpDatabaseWriter{writer: &output, remaining: mcpDatabaseListLimit}
	stdout := io.Writer(io.Discard)
	if input.action == "list" {
		stdout = bounded
	}
	if err := d.run(ctx, executable, args, stdout); err != nil {
		return nil, mcpDatabaseCommandError(ctx, input.action)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bounded.exceeded {
		return nil, errors.New("database listing exceeded the output limit")
	}
	if input.action != "list" {
		return mcpDatabaseMutation{Engine: input.engine, Database: input.database, Action: input.action}, nil
	}
	entries := make([]mcpDatabaseEntry, 0)
	seen := map[string]bool{}
	for _, line := range strings.Split(output.String(), "\n") {
		name := strings.TrimSuffix(line, "\r")
		if name == "" || seen[name] {
			continue
		}
		if !mcpDatabaseIdentifier.MatchString(name) {
			continue
		}
		seen[name] = true
		entries = append(entries, mcpDatabaseEntry{Name: name, System: mcpSystemDatabase(input.engine, name)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return mcpDatabaseList{Engine: input.engine, Databases: entries}, nil
}

func (d mcpDatabaseAdapter) command(input mcpDatabaseRequest) (string, []string) {
	if input.engine == "mysql" {
		args := []string{"--no-defaults", "--protocol=TCP", "-h", "127.0.0.1", "-P", "3306", "-u", "root", "--skip-password"}
		if input.action == "snapshot" {
			return filepath.Join(d.mysqlBin, "mysqldump.exe"), append(args, "--single-transaction", "--routines", "--events", "--triggers", "--databases", input.database)
		}
		query := "SHOW DATABASES;"
		if input.action == "create" {
			query = fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", input.database)
		}
		if input.action == "drop" {
			query = fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", input.database)
		}
		if input.action == "list" {
			args = append(args, "--batch", "--raw", "--skip-column-names")
		}
		return filepath.Join(d.mysqlBin, "mysql.exe"), append(args, "-e", query)
	}
	args := []string{"-h", "127.0.0.1", "-p", "5432", "-U", "postgres", "--no-password"}
	switch input.action {
	case "list":
		return filepath.Join(d.postgresBin, "psql.exe"), append(args, "--no-psqlrc", "--dbname=postgres", "--tuples-only", "--no-align", "-c", "SELECT datname FROM pg_database ORDER BY datname;")
	case "snapshot":
		return filepath.Join(d.postgresBin, "pg_dump.exe"), append(args, "--format=plain", "--dbname="+input.database)
	case "create":
		return filepath.Join(d.postgresBin, "createdb.exe"), append(args, "--", input.database)
	default:
		return filepath.Join(d.postgresBin, "dropdb.exe"), append(args, "--if-exists", "--", input.database)
	}
}

func (d mcpDatabaseAdapter) snapshot(ctx context.Context, input mcpDatabaseRequest) (any, error) {
	dir, err := mcpDatabaseSnapshotDir(d.stateRoot)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(dir, input.engine+"-"+input.database+"-*.sql")
	if err != nil {
		return nil, errors.New("could not create database snapshot")
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(file.Name())
		}
	}()
	writer := &mcpDatabaseWriter{writer: file, remaining: mcpDatabaseSnapshotLimit}
	executable, args := d.command(input)
	if err := d.run(ctx, executable, args, writer); err != nil {
		return nil, mcpDatabaseCommandError(ctx, "snapshot")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if writer.exceeded {
		return nil, errors.New("database snapshot exceeded the size limit")
	}
	if err := file.Sync(); err != nil {
		return nil, errors.New("could not flush database snapshot")
	}
	if err := file.Close(); err != nil {
		return nil, errors.New("could not close database snapshot")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	complete = true
	return mcpDatabaseSnapshot{Engine: input.engine, Database: input.database, Path: file.Name(), Bytes: writer.written}, nil
}

func mcpDatabaseSnapshotDir(stateRoot string) (string, error) {
	if stateRoot == "" {
		return "", errors.New("database snapshot state directory is unavailable")
	}
	stateRoot, err := filepath.Abs(stateRoot)
	if err != nil {
		return "", errors.New("invalid database snapshot state directory")
	}
	dir, err := filepath.Abs(filepath.Join(stateRoot, "mcp-snapshots"))
	if err != nil {
		return "", errors.New("invalid database snapshot directory")
	}
	if err := os.MkdirAll(stateRoot, 0700); err != nil {
		return "", errors.New("could not prepare database snapshot state directory")
	}
	// Both components are owned application directories. Reject a junction or
	// symlink before creating anything beneath it.
	for _, path := range []string{stateRoot, dir} {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) && path == dir {
			if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
				return "", errors.New("could not prepare database snapshot directory")
			}
			info, err = os.Lstat(dir)
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("database snapshot directory must be an owned directory without symbolic links")
		}
	}
	return dir, nil
}

func mcpDatabaseCommandError(ctx context.Context, action string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Client stderr can contain connection credentials or SQL/data. It never
	// becomes MCP progress or an error payload.
	return fmt.Errorf("local database %s failed; verify the installed client and local engine", action)
}

type mcpDatabaseWriter struct {
	writer             io.Writer
	remaining, written int64
	exceeded           bool
}

func (w *mcpDatabaseWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		w.exceeded = true
		data = data[:w.remaining]
	}
	n, err := w.writer.Write(data)
	w.remaining -= int64(n)
	w.written += int64(n)
	if err == nil && w.exceeded {
		err = errors.New("database output limit exceeded")
	}
	return n, err
}

func mcpRunDatabaseCommand(ctx context.Context, executable string, args []string, stdout io.Writer) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	// Explicit command arguments pin every connection to loopback. Service
	// environment variables must not add a service file or alternate address.
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch strings.ToUpper(key) {
		case "PGHOST", "PGHOSTADDR", "PGPORT", "PGSERVICE", "PGSERVICEFILE", "PGPASSWORD", "MYSQL_HOST", "MYSQL_TCP_PORT", "MYSQL_PWD":
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	return cmd.Run()
}

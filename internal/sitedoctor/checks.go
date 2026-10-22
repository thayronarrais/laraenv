package sitedoctor

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"lara4ever/internal/projects"
)

// Check reads project evidence; all external probes are explicit dependencies.
// Probe errors and configuration values never become report details.
func Check(ctx context.Context, target Target, deps Dependencies) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	p := target.Project
	report := Report{Project: p.Name, Path: p.Path, Kind: p.Kind, Healthy: true, Findings: []Finding{}, PhpRuntime: p.PhpRuntime, NodeRuntime: p.NodeRuntime}
	add := func(f Finding) {
		if p.Paused {
			f.FixID = ""
			f.RequiresConfirmation = false
		}
		if f.Status == "fail" || f.Status == "warning" {
			report.Healthy = false
		}
		report.Findings = append(report.Findings, f)
	}
	laravel, wordpress := p.Kind == "laravel", p.Kind == "wordpress"
	phpNeeded := laravel || wordpress || regularFile(filepath.Join(p.Path, "index.php")) || regularFile(filepath.Join(p.DocRoot, "index.php")) || regularFile(filepath.Join(p.Path, "composer.json"))
	phpUsable := runtimeUsable(p.PhpRuntime) && regularFile(target.PHPExe)
	vendor := regularFile(filepath.Join(p.Path, "vendor", "autoload.php"))
	docRoot := p.DocRoot
	if docRoot == "" {
		docRoot = p.Path
	}
	if info, err := os.Stat(docRoot); err == nil && info.IsDir() {
		add(finding("document_root", "pass", "document_root_ok"))
	} else if os.IsNotExist(err) || err == nil {
		add(finding("document_root", "fail", "document_root_missing"))
	} else {
		add(finding("document_root", "warning", "document_root_unreadable"))
	}
	add(runtimeFinding("php_runtime", "php", p.PhpRuntime, phpNeeded, target.PHPExe))
	nodeNeeded := p.PackageManager != "" || regularFile(filepath.Join(p.Path, "package.json"))
	nodeExe := ""
	if target.NodeDir != "" {
		nodeExe = filepath.Join(target.NodeDir, "node.exe")
	}
	add(runtimeFinding("node_runtime", "nodejs", p.NodeRuntime, nodeNeeded, nodeExe))
	if laravel || regularFile(filepath.Join(p.Path, "composer.json")) {
		f := finding("composer_dependencies", "pass", "composer_dependencies_ok")
		if !vendor {
			f = finding("composer_dependencies", "fail", "composer_dependencies_missing")
			if phpUsable && regularFile(target.ComposerPhar) {
				f.FixID = f.ID
			}
		}
		add(f)
	} else {
		add(finding("composer_dependencies", "skipped", "not_applicable"))
	}
	if nodeNeeded {
		f := finding("node_dependencies", "pass", "node_dependencies_ok")
		installed := directory(filepath.Join(p.Path, "node_modules")) || (p.PackageManager == "yarn" && regularFile(filepath.Join(p.Path, ".pnp.cjs")))
		if !installed {
			f = finding("node_dependencies", "warning", "node_dependencies_missing")
			if runtimeUsable(p.NodeRuntime) && regularFile(filepath.Join(target.NodeDir, "node.exe")) {
				f.FixID = f.ID
			}
		}
		add(f)
	} else {
		add(finding("node_dependencies", "skipped", "not_applicable"))
	}
	var env map[string]string
	envReady := false
	if laravel {
		var err error
		env, err = ReadEnvFile(filepath.Join(p.Path, ".env"))
		f := finding("env", "pass", "env_ok")
		switch {
		case missingPath(filepath.Join(p.Path, ".env")):
			f = finding("env", "fail", "env_missing")
			if regularFile(filepath.Join(p.Path, ".env.example")) {
				f.FixID = f.ID
			}
		case err != nil:
			f = finding("env", "warning", safeEnvCode(err.Error()))
		default:
			envReady = true
		}
		add(f)
		f = finding("app_key", "skipped", "prerequisites_missing")
		if envReady {
			key := strings.TrimSpace(env["APP_KEY"])
			switch {
			case HasCachedConfiguration(p.Path):
				f = finding("app_key", "skipped", "app_key_unsupported")
			case strings.Contains(key, "$"):
				f = finding("app_key", "skipped", "app_key_unsupported")
			case key == "":
				f = finding("app_key", "fail", "app_key_missing")
				if phpUsable && vendor {
					f.FixID = f.ID
				}
			default:
				f = finding("app_key", "pass", "app_key_ok")
			}
		}
		add(f)
		add(storageFinding(p.Path, envReady && phpUsable && vendor))
	} else {
		for _, id := range []string{"env", "app_key", "storage_link"} {
			add(finding(id, "skipped", "not_applicable"))
		}
	}
	wpReady := false
	if wordpress {
		path := filepath.Join(p.Path, "wp-config.php")
		f := finding("wp_config", "pass", "wp_config_ok")
		if missingPath(path) {
			f = finding("wp_config", "fail", "wp_config_missing")
		} else if _, err := boundedFile(path); err != nil {
			f = finding("wp_config", "warning", "wp_config_unreadable")
		} else {
			wpReady = true
		}
		add(f)
	} else {
		add(finding("wp_config", "skipped", "not_applicable"))
	}
	dbReady := false
	if laravel || wordpress {
		f := finding("database", "skipped", "prerequisites_missing")
		if p.Paused {
			f.Code = "project_paused"
		} else if envReady || wpReady {
			var conn DatabaseConnection
			var err error
			if laravel {
				conn, err = LaravelDatabase(p.Path)
			} else {
				conn, err = WordPressDatabase(p.Path)
			}
			if err != nil {
				f = finding("database", "skipped", "database_config_unsupported")
				if err.Error() == "database_port_invalid" {
					f = finding("database", "warning", "database_port_invalid")
				}
			} else {
				var state DatabaseState
				switch {
				case conn.Driver == "sqlite":
					state = sqliteState(p.Path, conn.Database)
				case deps.Database == nil:
					state = DatabaseState{Status: "inconclusive", Code: "database_probe_failed"}
				default:
					state, err = deps.Database(ctx, conn)
				}
				if ctx.Err() != nil {
					return Report{}, ctx.Err()
				}
				if err != nil {
					state = DatabaseState{Status: "inconclusive", Code: "database_probe_failed"}
				}
				f = databaseFinding(state)
				dbReady = state.Status == "exists"
				if state.Status == "missing" && CanCreateDatabase(conn) {
					f.FixID = f.ID
				}
			}
		}
		add(f)
	} else {
		add(finding("database", "skipped", "not_applicable"))
	}
	f := finding("migrations", "skipped", "not_applicable")
	if laravel {
		f = finding("migrations", "skipped", "prerequisites_missing")
		if p.Paused {
			f.Code = "project_paused"
		} else if envReady && phpUsable && vendor && dbReady {
			if deps.Migrations == nil {
				f = finding("migrations", "skipped", "migrations_unavailable")
			} else {
				state, err := deps.Migrations(ctx, p)
				if ctx.Err() != nil {
					return Report{}, ctx.Err()
				}
				switch {
				case err != nil || state.Pending < 0:
					f = finding("migrations", "skipped", "migrations_unavailable")
				case state.Pending > 0:
					f = finding("migrations", "warning", "migrations_pending")
					f.Detail = strconv.Itoa(state.Pending)
					f.FixID = f.ID
					f.RequiresConfirmation = true
				default:
					f = finding("migrations", "pass", "migrations_ok")
				}
			}
		}
	}
	add(f)
	if p.Paused {
		for _, id := range []string{"vhost", "hosts", "php_fpm"} {
			add(finding(id, "skipped", "project_paused"))
		}
	} else {
		vhost := vhostFinding(target.Vhosts)
		// Regeneration must use the same safely resolved PHP as serving.
		if phpNeeded && !runtimeUsable(p.PhpRuntime) {
			vhost.FixID = ""
			vhost.RequiresConfirmation = false
		}
		add(vhost)
		add(hostsFinding(p.URL, target.Hostnames, target.Hosts, target.HostsError))
		f = finding("php_fpm", "skipped", "not_applicable")
		if target.PHPFPMStatuses != nil {
			if len(target.PHPFPMStatuses) > 0 {
				f = finding("php_fpm", "pass", "php_fpm_ok")
			}
			for _, status := range target.PHPFPMStatuses {
				if status == "unavailable" {
					f = finding("php_fpm", "warning", "php_fpm_unavailable")
					break
				}
				if status != "running" {
					f = finding("php_fpm", "warning", "php_fpm_stopped")
					f.FixID = f.ID
				}
			}
		} else if phpNeeded && p.ProxyPort == 0 {
			switch {
			case !phpUsable || !regularFile(filepath.Join(filepath.Dir(target.PHPExe), "php-cgi.exe")):
				f = finding("php_fpm", "warning", "php_fpm_unavailable")
			case target.PHPFPMStatus == "running":
				f = finding("php_fpm", "pass", "php_fpm_ok")
			default:
				f = finding("php_fpm", "warning", "php_fpm_stopped")
				f.FixID = f.ID
			}
		}
		add(f)
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	return report, nil
}

func finding(id, status, code string) Finding {
	severity := "info"
	if status == "warning" {
		severity = "warning"
	} else if status == "fail" {
		severity = "error"
	}
	return Finding{ID: id, Status: status, Severity: severity, Code: code}
}

func runtimeUsable(r projects.RuntimeResolution) bool {
	return r.Version != "" && (r.Status == "resolved" || r.Status == "incompatible")
}

func runtimeFinding(id, kind string, r projects.RuntimeResolution, needed bool, executable string) Finding {
	declared := r.Constraint != "" || (r.Source != "" && r.Source != "global")
	if !needed && !declared {
		return finding(id, "skipped", "not_applicable")
	}
	f := finding(id, "pass", "runtime_ok")
	switch r.Status {
	case "resolved":
	case "missing":
		f = finding(id, "fail", "runtime_missing")
		f.FixID = "download"
		f.RuntimeKind = kind
		f.InstallVersion = r.InstallVersion
	case "invalid":
		f = finding(id, "fail", "runtime_invalid")
	case "incompatible":
		f = finding(id, "warning", "runtime_incompatible")
	case "unmanaged":
		f = finding(id, "skipped", "runtime_unmanaged")
		if !regularFile(executable) {
			f = finding(id, "fail", "runtime_path_missing")
		}
	default:
		f = finding(id, "warning", "runtime_unmanaged")
	}
	return f
}

func safeEnvCode(code string) string {
	switch code {
	case "env_too_large", "env_unsupported":
		return code
	default:
		return "env_unreadable"
	}
}

func databaseFinding(state DatabaseState) Finding {
	switch state.Status {
	case "inconclusive":
		return finding("database", "skipped", "database_probe_failed")
	case "exists":
		return finding("database", "pass", "database_ok")
	case "missing":
		return finding("database", "fail", "database_missing")
	case "unsupported", "unreachable":
		code := state.Code
		switch code {
		case "database_unreachable", "database_auth_failed", "database_external", "database_unsupported", "database_name_unsupported", "database_extension_missing":
		default:
			code = "database_probe_failed"
		}
		// A failed inspection is not evidence that the application's database
		// connection is broken. Keep it visible without guessing a fault.
		if code == "database_probe_failed" {
			return finding("database", "skipped", code)
		}
		// Unsupported configurations were not probed. They are limits of the
		// diagnosis, rather than evidence of a broken database connection.
		if state.Status == "unsupported" {
			switch code {
			case "database_external", "database_unsupported", "database_name_unsupported":
				return finding("database", "skipped", code)
			}
		}
		return finding("database", "warning", code)
	default:
		return finding("database", "skipped", "database_probe_failed")
	}
}

func sqliteState(root, database string) DatabaseState {
	if database == ":memory:" {
		return DatabaseState{Status: "exists"}
	}
	if database == "" {
		database = filepath.Join("database", "database.sqlite")
	}
	if !filepath.IsAbs(database) {
		database = filepath.Join(root, database)
	}
	if regularFile(database) {
		return DatabaseState{Status: "exists"}
	}
	if missingPath(database) {
		return DatabaseState{Status: "missing"}
	}
	return DatabaseState{Status: "inconclusive", Code: "database_probe_failed"}
}

func vhostFinding(checks []VhostCheck) Finding {
	if len(checks) == 0 {
		return finding("vhost", "skipped", "vhost_unavailable")
	}
	f := finding("vhost", "pass", "vhost_ok")
	unavailable := false
	for _, check := range checks {
		// An unknown route must not erase problems in routes we can inspect.
		// Regeneration is global, so any unknown route disables that repair.
		if len(check.Expected) == 0 {
			unavailable = true
			continue
		}
		if missingPath(check.Path) {
			if f.Status == "pass" {
				f = finding("vhost", "fail", "vhost_missing")
				f.FixID = f.ID
			}
			continue
		}
		data, err := boundedFile(check.Path)
		if err != nil {
			return finding("vhost", "warning", "vhost_unreadable")
		}
		normalize := func(data []byte) []byte { return bytes.TrimSpace(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))) }
		if !bytes.Equal(normalize(data), normalize(check.Expected)) {
			f = finding("vhost", "warning", "vhost_mismatch")
			f.FixID = f.ID
			f.RequiresConfirmation = true
		}
	}
	if unavailable {
		if f.Status == "pass" {
			return finding("vhost", "skipped", "vhost_unavailable")
		}
		f.FixID = ""
		f.RequiresConfirmation = false
	}
	return f
}

func hostsFinding(projectURL string, hostnames []string, hosts []byte, readErr error) Finding {
	if readErr != nil {
		return finding("hosts", "warning", "hosts_unreadable")
	}
	if len(hostnames) == 0 {
		u, err := url.Parse(projectURL)
		if err != nil || u.Hostname() == "" {
			return finding("hosts", "warning", "hosts_unreadable")
		}
		hostnames = []string{u.Hostname()}
	}
	found := map[string]bool{}
	conflicting := map[string]bool{}
	unmanagedConflict := map[string]bool{}
	for _, line := range strings.Split(string(hosts), "\n") {
		var comment string
		line, comment, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ip := net.ParseIP(fields[0])
		if ip == nil {
			continue
		}
		for _, host := range fields[1:] {
			key := strings.ToLower(host)
			if ip.IsLoopback() {
				found[key] = true
			} else {
				conflicting[key] = true
				// Match the marker emitted by HostsEditor, never a similar
				// user comment containing the application's name.
				if strings.TrimSpace(comment) != "lara4ever" {
					unmanagedConflict[key] = true
				}
			}
		}
	}
	allFound := true
	canRepair := true
	for _, hostname := range hostnames {
		key := strings.ToLower(hostname)
		if !found[key] || conflicting[key] {
			allFound = false
		}
		if unmanagedConflict[key] {
			canRepair = false
		}
	}
	if allFound {
		return finding("hosts", "pass", "hosts_ok")
	}
	f := finding("hosts", "fail", "hosts_missing")
	if canRepair {
		f.FixID = f.ID
	}
	return f
}

func regularFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func directory(path string) bool   { info, err := os.Stat(path); return err == nil && info.IsDir() }
func missingPath(path string) bool { _, err := os.Stat(path); return os.IsNotExist(err) }

func boundedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, os.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err == nil && len(data) > 1<<20 {
		err = os.ErrInvalid
	}
	return data, err
}

package main

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"lara4ever/internal/paths"
	"lara4ever/internal/php"
	"lara4ever/internal/projectroutes"
	"lara4ever/internal/projects"
	"lara4ever/internal/sitedoctor"
	"lara4ever/internal/vhost"
)

func (a *App) doctorProject(name string) (projects.Project, error) {
	if a.projects == nil || !mcpDiagnosticDirectory(a.projects.Root()) {
		return projects.Project{}, errors.New("configured projects directory is unavailable")
	}
	return a.mcpProject(name)
}

func (a *App) DiagnoseProject(name string) (sitedoctor.Report, error) {
	ctx, cancel := context.WithTimeout(a.appContext(), 45*time.Second)
	defer cancel()
	return a.diagnoseProject(ctx, name)
}

func (a *App) diagnoseProject(ctx context.Context, name string) (sitedoctor.Report, error) {
	p, err := a.doctorProject(name)
	if err != nil {
		return sitedoctor.Report{}, err
	}
	return a.diagnoseProjectSnapshot(ctx, p)
}

func (a *App) diagnoseProjectSnapshot(ctx context.Context, p projects.Project) (sitedoctor.Report, error) {
	target, err := a.doctorTarget(p)
	if err != nil {
		return sitedoctor.Report{}, err
	}
	return sitedoctor.Check(ctx, target, sitedoctor.Dependencies{
		Database: func(ctx context.Context, connection sitedoctor.DatabaseConnection) (sitedoctor.DatabaseState, error) {
			return sitedoctor.ProbeDatabase(ctx, connection, doctorDatabaseExecutor(p, target.PHPExe, nil))
		},
		Migrations: a.doctorPendingMigrations,
	})
}

func (a *App) DiagnoseAllProjects() ([]sitedoctor.Report, error) {
	if a.projects == nil || !mcpDiagnosticDirectory(a.projects.Root()) {
		return nil, errors.New("configured projects directory is unavailable")
	}
	list, err := a.ListProjects()
	if err != nil {
		return nil, err
	}
	reports := make([]sitedoctor.Report, len(list))
	jobs := make(chan int, len(list))
	for i := range list {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for worker := 0; worker < 4 && worker < len(list); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				ctx, cancel := context.WithTimeout(a.appContext(), 45*time.Second)
				report, checkErr := a.diagnoseProject(ctx, list[i].Name)
				cancel()
				if checkErr != nil {
					report = sitedoctor.Report{Project: list[i].Name, Path: list[i].Path, Kind: list[i].Kind, Findings: []sitedoctor.Finding{{ID: "document_root", Status: "warning", Severity: "warning", Code: "inspection_failed"}}}
				}
				reports[i] = report
			}
		}()
	}
	workers.Wait()
	return reports, nil
}

func (a *App) doctorTarget(p projects.Project) (sitedoctor.Target, error) {
	target := sitedoctor.Target{Project: p, ComposerPhar: paths.ComposerPhar(), PHPFPMStatuses: map[string]string{}}
	if p.PhpRuntime.Version != "" && a.php != nil {
		target.PHPExe = filepath.Join(a.php.VersionDir(p.PhpRuntime.Version), "php.exe")
	}
	if target.PHPExe == "" && p.PhpRuntime.Status == "unmanaged" {
		target.PHPExe, _ = exec.LookPath("php.exe")
	}
	if p.NodeRuntime.Version != "" && a.nodejs != nil {
		target.NodeDir = a.nodejs.VersionDir(p.NodeRuntime.Version)
	}
	if p.NodeRuntime.Status == "unmanaged" {
		if executable, lookupErr := exec.LookPath("node.exe"); lookupErr == nil {
			target.NodeDir = filepath.Dir(executable)
		}
	}
	if a.hosts != nil {
		target.Hosts, target.HostsError = a.hosts.Snapshot()
	} else {
		target.HostsError = errors.New("hosts file is unavailable")
	}
	if a.services != nil && p.PhpRuntime.Version != "" {
		target.PHPFPMStatus = a.services.Status("php-fpm:" + p.PhpRuntime.Version)
	}
	if p.Paused {
		return target, nil
	}
	target.Vhosts, target.Hostnames, target.PHPFPMStatuses = a.doctorVhostChecks(p)
	return target, nil
}

// Render expected files without EnsureIni, certificate generation or service starts.
func (a *App) doctorVhostChecks(p projects.Project) ([]sitedoctor.VhostCheck, []string, map[string]string) {
	var checks []sitedoctor.VhostCheck
	var hosts []string
	fpm := map[string]string{}
	proxy := p.ProxyPort
	if a.devsrv != nil {
		if status := a.devsrv.Status(p.Name); status.Running && status.Port > 0 {
			proxy = status.Port
		}
	}
	appendRoute := func(folder, host, path string, proxyPort int, useMain bool) {
		det := projects.Detect(path)
		phpRuntime := p.PhpRuntime
		if !useMain {
			phpRuntime, _ = a.projectRuntimeSelection(p.Name, path)
		}
		unresolved := runtimeRequirementError(p.Name, "PHP", phpRuntime) != nil
		spec := vhost.VhostSpec{ServerName: host, DocRoot: det.DocRoot, Kind: string(det.Kind), LogPrefix: folder, FastCGIPort: php.PortFor(phpRuntime.Version), FastCGIUpstream: php.UpstreamName(phpRuntime.Version), ProxyPort: proxyPort, SSL: p.SSL}
		if p.SSL {
			spec.CertFile = filepath.ToSlash(filepath.Join(paths.NginxCerts(), host+".crt"))
			spec.KeyFile = filepath.ToSlash(filepath.Join(paths.NginxCerts(), host+".key"))
		}
		if a.nginx != nil {
			var expected []byte
			if !unresolved {
				expected = vhost.RenderNginxVhost(folder, spec)
			}
			checks = append(checks, sitedoctor.VhostCheck{Path: a.nginx.VhostPath(folder), Expected: expected})
		}
		if a.apache != nil && mcpDiagnosticDirectory(paths.ApacheRoot()) {
			var expected []byte
			if !unresolved {
				expected = vhost.RenderApacheVhost(folder, spec)
			}
			checks = append(checks, sitedoctor.VhostCheck{Path: a.apache.VhostPath(folder), Expected: expected})
		}
		if a.caddy != nil && mcpDiagnosticDirectory(paths.CaddyRoot()) {
			var expected []byte
			if !unresolved {
				expected = vhost.RenderCaddyfile(folder, spec)
			}
			checks = append(checks, sitedoctor.VhostCheck{Path: a.caddy.VhostPath(folder), Expected: expected})
		}
		hosts = append(hosts, host)
		if proxyPort == 0 && phpRuntime.Version != "" && (det.Kind == projects.KindLaravel || det.Kind == projects.KindWordPress || mcpDiagnosticRegularFile(filepath.Join(det.DocRoot, "index.php"))) {
			status := "stopped"
			if a.services != nil {
				status = a.services.Status("php-fpm:" + phpRuntime.Version)
			}
			if a.php == nil || !mcpDiagnosticRegularFile(filepath.Join(a.php.VersionDir(phpRuntime.Version), "php-cgi.exe")) {
				status = "unavailable"
			}
			fpm[phpRuntime.Version] = status
		}
	}
	if a.routes != nil {
		if routes := a.routes.Stored(p.Name); routesNeedManagedApply(routes, p.Path) {
			for _, route := range routes {
				routeProxy := route.ProxyPort
				if route.TargetKind == projectroutes.TargetMainCheckout && proxy > 0 {
					routeProxy = proxy
				}
				appendRoute(routeFolder(p.Name, route), route.Hostname, route.TargetPath, routeProxy, false)
			}
			return checks, hosts, fpm
		}
	}
	parsed, _ := url.Parse(p.URL)
	host := p.Name + "." + a.projects.TLD()
	if parsed != nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	}
	appendRoute(p.Name, host, p.Path, proxy, true)
	return checks, hosts, fpm
}

func doctorDatabaseExecutor(p projects.Project, phpExe string, progress func(string)) sitedoctor.DBExecutor {
	return func(ctx context.Context, script string, input []byte) ([]byte, error) {
		if !mcpDiagnosticRegularFile(phpExe) {
			return nil, errors.New("project PHP executable is unavailable")
		}
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		spec := mcpExecSpec{Executable: phpExe, Args: []string{"-r", script}, Dir: p.Path, Env: mcpEnvironment([]string{filepath.Dir(phpExe)}), Stdin: bytes.NewReader(input)}
		result, err := runMCPExec(ctx, spec, nil)
		if progress != nil && err == nil {
			progress("Database command completed.\n")
		}
		if err != nil || result.Truncated {
			return nil, errors.New("database probe failed")
		}
		return []byte(result.Output), nil
	}
}

func (a *App) doctorPendingMigrations(ctx context.Context, p projects.Project) (sitedoctor.MigrationState, error) {
	spec, err := a.doctorPHPCommandSpec(p, "artisan", []string{"migrate:status", "--no-ansi", "--no-interaction"})
	if err != nil {
		return sitedoctor.MigrationState{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result, err := runMCPExec(ctx, spec, nil)
	if result.Truncated {
		return sitedoctor.MigrationState{}, errors.New("migration status exceeded output limit")
	}
	if err != nil {
		if strings.Contains(result.Output, "Migration table not found") {
			files, _ := filepath.Glob(filepath.Join(p.Path, "database", "migrations", "*.php"))
			if len(files) > 0 {
				return sitedoctor.MigrationState{Pending: len(files)}, nil
			}
		}
		return sitedoctor.MigrationState{}, errors.New("migration status is unavailable")
	}
	return doctorParseMigrations(result.Output)
}

var doctorPendingRow = regexp.MustCompile(`(?i)\bPending\s*$`)
var doctorRanRow = regexp.MustCompile(`(?i)\bRan\s*$`)

func doctorParseMigrations(output string) (sitedoctor.MigrationState, error) {
	state := sitedoctor.MigrationState{}
	valid := strings.Contains(strings.ToLower(output), "no migrations found")
	for _, row := range strings.Split(output, "\n") {
		row = strings.TrimSpace(row)
		if doctorPendingRow.MatchString(row) {
			state.Pending++
			valid = true
			continue
		}
		if doctorRanRow.MatchString(row) {
			valid = true
			continue
		}
		fields := strings.Split(row, "|")
		if len(fields) >= 4 {
			switch strings.ToLower(strings.TrimSpace(fields[1])) {
			case "no":
				state.Pending++
				valid = true
			case "yes":
				valid = true
			}
		}
	}
	if !valid {
		return state, errors.New("migration status could not be interpreted")
	}
	return state, nil
}

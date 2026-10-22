package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"lara4ever/internal/paths"
	"lara4ever/internal/projects"
	"lara4ever/internal/sitedoctor"
)

type DoctorFixRequest struct {
	Project     string `json:"project"`
	FixID       string `json:"fixID"`
	Confirmed   bool   `json:"confirmed"`
	OperationID string `json:"operationID"`
}

type DoctorFixResult struct {
	Project     string `json:"project"`
	FixID       string `json:"fixID"`
	OperationID string `json:"operationID"`
	Success     bool   `json:"success"`
	Output      string `json:"output"`
	ExitCode    int    `json:"exitCode"`
	Truncated   bool   `json:"truncated"`
}

type doctorOperation struct {
	project string
	cancel  context.CancelFunc
}

var doctorOperationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func (a *App) ApplyDoctorFix(request DoctorFixRequest) (DoctorFixResult, error) {
	result := DoctorFixResult{Project: request.Project, FixID: request.FixID, OperationID: request.OperationID}
	if !doctorOperationID.MatchString(request.OperationID) {
		return result, errors.New("a valid Doctor operation ID is required")
	}
	if request.FixID == "migrations" && !request.Confirmed {
		return result, errors.New("confirm migration execution before applying this fix")
	}
	switch request.FixID {
	case "env", "app_key", "composer_dependencies", "node_dependencies", "storage_link", "database", "migrations", "vhost", "hosts", "php_fpm":
	default:
		return result, errors.New("unsupported Doctor fix")
	}
	ctx, cancel := context.WithTimeout(a.appContext(), 15*time.Minute)
	defer cancel()
	a.doctorMu.Lock()
	if a.doctorOperations == nil {
		a.doctorOperations = map[string]doctorOperation{}
	}
	if canceledAt, exists := a.doctorPendingCancellation[request.OperationID]; exists {
		delete(a.doctorPendingCancellation, request.OperationID)
		if time.Since(canceledAt) < time.Minute {
			a.doctorMu.Unlock()
			return result, context.Canceled
		}
	}
	for id, operation := range a.doctorOperations {
		if id == request.OperationID || operation.project == request.Project {
			a.doctorMu.Unlock()
			return result, errors.New("a Doctor repair is already running for this project or operation")
		}
	}
	a.doctorOperations[request.OperationID] = doctorOperation{project: request.Project, cancel: cancel}
	a.doctorMu.Unlock()
	defer func() { a.doctorMu.Lock(); delete(a.doctorOperations, request.OperationID); a.doctorMu.Unlock() }()
	p, err := a.doctorProject(request.Project)
	if err != nil {
		return result, err
	}
	if p.Paused {
		return result, errors.New("resume the project before applying fixes")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	report, err := a.diagnoseProjectSnapshot(ctx, p)
	if err != nil {
		return result, err
	}
	var finding *sitedoctor.Finding
	for i := range report.Findings {
		if report.Findings[i].FixID == request.FixID {
			finding = &report.Findings[i]
			break
		}
	}
	if finding == nil {
		return result, errors.New("this repair is no longer applicable; run Doctor again")
	}
	if finding.RequiresConfirmation && !request.Confirmed {
		return result, errors.New("confirm this repair before replacing project configuration")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	current, err := a.doctorProject(request.Project)
	if err != nil {
		return result, err
	}
	if err := doctorValidateRepairSnapshot(p, current); err != nil {
		return result, err
	}
	progress := func(line string) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "doctor:output", map[string]string{"operationID": request.OperationID, "project": request.Project, "line": line})
		}
	}
	var command *mcpExecSpec
	switch request.FixID {
	case "env":
		err = sitedoctor.CopyEnvironmentExample(p.Path)
	case "app_key", "storage_link", "migrations", "composer_dependencies":
		kind := "artisan"
		args := []string{}
		switch request.FixID {
		case "app_key":
			if err := doctorValidateEmptyKey(p.Path); err != nil {
				return result, err
			}
			args = []string{"key:generate", "--no-ansi", "--no-interaction"}
		case "storage_link":
			args = []string{"storage:link", "--no-ansi", "--no-interaction"}
		case "migrations":
			args = []string{"migrate", "--force", "--no-ansi", "--no-interaction"}
		case "composer_dependencies":
			kind = "composer"
			args = []string{"install", "--no-interaction", "--no-ansi"}
		}
		var spec mcpExecSpec
		spec, err = a.doctorPHPCommandSpec(p, kind, args)
		command = &spec
	case "node_dependencies":
		var spec mcpExecSpec
		spec, err = a.doctorNodeInstallSpec(p)
		command = &spec
	case "database":
		var connection sitedoctor.DatabaseConnection
		if p.Kind == "wordpress" {
			connection, err = sitedoctor.WordPressDatabase(p.Path)
		} else {
			connection, err = sitedoctor.LaravelDatabase(p.Path)
		}
		if err == nil {
			target, _ := a.doctorTarget(p)
			err = sitedoctor.CreateDatabase(ctx, connection, doctorDatabaseExecutor(p, target.PHPExe, progress))
		}
	case "hosts":
		a.projectRuntimeMu.Lock()
		if err = a.doctorValidateInfrastructureFix(ctx, p, request); err == nil {
			err = a.doctorRepairHosts(p)
		}
		a.projectRuntimeMu.Unlock()
	case "vhost":
		if a.services == nil || a.nginx == nil || a.apache == nil || a.caddy == nil || a.hosts == nil {
			err = errors.New("local web configuration is unavailable")
		} else {
			a.projectRuntimeMu.Lock()
			if err = a.doctorValidateInfrastructureFix(ctx, p, request); err == nil {
				err = a.doctorWithPreservedHosts(p, func() error { return a.applyVhostContext(ctx, p.Name, true, a.anyWebServerRunning()) })
			}
			a.projectRuntimeMu.Unlock()
		}
	case "php_fpm":
		if a.services == nil {
			err = errors.New("local service manager is unavailable")
		} else {
			a.projectRuntimeMu.Lock()
			if err = a.doctorValidateInfrastructureFix(ctx, p, request); err == nil {
				_, _, pools := a.doctorVhostChecks(p)
				for version := range pools {
					if err = a.ensurePhpFpmRunningContext(ctx, version); err != nil {
						break
					}
				}
			}
			a.projectRuntimeMu.Unlock()
		}
	}
	if err == nil && command != nil {
		output, runErr := runMCPExec(ctx, *command, progress)
		result.Output, result.ExitCode, result.Truncated = output.Output, output.ExitCode, output.Truncated
		err = runErr
	}
	if err != nil {
		result.Success = false
		if command == nil || result.Output == "" {
			result.ExitCode = -1
			result.Output = err.Error()
			progress(result.Output + "\n")
		}
		return result, nil
	}
	result.Success = true
	if command == nil {
		result.Output = "Repair completed. Run Doctor again to verify.\n"
		progress(result.Output)
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "project:runtime-changed", p.Name)
	}
	return result, nil
}

func (a *App) CancelDoctorFix(operationID string) error {
	if !doctorOperationID.MatchString(operationID) {
		return errors.New("invalid Doctor operation ID")
	}
	a.doctorMu.Lock()
	operation, exists := a.doctorOperations[operationID]
	if !exists {
		if a.doctorPendingCancellation == nil {
			a.doctorPendingCancellation = map[string]time.Time{}
		}
		for id, canceledAt := range a.doctorPendingCancellation {
			if time.Since(canceledAt) >= time.Minute {
				delete(a.doctorPendingCancellation, id)
			}
		}
		if len(a.doctorPendingCancellation) >= 128 {
			var oldestID string
			var oldest time.Time
			for id, canceledAt := range a.doctorPendingCancellation {
				if oldestID == "" || canceledAt.Before(oldest) {
					oldestID, oldest = id, canceledAt
				}
			}
			delete(a.doctorPendingCancellation, oldestID)
		}
		a.doctorPendingCancellation[operationID] = time.Now()
	}
	a.doctorMu.Unlock()
	if exists {
		operation.cancel()
	}
	return nil
}

func (a *App) cancelDoctorOperations() {
	a.doctorMu.Lock()
	defer a.doctorMu.Unlock()
	for _, operation := range a.doctorOperations {
		operation.cancel()
	}
}

func (a *App) doctorCommandEnvironment(p projects.Project) []string {
	var dirs []string
	if p.PhpRuntime.Version != "" && a.php != nil {
		dirs = append(dirs, a.php.VersionDir(p.PhpRuntime.Version))
	}
	if p.NodeRuntime.Version != "" && a.nodejs != nil {
		dirs = append(dirs, a.nodejs.VersionDir(p.NodeRuntime.Version))
	}
	return mcpEnvironment(append(dirs, paths.ComposerRoot()))
}

func doctorValidateRepairSnapshot(checked, current projects.Project) error {
	if current.Paused || checked.Path != current.Path || checked.URL != current.URL || checked.Kind != current.Kind || checked.DocRoot != current.DocRoot || checked.SSL != current.SSL || checked.ProxyPort != current.ProxyPort || checked.PackageManager != current.PackageManager || checked.PhpRuntime != current.PhpRuntime || checked.NodeRuntime != current.NodeRuntime {
		return errors.New("project configuration changed during diagnosis; run Doctor again")
	}
	return nil
}

func (a *App) doctorValidateInfrastructureFix(ctx context.Context, checked projects.Project, request DoctorFixRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := a.doctorProject(checked.Name)
	if err != nil {
		return err
	}
	if err := doctorValidateRepairSnapshot(checked, current); err != nil {
		return err
	}
	target, err := a.doctorTarget(current)
	if err != nil {
		return err
	}
	report, err := sitedoctor.Check(ctx, target, sitedoctor.Dependencies{})
	if err != nil {
		return err
	}
	for _, finding := range report.Findings {
		if finding.FixID == request.FixID {
			if finding.RequiresConfirmation && !request.Confirmed {
				return errors.New("confirm this repair before replacing project configuration")
			}
			return ctx.Err()
		}
	}
	return errors.New("this repair is no longer applicable; run Doctor again")
}

func doctorValidateEmptyKey(path string) error {
	if sitedoctor.HasCachedConfiguration(path) {
		return errors.New("cached application configuration requires manual review")
	}
	values, err := sitedoctor.ReadEnvFile(filepath.Join(path, ".env"))
	if err != nil {
		return errors.New("project environment is unavailable; run Doctor again")
	}
	if strings.TrimSpace(values["APP_KEY"]) != "" {
		return errors.New("APP_KEY is already configured; run Doctor again")
	}
	return nil
}

// Route application rewrites managed hosts lines. Preserve other aliases and
// reject conflicting user mappings before any vhost or service changes.
func (a *App) doctorWithPreservedHosts(p projects.Project, apply func() error) error {
	if a.hosts == nil {
		return errors.New("hosts file is unavailable")
	}
	original, err := a.hosts.Snapshot()
	if err != nil {
		return err
	}
	repaired := original
	_, hosts, _ := a.doctorVhostChecks(p)
	for _, host := range hosts {
		repaired, err = doctorRepairHostsData(repaired, host)
		if err != nil {
			return err
		}
	}
	if err := apply(); err != nil {
		if restoreErr := a.hosts.Restore(original); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return err
	}
	return a.hosts.Restore(repaired)
}

func (a *App) doctorPHPCommandSpec(p projects.Project, kind string, args []string) (mcpExecSpec, error) {
	spec := mcpExecSpec{Dir: p.Path, Env: a.doctorCommandEnvironment(p)}
	if err := runtimeRequirementError(p.Name, "PHP", p.PhpRuntime); err != nil {
		return spec, err
	}
	if p.PhpRuntime.Version != "" && a.php != nil {
		spec.Executable = filepath.Join(a.php.VersionDir(p.PhpRuntime.Version), "php.exe")
	} else if p.PhpRuntime.Status == "unmanaged" {
		spec.Executable, _ = exec.LookPath("php.exe")
	}
	if !mcpDiagnosticRegularFile(spec.Executable) {
		return spec, errors.New("project PHP executable is unavailable")
	}
	script := filepath.Join(p.Path, "artisan")
	if kind == "composer" {
		script = paths.ComposerPhar()
	}
	if !mcpDiagnosticRegularFile(script) {
		return spec, errors.New("requested project command entry point is unavailable")
	}
	spec.Args = append([]string{script}, args...)
	return spec, nil
}

func (a *App) doctorNodeInstallSpec(p projects.Project) (mcpExecSpec, error) {
	spec := mcpExecSpec{Dir: p.Path, Env: a.doctorCommandEnvironment(p)}
	if err := runtimeRequirementError(p.Name, "Node", p.NodeRuntime); err != nil {
		return spec, err
	}
	nodeDir := ""
	if p.NodeRuntime.Version != "" && a.nodejs != nil {
		nodeDir = a.nodejs.VersionDir(p.NodeRuntime.Version)
	} else if p.NodeRuntime.Status == "unmanaged" {
		if exe, err := exec.LookPath("node.exe"); err == nil {
			nodeDir = filepath.Dir(exe)
		}
	}
	if nodeDir == "" || !mcpDiagnosticRegularFile(filepath.Join(nodeDir, "node.exe")) {
		return spec, errors.New("project Node executable is unavailable")
	}
	manager := p.PackageManager
	if manager == "" {
		manager = projects.Detect(p.Path).PackageManager
	}
	if manager == "" {
		manager = "npm"
	}
	if manager == "npm" {
		spec.Executable = filepath.Join(nodeDir, "node.exe")
		cli := filepath.Join(nodeDir, "node_modules", "npm", "bin", "npm-cli.js")
		if !mcpDiagnosticRegularFile(cli) {
			return spec, errors.New("npm is unavailable in the selected Node installation")
		}
		spec.Args = []string{cli, "install", "--no-fund", "--no-audit"}
		return spec, nil
	}
	bin, managerErr := projects.ResolvePackageManagerPublic(manager, nodeDir)
	if managerErr != nil || bin == "" {
		return spec, fmt.Errorf("the project's %s package manager is unavailable", manager)
	}
	if manager == "bun" {
		if !strings.EqualFold(filepath.Ext(bin), ".exe") || !mcpDiagnosticRegularFile(bin) {
			return spec, errors.New("bun executable is unavailable")
		}
		spec.Executable = bin
		spec.Args = []string{"install"}
		return spec, nil
	}
	// Invoke the JS entry point with the selected Node. Global CMD wrappers can
	// otherwise prefer their sibling node.exe over the project PATH.
	entries := map[string][]string{"pnpm": {"pnpm.cjs", "pnpm.js"}, "yarn": {"yarn.js", "yarn.cjs"}}
	for _, dir := range []string{nodeDir, filepath.Dir(bin)} {
		for _, entry := range entries[manager] {
			cli := filepath.Join(dir, "node_modules", manager, "bin", entry)
			if mcpDiagnosticRegularFile(cli) {
				spec.Executable = filepath.Join(nodeDir, "node.exe")
				spec.Args = []string{cli, "install"}
				return spec, nil
			}
		}
		cli := filepath.Join(dir, "node_modules", "corepack", "dist", manager+".js")
		if mcpDiagnosticRegularFile(cli) {
			spec.Executable = filepath.Join(nodeDir, "node.exe")
			spec.Args = []string{cli, "install"}
			return spec, nil
		}
	}
	return spec, fmt.Errorf("%s has no usable JavaScript entry point for the selected Node", manager)
}

func (a *App) doctorRepairHosts(p projects.Project) error {
	if a.hosts == nil {
		return errors.New("hosts file is unavailable")
	}
	_, hosts, _ := a.doctorVhostChecks(p)
	data, err := a.hosts.Snapshot()
	if err != nil {
		return err
	}
	for _, host := range hosts {
		data, err = doctorRepairHostsData(data, host)
		if err != nil {
			return err
		}
	}
	return a.hosts.Restore(data)
}

func doctorRepairHostsData(data []byte, host string) ([]byte, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	var result []string
	present := false
	for _, line := range lines {
		content, comment, _ := strings.Cut(line, "#")
		fields := strings.Fields(content)
		if len(fields) < 2 {
			result = append(result, line)
			continue
		}
		found := false
		for _, alias := range fields[1:] {
			if strings.EqualFold(alias, host) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, line)
			continue
		}
		if ip := net.ParseIP(fields[0]); ip != nil && ip.IsLoopback() {
			present = true
			result = append(result, line)
			continue
		}
		if strings.TrimSpace(comment) != "lara4ever" {
			return nil, errors.New("hosts mapping is owned by the user; update it manually")
		}
		aliases := []string{}
		for _, alias := range fields[1:] {
			if !strings.EqualFold(alias, host) {
				aliases = append(aliases, alias)
			}
		}
		if len(aliases) > 0 {
			result = append(result, fields[0]+" "+strings.Join(aliases, " ")+" # lara4ever")
		}
	}
	if !present {
		result = append(result, "127.0.0.1 "+host+" # lara4ever")
	}
	return []byte(strings.Join(result, "\n")), nil
}

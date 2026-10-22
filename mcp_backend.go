package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"lara4ever/internal/applicationcontext"
	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/projectroutes"
	"lara4ever/internal/projects"
)

type appMCPBackend struct{ app *App }

func (b appMCPBackend) Call(ctx context.Context, group string, in mcpbridge.Args, progress func(string)) (any, error) {
	if in.TimeoutSeconds < 0 || in.TimeoutSeconds > 300 {
		return nil, errors.New("timeoutSeconds must be between 1 and 300")
	}
	seconds := in.TimeoutSeconds
	if seconds == 0 {
		seconds = 60
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	// Long-running commands and read-only logs/diagnostics do not block each
	// other. Mutations of the shared stack/workers are serialized across agents.
	mutation := (group == "project" && in.Action != "list" && in.Action != "detail") || (group == "service" && in.Action != "status") || (group == "worker" && in.Action != "status" && in.Action != "logs") || (group == "db" && in.Action != "list") || (group == "context" && (in.Action == "install_integration" || in.Action == "write_guidelines"))
	if mutation {
		b.app.mcpCalls.Lock()
		if b.app.mcpGate == nil {
			b.app.mcpGate = make(chan struct{}, 1)
		}
		gate := b.app.mcpGate
		b.app.mcpCalls.Unlock()
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch group {
	case "project":
		return b.app.mcpProjects(in)
	case "service":
		return b.app.mcpServices(in)
	case "worker":
		return b.app.mcpWorkers(in)
	case "exec":
		return b.app.mcpExec(ctx, in, progress)
	case "logs":
		return b.app.mcpLogs(in)
	case "db":
		return b.app.mcpDatabase(ctx, in, progress)
	case "diag":
		return b.app.mcpDiagnostics(ctx, in, progress)
	case "context":
		return b.app.mcpApplicationContext(ctx, in)
	}
	return nil, errors.New("unknown LaraEnv tool group")
}

func mcpContained(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func (a *App) mcpProject(name string) (projects.Project, error) {
	if name == "" || strings.ContainsAny(name, "/\\:\x00") || name == "." || name == ".." {
		return projects.Project{}, errors.New("project must be a name from laraenv_project list")
	}
	list, err := a.projects.List()
	if err != nil {
		return projects.Project{}, err
	}
	for _, p := range list {
		if p.Name == name {
			root, e := filepath.EvalSymlinks(a.projects.Root())
			if e != nil {
				return projects.Project{}, e
			}
			actual, e := filepath.EvalSymlinks(p.Path)
			if e != nil {
				return projects.Project{}, e
			}
			if !mcpContained(root, actual) {
				return projects.Project{}, errors.New("project resolves outside the configured projects directory")
			}
			p.Path = actual
			p.PhpRuntime, p.NodeRuntime = a.projectRuntimeSelection(p.Name, actual)
			return p, nil
		}
	}
	return projects.Project{}, errors.New("project not found in LaraEnv")
}

func (a *App) mcpProjectDetail(p projects.Project) map[string]any {
	p.PhpRuntime, p.NodeRuntime = a.projectRuntimeSelection(p.Name, p.Path)
	return map[string]any{"project": p, "effectivePHP": p.PhpRuntime.Version, "effectiveNode": p.NodeRuntime.Version, "devserver": a.GetDevServerStatus(p.Name), "queue": a.GetQueueWorkerStatus(p.Name)}
}
func (a *App) mcpProjects(in mcpbridge.Args) (any, error) {
	if in.Action == "list" {
		return a.ListProjects()
	}
	p, err := a.mcpProject(in.Project)
	if err != nil {
		return nil, err
	}
	switch in.Action {
	case "detail":
		return a.mcpProjectDetail(p), nil
	case "set_php":
		installed, _ := a.php.List()
		valid := in.Version == ""
		for _, v := range installed {
			valid = valid || v.Version == in.Version
		}
		if !valid {
			return nil, errors.New("PHP version is not installed")
		}
		err = a.SetProjectPhp(p.Name, in.Version)
	case "set_node":
		installed, _ := a.nodejs.List()
		valid := in.Version == ""
		for _, v := range installed {
			valid = valid || v.Version == in.Version
		}
		if !valid {
			return nil, errors.New("Node version is not installed")
		}
		err = a.SetProjectNode(p.Name, in.Version)
	case "set_ssl":
		if in.Enabled == nil {
			return nil, errors.New("enabled is required")
		}
		err = a.SetProjectSSL(p.Name, *in.Enabled)
	case "set_proxy":
		if in.Port < 0 || in.Port > 65535 {
			return nil, errors.New("port must be 0 (disabled) or 1..65535")
		}
		err = a.SetProjectProxyPort(p.Name, in.Port)
	case "pause", "resume":
		err = a.mcpSetProjectPaused(p.Name, in.Action == "pause")
	default:
		return nil, errors.New("unknown project action")
	}
	if err != nil {
		return nil, err
	}
	p, err = a.mcpProject(p.Name)
	if err != nil {
		return nil, err
	}
	return a.mcpProjectDetail(p), nil
}

func (a *App) mcpSetProjectPaused(name string, paused bool) error {
	previous := a.cfg.Get().Projects.Paused[name]
	if previous == paused {
		return nil
	}
	if err := a.cfg.SetProjectPaused(name, paused); err != nil {
		return err
	}
	var err error
	if paused {
		if err = a.StopDevServer(name); err == nil {
			err = a.StopQueueWorker(name)
		}
		if err == nil {
			for _, instance := range a.devsrv.ListRunning() {
				if instance.Project == name {
					if err = a.devsrv.Stop(instance.Instance); err != nil {
						break
					}
				}
			}
		}
		if err == nil {
			err = a.removePausedVhosts(name, true)
		}
	} else {
		err = a.applyVhostFor(name)
	}
	if err != nil {
		_ = a.cfg.SetProjectPaused(name, previous)
		_ = a.applyVhostFor(name)
		return err
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "project:paused-changed", name, paused)
	}
	return nil
}

// Pausing keeps route definitions and certificates so resuming restores the
// same project. Deleting/removing a project has different cleanup semantics.
func (a *App) removePausedVhosts(name string, reload bool) error {
	var routes []projectroutes.Route
	if a.routes != nil {
		routes = a.routes.Stored(name)
	}
	return a.removePausedRouteVhosts(name, routes, reload)
}

func (a *App) removePausedRouteVhosts(name string, routes []projectroutes.Route, reload bool) error {
	tld := a.projects.TLD()
	if tld == "" {
		tld = "test"
	}
	targets := map[string]string{name: name + "." + tld}
	for _, r := range routes {
		targets[routeFolder(name, r)] = r.Hostname
	}
	for folder, host := range targets {
		for _, remove := range []func(string) error{a.nginx.RemoveVhost, a.apache.RemoveVhost, a.caddy.RemoveVhost} {
			if err := remove(folder); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := a.hosts.RemoveEntry(host); err != nil {
			return err
		}
	}
	if reload {
		for _, name := range []string{"nginx", "apache", "caddy"} {
			if a.services.Status(name) == "running" {
				if err := a.services.Restart(name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

var mcpServiceNames = []string{"nginx", "apache", "caddy", "mysql", "postgres", "redis", "mailpit"}

func mcpValidService(name string) bool {
	for _, s := range mcpServiceNames {
		if name == s {
			return true
		}
	}
	return false
}
func (a *App) mcpServices(in mcpbridge.Args) (any, error) {
	if in.Action == "status" && in.Service == "" {
		out := []map[string]any{}
		enabled := a.GetEnabledServices()
		for _, s := range mcpServiceNames {
			out = append(out, map[string]any{"name": s, "status": a.ServiceStatus(s), "enabled": enabled[s]})
		}
		return out, nil
	}
	if !mcpValidService(in.Service) {
		return nil, errors.New("unknown local service")
	}
	var err error
	switch in.Action {
	case "status":
	case "start":
		err = a.StartService(in.Service)
	case "stop":
		err = a.StopService(in.Service)
	case "reload":
		err = a.ReloadService(in.Service)
	default:
		return nil, errors.New("unknown service action")
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": in.Service, "status": a.ServiceStatus(in.Service)}, nil
}

func (a *App) mcpWorkers(in mcpbridge.Args) (any, error) {
	if len(in.Args) > 0 {
		return nil, errors.New("worker commands use the saved project arguments; pass args to laraenv_exec instead")
	}
	if in.Kind == "cron" {
		jobs := a.ListCrons()
		if in.ID == "" && in.Action == "status" {
			out := []map[string]any{}
			for _, j := range jobs {
				if in.Project == "" || j.ProjectName == in.Project {
					out = append(out, map[string]any{"id": j.ID, "name": j.Name, "project": j.ProjectName, "enabled": j.Enabled, "schedule": j.Schedule, "status": j.LastStatus, "lastRun": j.LastRun, "nextRun": j.NextRun})
				}
			}
			return out, nil
		}
		for _, j := range jobs {
			if j.ID == in.ID {
				if in.Project != "" && in.Project != j.ProjectName {
					return nil, errors.New("cron does not belong to that project")
				}
				if in.Action == "logs" {
					return mcpTail(strings.Split(j.LastOutput, "\n"), in.Lines), nil
				}
				switch in.Action {
				case "status":
					return map[string]any{"id": j.ID, "enabled": j.Enabled, "status": j.LastStatus, "lastRun": j.LastRun, "nextRun": j.NextRun}, nil
				case "start", "run":
					if !in.Confirm {
						return nil, errors.New("confirm:true is required to run or enable a saved cron command")
					}
					if j.ProjectName != "" {
						p, e := a.mcpProject(j.ProjectName)
						if e != nil {
							return nil, e
						}
						if p.Paused {
							return nil, errors.New("resume the project before starting tasks")
						}
					}
					if in.Action == "run" {
						return map[string]any{"accepted": true}, a.RunCronNow(j.ID)
					}
					return map[string]any{"enabled": true}, a.ToggleCron(j.ID, true)
				case "stop":
					return map[string]any{"enabled": false, "note": "future scheduling disabled; an existing run may finish"}, a.ToggleCron(j.ID, false)
				default:
					return nil, errors.New("unknown cron worker action")
				}
			}
		}
		return nil, errors.New("cron job not found")
	}
	if in.Kind != "devserver" && in.Kind != "queue" {
		return nil, errors.New("kind must be devserver, queue or cron")
	}
	if in.Action == "status" && in.Project == "" {
		list, e := a.ListProjects()
		if e != nil {
			return nil, e
		}
		out := []map[string]any{}
		for _, p := range list {
			var s any
			if in.Kind == "queue" {
				s = a.GetQueueWorkerStatus(p.Name)
			} else {
				s = a.GetDevServerStatus(p.Name)
			}
			out = append(out, map[string]any{"project": p.Name, "status": s})
		}
		return out, nil
	}
	p, err := a.mcpProject(in.Project)
	if err != nil {
		return nil, err
	}
	if (in.Action == "start" || in.Action == "restart") && p.Paused {
		return nil, errors.New("resume the project before starting tasks")
	}
	if in.Action == "start" || in.Action == "restart" {
		if !in.Confirm {
			return nil, errors.New("confirm:true is required to execute project scripts or queued jobs")
		}
	}
	if in.Kind == "queue" {
		switch in.Action {
		case "status":
		case "start":
			_, err = a.StartQueueWorker(p.Name)
		case "stop":
			err = a.StopQueueWorker(p.Name)
		case "restart":
			_, err = a.RestartQueueWorker(p.Name)
		case "logs":
			return mcpTail(a.GetQueueWorkerLogs(p.Name), in.Lines), nil
		default:
			return nil, errors.New("unknown queue action")
		}
		if err != nil {
			return nil, err
		}
		return a.GetQueueWorkerStatus(p.Name), nil
	}
	switch in.Action {
	case "status":
	case "start":
		_, err = a.StartDevServer(p.Name)
	case "stop":
		err = a.StopDevServer(p.Name)
	case "restart":
		err = a.StopDevServer(p.Name)
		if err == nil {
			_, err = a.StartDevServer(p.Name)
		}
	case "logs":
		return mcpTail(a.GetDevServerLogs(p.Name), in.Lines), nil
	default:
		return nil, errors.New("unknown devserver action")
	}
	if err != nil {
		return nil, err
	}
	return a.GetDevServerStatus(p.Name), nil
}

func mcpTail(lines []string, count int) []string {
	if count <= 0 {
		count = 100
	}
	if count > 500 {
		count = 500
	}
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	out := make([]string, len(lines))
	for i, s := range lines {
		if len(s) > 8192 {
			s = s[:8192-len(" [truncated]")] + " [truncated]"
		}
		out[i] = s
	}
	return out
}
func mcpAppLog(root string, count int) ([]string, error) {
	logs, err := applicationcontext.ReadLogs(context.Background(), root, count)
	return logs.Lines, err
}
func (a *App) mcpLogs(in mcpbridge.Args) (any, error) {
	if in.Lines < 0 || in.Lines > 500 {
		return nil, errors.New("lines must be between 1 and 500")
	}
	switch in.Action {
	case "service":
		if !mcpValidService(in.Service) {
			return nil, errors.New("unknown local service")
		}
		return mcpTail(a.GetServiceLogs(in.Service), in.Lines), nil
	case "app":
		p, e := a.mcpProject(in.Project)
		if e != nil {
			return nil, e
		}
		return mcpAppLog(p.Path, in.Lines)
	case "worker":
		in.Action = "logs"
		return a.mcpWorkers(in)
	case "activity":
		return a.GetMCPActivity(), nil
	}
	return nil, fmt.Errorf("unknown logs action")
}

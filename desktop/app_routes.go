package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/atomicfile"
	"lara4ever/internal/i18n"
	"lara4ever/internal/paths"
	"lara4ever/internal/php"
	"lara4ever/internal/projectroutes"
	"lara4ever/internal/projects"
	"lara4ever/internal/vhost"
)

type routeFileSnapshot struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

type AgentWorktreeRuntime struct {
	SessionID       string   `json:"sessionId"`
	ProjectName     string   `json:"projectName"`
	WorktreePath    string   `json:"worktreePath"`
	NeedsDevServer  bool     `json:"needsDevServer"`
	Running         bool     `json:"running"`
	PID             int      `json:"pid"`
	Port            int      `json:"port"`
	ConfiguredPort  int      `json:"configuredPort"`
	Script          string   `json:"script,omitempty"`
	RoutedHostnames []string `json:"routedHostnames"`
	WorktreeDirty   bool     `json:"worktreeDirty"`
	CanRemove       bool     `json:"canRemove"`
	Blockers        []string `json:"blockers"`
}

func agentWorktreeInstanceID(sessionID string) string { return "agent-worktree:" + sessionID }

func agentWorktreeSessionID(instanceID string) (string, bool) {
	const prefix = "agent-worktree:"
	if !strings.HasPrefix(instanceID, prefix) || len(instanceID) == len(prefix) {
		return "", false
	}
	return strings.TrimPrefix(instanceID, prefix), true
}

func (a *App) syncAgentWorktreeRoute(instanceID string) {
	sessionID, ok := agentWorktreeSessionID(instanceID)
	if !ok {
		return
	}
	runtime, err := a.GetAgentWorktreeRuntime(sessionID)
	if err != nil || !runtime.Running || runtime.Port <= 0 || len(runtime.RoutedHostnames) == 0 || runtime.Port == runtime.ConfiguredPort {
		return
	}
	if _, err := a.routes.UpdateWorktreeProxyPort(a.appContext(), runtime.ProjectName, runtime.WorktreePath, runtime.Port); err != nil {
		_ = a.devsrv.Stop(instanceID)
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "app:notice", i18n.T("backend.routes.worktreeServerStoppedRouteFailed", map[string]any{"error": err}))
		}
	}
}

func (a *App) GetAgentWorktreeRuntime(sessionID string) (AgentWorktreeRuntime, error) {
	session, ok := a.agents.GetSession(sessionID)
	if !ok {
		return AgentWorktreeRuntime{}, i18n.Errorf("errors.routes.agentSessionNotFound", map[string]any{"sessionId": sessionID})
	}
	if session.LaunchMode != pkgagents.LaunchWorktree || strings.TrimSpace(session.WorktreePath) == "" {
		return AgentWorktreeRuntime{}, i18n.Errorf("errors.routes.agentSessionNotWorktree", nil)
	}
	ctx := a.appContext()
	project, err := a.resolveRouteProject(ctx, session.ProjectName)
	if err != nil {
		return AgentWorktreeRuntime{}, err
	}
	runtime := AgentWorktreeRuntime{
		SessionID: session.ID, ProjectName: session.ProjectName, WorktreePath: session.WorktreePath,
		RoutedHostnames: []string{}, Blockers: []string{},
	}
	status := a.devsrv.Status(agentWorktreeInstanceID(session.ID))
	runtime.Running, runtime.PID, runtime.Port, runtime.Script = status.Running, status.PID, status.Port, status.Script

	routes, err := a.routes.List(ctx, session.ProjectName)
	if err != nil {
		return AgentWorktreeRuntime{}, err
	}
	for _, route := range routes {
		if !sameWindowsPath(route.TargetPath, session.WorktreePath) {
			continue
		}
		runtime.RoutedHostnames = append(runtime.RoutedHostnames, route.Hostname)
		if route.ProxyPort > 0 {
			runtime.NeedsDevServer = true
			runtime.ConfiguredPort = route.ProxyPort
		}
	}
	if project.MainProxyPort > 0 {
		runtime.NeedsDevServer = true
		if runtime.ConfiguredPort == 0 {
			runtime.ConfiguredPort = project.MainProxyPort + 1
			if runtime.ConfiguredPort > 65535 {
				runtime.ConfiguredPort = 3000
			}
		}
	}
	if _, statErr := os.Stat(session.WorktreePath); statErr != nil {
		runtime.Blockers = append(runtime.Blockers, i18n.T("backend.routes.blockerWorktreeMissing", nil))
	} else if info := a.gitworkspaces.Inspect(ctx, session.WorktreePath); info.Dirty {
		runtime.WorktreeDirty = true
		runtime.Blockers = append(runtime.Blockers, i18n.T("backend.routes.blockerWorktreeDirty", nil))
	}
	if runtime.Running {
		runtime.Blockers = append(runtime.Blockers, i18n.T("backend.routes.blockerDevServerRunning", nil))
	}
	if len(runtime.RoutedHostnames) > 0 {
		runtime.Blockers = append(runtime.Blockers, i18n.T("backend.routes.blockerOwnsDomainRoutes", nil))
	}
	for _, related := range a.agents.SessionsForWorktree(session.WorktreePath) {
		if pkgagents.IsLiveState(related.State) && related.TerminalID != "" {
			runtime.Blockers = append(runtime.Blockers, i18n.T("backend.routes.blockerAgentSessionRunning", map[string]any{"title": related.Title}))
		}
	}
	runtime.CanRemove = len(runtime.Blockers) == 0
	return runtime, nil
}

func (a *App) StartAgentWorktreeDevServer(sessionID string) (AgentWorktreeRuntime, error) {
	if err := a.requireAgentSessionsEnabled(); err != nil {
		return AgentWorktreeRuntime{}, err
	}
	runtime, err := a.GetAgentWorktreeRuntime(sessionID)
	if err != nil {
		return AgentWorktreeRuntime{}, err
	}
	if runtime.Running {
		return runtime, nil
	}
	if a.cfg.Get().Projects.Paused[runtime.ProjectName] {
		return AgentWorktreeRuntime{}, i18n.Errorf("errors.app.projectPaused", nil)
	}
	if !runtime.NeedsDevServer {
		return AgentWorktreeRuntime{}, i18n.Errorf("errors.routes.worktreeServedDirectly", nil)
	}
	if runtime.ConfiguredPort <= 0 {
		return AgentWorktreeRuntime{}, i18n.Errorf("errors.routes.dedicatedProxyPortRequired", nil)
	}
	detection := projects.Detect(runtime.WorktreePath)
	packageManager := detection.PackageManager
	if packageManager == "" {
		packageManager = a.cfg.Get().Projects.DefaultPackageManager
	}
	if packageManager == "" {
		packageManager = "npm"
	}
	_, err = a.devsrv.StartInstance(
		agentWorktreeInstanceID(sessionID), runtime.ProjectName, runtime.WorktreePath, "", packageManager,
		runtime.ConfiguredPort, a.cfg.ProjectDevCommand(runtime.ProjectName),
	)
	if err != nil {
		return AgentWorktreeRuntime{}, err
	}
	status := a.devsrv.Status(agentWorktreeInstanceID(sessionID))
	if len(runtime.RoutedHostnames) > 0 && status.Port > 0 && status.Port != runtime.ConfiguredPort {
		if _, routeErr := a.routes.UpdateWorktreeProxyPort(a.appContext(), runtime.ProjectName, runtime.WorktreePath, status.Port); routeErr != nil {
			_ = a.devsrv.Stop(agentWorktreeInstanceID(sessionID))
			return AgentWorktreeRuntime{}, i18n.Wrapf(routeErr, "errors.routes.updateWorktreeRouteFailed", nil)
		}
	}
	return a.GetAgentWorktreeRuntime(sessionID)
}

func (a *App) StopAgentWorktreeDevServer(sessionID string) (AgentWorktreeRuntime, error) {
	if _, err := a.GetAgentWorktreeRuntime(sessionID); err != nil {
		return AgentWorktreeRuntime{}, err
	}
	if err := a.devsrv.Stop(agentWorktreeInstanceID(sessionID)); err != nil {
		return AgentWorktreeRuntime{}, err
	}
	return a.GetAgentWorktreeRuntime(sessionID)
}

func (a *App) GetAgentWorktreeDevServerLogs(sessionID string) []string {
	return a.devsrv.Logs(agentWorktreeInstanceID(sessionID))
}

func (a *App) DetachAgentWorktreeRoutes(sessionID string) (AgentWorktreeRuntime, error) {
	runtime, err := a.GetAgentWorktreeRuntime(sessionID)
	if err != nil {
		return AgentWorktreeRuntime{}, err
	}
	if _, err := a.routes.DetachWorktree(a.appContext(), runtime.ProjectName, runtime.WorktreePath); err != nil {
		return AgentWorktreeRuntime{}, err
	}
	return a.GetAgentWorktreeRuntime(sessionID)
}

func (a *App) RemoveAgentWorktree(sessionID string) error {
	runtime, err := a.GetAgentWorktreeRuntime(sessionID)
	if err != nil {
		return err
	}
	if !runtime.CanRemove {
		return i18n.Errorf("errors.routes.worktreeCannotBeRemoved", map[string]any{"blockers": strings.Join(runtime.Blockers, "; ")})
	}
	project, err := a.resolveRouteProject(a.appContext(), runtime.ProjectName)
	if err != nil {
		return err
	}
	if err := a.gitworkspaces.Remove(a.appContext(), project.MainPath, runtime.WorktreePath); err != nil {
		return err
	}
	return a.agents.ArchiveWorktreeSessions(runtime.WorktreePath)
}

func (a *App) resolveRouteProject(_ context.Context, name string) (projectroutes.Project, error) {
	list, err := a.projects.List()
	if err != nil {
		return projectroutes.Project{}, err
	}
	for _, project := range list {
		if project.Name != name {
			continue
		}
		proxyPort := project.ProxyPort
		if proxyPort > 0 && a.devsrv != nil {
			if status := a.devsrv.Status(name); status.Running && status.Port > 0 {
				proxyPort = status.Port
			}
		}
		return projectroutes.Project{
			Name: name, MainPath: project.Path, TLD: a.projects.TLD(),
			SSL: project.SSL, MainProxyPort: proxyPort,
		}, nil
	}
	return projectroutes.Project{}, i18n.Errorf("errors.routes.projectNotFound", map[string]any{"name": name})
}

func (a *App) resolveRouteTarget(ctx context.Context, project projectroutes.Project, targetPath string) (projectroutes.Target, error) {
	targetAbs, err := filepath.Abs(strings.TrimSpace(targetPath))
	if err != nil {
		return projectroutes.Target{}, err
	}
	mainAbs, err := filepath.Abs(project.MainPath)
	if err != nil {
		return projectroutes.Target{}, err
	}
	if sameWindowsPath(targetAbs, mainAbs) {
		return projectroutes.Target{Path: mainAbs, Kind: projectroutes.TargetMainCheckout}, nil
	}
	if _, err := os.Stat(targetAbs); err != nil {
		return projectroutes.Target{}, i18n.Wrapf(err, "errors.routes.routeTargetFailed", nil)
	}
	if err := pathInside(paths.WorktreesRoot(), targetAbs); err != nil {
		return projectroutes.Target{}, err
	}
	info := a.gitworkspaces.Inspect(ctx, mainAbs)
	if !info.Repository {
		return projectroutes.Target{}, i18n.Errorf("errors.routes.projectNotGitRepository", nil)
	}
	for _, worktree := range info.Worktrees {
		if sameWindowsPath(worktree.Path, targetAbs) {
			return projectroutes.Target{Path: targetAbs, Kind: projectroutes.TargetWorktree}, nil
		}
	}
	return projectroutes.Target{}, i18n.Errorf("errors.routes.routeTargetNotRegisteredWorktree", nil)
}

func (a *App) applyProjectRouteSet(ctx context.Context, project projectroutes.Project, before, after []projectroutes.Route) error {
	if a.cfg.Get().Projects.Paused[project.Name] {
		return a.removePausedRouteVhosts(project.Name, append(append([]projectroutes.Route(nil), before...), after...), true)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	folders := map[string]bool{}
	for _, route := range append(append([]projectroutes.Route(nil), before...), after...) {
		folders[routeFolder(project.Name, route)] = true
	}
	pathsToSnapshot := make([]string, 0, len(folders)*3)
	for folder := range folders {
		// Every server whose route file renderRouteFiles may write or delete
		// has to be snapshotted, or a failed apply restores some servers and
		// leaves the others holding the half-applied config.
		pathsToSnapshot = append(pathsToSnapshot,
			a.nginx.VhostPath(folder), a.apache.VhostPath(folder), a.caddy.VhostPath(folder))
	}
	sort.Strings(pathsToSnapshot)
	snapshots := make([]routeFileSnapshot, 0, len(pathsToSnapshot))
	for _, path := range pathsToSnapshot {
		snapshot, err := snapshotRouteFile(path)
		if err != nil {
			return err
		}
		snapshots = append(snapshots, snapshot)
	}
	hostsSnapshot, err := a.hosts.Snapshot()
	if err != nil {
		return i18n.Wrapf(err, "errors.routes.snapshotHostsFileFailed", nil)
	}
	hostnames := make([]string, 0, len(before)+len(after))
	for _, routes := range [][]projectroutes.Route{before, after} {
		for _, route := range routes {
			hostnames = append(hostnames, route.Hostname)
		}
	}

	certExisted := map[string]bool{}
	for _, route := range after {
		if project.SSL {
			_, err := os.Stat(filepath.Join(paths.NginxCerts(), route.Hostname+".crt"))
			certExisted[route.Hostname] = err == nil
		}
	}

	desired, err := a.renderRouteFilesContext(ctx, project, after)
	if err != nil {
		a.removeNewRouteCerts(certExisted)
		return err
	}
	rollback := func(cause error) error {
		var rollbackErrors []string
		for _, snapshot := range snapshots {
			if restoreErr := restoreRouteFile(snapshot); restoreErr != nil {
				rollbackErrors = append(rollbackErrors, restoreErr.Error())
			}
		}
		if _, restoreErr := a.hosts.RestoreEntries(hostsSnapshot, hostnames); restoreErr != nil {
			rollbackErrors = append(rollbackErrors, restoreErr.Error())
		}
		a.removeNewRouteCerts(certExisted)
		a.restartRunningWebServers()
		if len(rollbackErrors) > 0 {
			// i18n conversion note: the original was
			// fmt.Errorf("%w; rollback errors: %s", cause, ...) — cause first,
			// rollback errors last. i18n.Wrapf's Error() is fixed as
			// "own message: wrapped.Error()", so this now renders as
			// "rollback errors: <list>: <cause>" — reordered but no information
			// lost. Nothing parses this string's structure, only displays it
			// (same class as task-18b/18d's %w-plus-extra-value precedent).
			return i18n.Wrapf(cause, "errors.routes.rollbackErrors", map[string]any{"rollbackErrors": strings.Join(rollbackErrors, "; ")})
		}
		return cause
	}

	for _, path := range pathsToSnapshot {
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		data, keep := desired[path]
		if keep {
			if err := atomicfile.Write(path, data, 0644); err != nil {
				return rollback(i18n.Wrapf(err, "errors.routes.writeRouteVhostFailed", nil))
			}
		} else if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return rollback(i18n.Wrapf(err, "errors.routes.removeOldRouteVhostFailed", nil))
		}
	}

	for _, route := range append(append([]projectroutes.Route(nil), before...), after...) {
		if err := a.hosts.RemoveEntry(route.Hostname); err != nil {
			return rollback(i18n.Wrapf(err, "errors.routes.removeHostsEntryFailed", nil))
		}
	}
	for _, route := range after {
		if err := a.hosts.AddEntry("127.0.0.1", route.Hostname); err != nil {
			return rollback(i18n.Wrapf(err, "errors.routes.addHostsEntryFailed", nil))
		}
	}
	if err := a.validateRunningWebServers(ctx); err != nil {
		return rollback(err)
	}
	if err := a.restartRunningWebServersContext(ctx); err != nil {
		return rollback(err)
	}
	afterHosts := make(map[string]bool, len(after))
	for _, route := range after {
		afterHosts[strings.ToLower(route.Hostname)] = true
		if !project.SSL {
			vhost.RemoveCert(paths.NginxCerts(), route.Hostname)
		}
	}
	for _, route := range before {
		if !afterHosts[strings.ToLower(route.Hostname)] {
			vhost.RemoveCert(paths.NginxCerts(), route.Hostname)
		}
	}
	return nil
}

func (a *App) renderRouteFiles(project projectroutes.Project, routes []projectroutes.Route) (map[string][]byte, error) {
	return a.renderRouteFilesContext(context.Background(), project, routes)
}

func (a *App) renderRouteFilesContext(ctx context.Context, project projectroutes.Project, routes []projectroutes.Route) (map[string][]byte, error) {
	desired := make(map[string][]byte, len(routes)*2)
	for _, route := range routes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := os.Stat(route.TargetPath); err != nil {
			return nil, i18n.Wrapf(err, "errors.routes.routeTargetPathFailed", map[string]any{"path": route.TargetPath})
		}
		detection := projects.Detect(route.TargetPath)
		phpRuntime, _ := a.projectRuntimeSelection(project.Name, route.TargetPath)
		if err := runtimeRequirementError(project.Name, "PHP", phpRuntime); err != nil {
			return nil, err
		}
		if a.watcher != nil {
			_ = a.watcher.AddRuntimePath(project.Name, route.TargetPath)
		}
		if a.services != nil && (a.services.Status("nginx") == "running" || a.services.Status("apache") == "running" || a.services.Status("caddy") == "running") {
			if err := a.ensurePhpFpmRunningContext(ctx, phpRuntime.Version); err != nil {
				return nil, err
			}
		}
		proxyPort := route.ProxyPort
		if route.TargetKind == projectroutes.TargetMainCheckout && project.MainProxyPort > 0 {
			proxyPort = project.MainProxyPort
		}
		folder := routeFolder(project.Name, route)
		spec := vhost.VhostSpec{
			ServerName: route.Hostname, DocRoot: detection.DocRoot, Kind: string(detection.Kind),
			LogPrefix: folder, FastCGIPort: php.PortFor(phpRuntime.Version),
			FastCGIUpstream: php.UpstreamName(phpRuntime.Version), ProxyPort: proxyPort,
		}
		if project.SSL {
			// Same auto-trust policy as applyVhost: automatic for an MSI
			// install, explicit (TrustLocalCA) for a portable copy.
			cert, key, err := vhost.EnsureSelfSignedCert(paths.NginxCerts(), route.Hostname, !paths.IsPortable())
			if err != nil {
				return nil, i18n.Wrapf(err, "errors.routes.routeCertificateFailed", map[string]any{"hostname": route.Hostname})
			}
			spec.SSL, spec.CertFile, spec.KeyFile = true, cert, key
			a.refreshLocalPHPTrust()
		}
		desired[a.nginx.VhostPath(folder)] = vhost.RenderNginxVhost(folder, spec)
		if _, err := os.Stat(paths.ApacheRoot()); err == nil {
			desired[a.apache.VhostPath(folder)] = vhost.RenderApacheVhost(folder, spec)
		}
		// Caddy gets its route file for the same reason Apache does: the
		// config is written for every installed server so switching the
		// running one needs no regeneration. Without this the delete half in
		// removeVhostFor still runs and switching to Caddy silently drops
		// every route — the hostname resolves and Caddy has no site block.
		if _, err := os.Stat(paths.CaddyRoot()); err == nil {
			desired[a.caddy.VhostPath(folder)] = vhost.RenderCaddyfile(folder, spec)
		}
	}
	return desired, nil
}

func (a *App) validateRunningWebServers(ctx context.Context) error {
	if a.services.Status("nginx") == "running" {
		if err := runWebConfigTest(ctx, paths.NginxExe(), paths.NginxRoot(), "-t", "-p", paths.NginxRoot(), "-c", `conf\nginx.conf`); err != nil {
			return i18n.Wrapf(err, "errors.routes.nginxConfigTestFailed", nil)
		}
	}
	if a.services.Status("apache") == "running" {
		if err := runWebConfigTest(ctx, paths.ApacheExe(), paths.ApacheRoot(), "-t", "-f", paths.ApacheConf()); err != nil {
			return i18n.Wrapf(err, "errors.routes.apacheConfigTestFailed", nil)
		}
	}
	if a.services.Status("caddy") == "running" {
		if err := runWebConfigTest(ctx, paths.CaddyExe(), paths.CaddyRoot(),
			"validate", "--config", paths.CaddyFile(), "--adapter", "caddyfile"); err != nil {
			return i18n.Wrapf(err, "errors.routes.caddyConfigTestFailed", nil)
		}
	}
	return nil
}

func runWebConfigTest(ctx context.Context, executable, workdir string, args ...string) error {
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = workdir
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	output, err := command.CombinedOutput()
	if err != nil {
		// i18n conversion note: the original was fmt.Errorf("%w: %s", err,
		// output) — err first, output last. i18n.Wrapf's Error() is fixed as
		// "own message: wrapped.Error()", so this now renders as
		// "<output>: <err>" — reordered but no information lost. Nothing
		// parses this string's structure, only displays it.
		return i18n.Wrapf(err, "errors.routes.configTestCommandFailed", map[string]any{"output": strings.TrimSpace(string(output))})
	}
	return nil
}

func (a *App) restartRunningWebServers() error {
	return a.restartRunningWebServersContext(context.Background())
}

func (a *App) restartRunningWebServersContext(ctx context.Context) error {
	var first error
	for _, name := range []string{"nginx", "apache", "caddy"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if a.services.Status(name) == "running" {
			if err := a.services.RestartContext(ctx, name); err != nil && first == nil {
				first = i18n.Wrapf(err, "errors.routes.restartServiceFailed", map[string]any{"name": name})
			}
		}
	}
	return first
}

func (a *App) removeNewRouteCerts(existed map[string]bool) {
	for host, alreadyExisted := range existed {
		if !alreadyExisted {
			vhost.RemoveCert(paths.NginxCerts(), host)
		}
	}
}

func snapshotRouteFile(path string) (routeFileSnapshot, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return routeFileSnapshot{path: path}, nil
	}
	if err != nil {
		return routeFileSnapshot{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return routeFileSnapshot{}, err
	}
	return routeFileSnapshot{path: path, data: data, mode: info.Mode(), exists: true}, nil
}

func restoreRouteFile(snapshot routeFileSnapshot) error {
	if !snapshot.exists {
		if err := os.Remove(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return atomicfile.Write(snapshot.path, snapshot.data, snapshot.mode)
}

func routeFolder(projectName string, route projectroutes.Route) string {
	if route.Canonical {
		return projectName
	}
	id := strings.Map(func(char rune) rune {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' {
			return char
		}
		return '-'
	}, route.ID)
	return projectName + "--preview--" + id
}

func sameWindowsPath(left, right string) bool {
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

func pathInside(root, candidate string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return i18n.Errorf("errors.routes.routeTargetOutsideManagedDir", nil)
	}
	return nil
}

func routesNeedManagedApply(routes []projectroutes.Route, mainPath string) bool {
	if len(routes) > 1 {
		return true
	}
	return len(routes) == 1 && (!routes[0].Canonical || !sameWindowsPath(routes[0].TargetPath, mainPath))
}

// Wails route API.
func (a *App) ListProjectRoutes(projectName string) ([]projectroutes.Route, error) {
	return a.routes.List(a.appContext(), projectName)
}

func (a *App) SuggestWorktreePreviewHostname(projectName, worktreeName string) string {
	return a.routes.SuggestPreview(projectName, a.projects.TLD(), worktreeName)
}

func (a *App) SetCanonicalRouteTarget(projectName, targetPath string, proxyPort int) ([]projectroutes.Route, error) {
	return a.routes.SetCanonical(a.appContext(), projectName, targetPath, proxyPort)
}

func (a *App) CreateWorktreePreviewRoute(projectName, targetPath, hostname string, proxyPort int) (projectroutes.Route, error) {
	if err := a.requireAgentSessionsEnabled(); err != nil {
		return projectroutes.Route{}, err
	}
	return a.routes.CreatePreview(a.appContext(), projectName, targetPath, hostname, proxyPort)
}

func (a *App) RemoveProjectRoute(projectName, routeID string) ([]projectroutes.Route, error) {
	return a.routes.Remove(a.appContext(), projectName, routeID)
}

func (a *App) RestoreCanonicalRoute(projectName string) ([]projectroutes.Route, error) {
	return a.routes.RestoreCanonical(a.appContext(), projectName)
}

func (a *App) ConfigureWorktreeRouting(projectName, targetPath string, canonical bool, previewHostname string, proxyPort int) ([]projectroutes.Route, error) {
	if err := a.requireAgentSessionsEnabled(); err != nil {
		return nil, err
	}
	return a.routes.ConfigureWorktree(a.appContext(), projectName, targetPath, canonical, previewHostname, proxyPort)
}

func (a *App) appContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

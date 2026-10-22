package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	xssh "golang.org/x/crypto/ssh"

	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/cloud"
	"lara4ever/internal/config"
	"lara4ever/internal/credentials"
	pkgcron "lara4ever/internal/cron"
	"lara4ever/internal/deploycoord"
	"lara4ever/internal/deployments"
	"lara4ever/internal/devserver"
	"lara4ever/internal/documentshutdown"
	"lara4ever/internal/editor"
	"lara4ever/internal/gitworkspace"
	"lara4ever/internal/hotkey"
	"lara4ever/internal/i18n"
	"lara4ever/internal/installer"
	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/netports"
	"lara4ever/internal/nodejs"
	pkgnotifications "lara4ever/internal/notifications"
	"lara4ever/internal/paths"
	"lara4ever/internal/php"
	"lara4ever/internal/projectroutes"
	"lara4ever/internal/projects"
	pkgpty "lara4ever/internal/pty"
	"lara4ever/internal/queueworker"
	"lara4ever/internal/relocate"
	"lara4ever/internal/services"
	"lara4ever/internal/sourcecontrol"
	pkgssh "lara4ever/internal/ssh"
	"lara4ever/internal/sysmetrics"
	"lara4ever/internal/terminal"
	"lara4ever/internal/tray"
	"lara4ever/internal/updater"
	"lara4ever/internal/vcruntime"
	"lara4ever/internal/version"
	"lara4ever/internal/vhost"
	"lara4ever/internal/workspacefiles"

	"github.com/wailsapp/wails/v2/pkg/options"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	doctorMu                  sync.Mutex
	doctorOperations          map[string]doctorOperation
	doctorPendingCancellation map[string]time.Time
	projectRuntimeMu          sync.Mutex
	projectRuntimeSnapshots   map[string]projectRuntimeSnapshot
	projectRuntimeApplied     map[string]projectRuntimeSnapshot
	mcpMu                     sync.Mutex
	mcpServer                 *mcpbridge.Server
	mcpReady                  bool
	mcpMessage                string
	mcpCalls                  sync.Mutex
	mcpGate                   chan struct{}
	mcpActivityMu             sync.Mutex
	mcpActivity               []MCPActivity
	ctx                       context.Context
	cfg                       *config.Store
	services                  *services.Manager
	php                       *php.Manager
	nodejs                    *nodejs.Manager
	hosts                     *vhost.HostsEditor
	nginx                     *vhost.NginxConfigurator
	apache                    *vhost.ApacheConfigurator
	caddy                     *vhost.CaddyConfigurator
	terminals                 *pkgpty.Registry
	inst                      *installer.Manager
	projects                  *projects.Scanner
	workspaceFiles            *workspacefiles.Service
	sourceControl             *sourcecontrol.Service
	creator                   *projects.Creator
	watcher                   *projects.Watcher
	sysmet                    *sysmetrics.Sampler
	ssh                       *pkgssh.Manager
	sftp                      *pkgssh.SFTPRegistry
	sshClients                sync.Map // termId -> *xssh.Client (for SFTP layering); kept here to avoid a cycle with pkgssh
	updater                   *updater.Manager
	crons                     *pkgcron.Manager
	cloud                     *cloud.Client
	cloudSyncEmit             func(cloud.SyncStatus)
	localDeployments          deploymentRepository
	localDeploymentsErr       error
	deploymentCoordinator     deploycoord.Coordinator
	deploymentExecutor        deploymentExecutor
	deploymentEmit            func(string, any)
	devsrv                    *devserver.Manager
	queue                     *queueworker.Manager
	agents                    *pkgagents.Manager
	agentUsage                *pkgagents.UsageManager
	agentActivity             *appAgentActivity
	agentStoreNotice          *pkgagents.StoreNotice
	notifications             *pkgnotifications.Manager
	notifyStoreNotice         *pkgnotifications.StoreNotice
	routeStoreNotice          *projectroutes.StoreNotice
	gitworkspaces             *gitworkspace.Manager
	routes                    *projectroutes.Manager

	// quitting becomes true once the user explicitly chose Quit (from the tray
	// menu or the in-app menu). beforeClose checks it to decide whether to
	// hide the window or let Wails actually close.
	quitting         atomic.Bool
	documentGateOnce sync.Once
	documentGate     *documentshutdown.Gate
	documentEffects  documentShutdownEffects
	trayIcon         []byte

	// hotkeys owns the optional OS-wide global hotkey that summons the palette.
	hotkeys *hotkey.Manager

	// detached marks this process as a satellite "terminal window" launched
	// via `--terminal-window <seedFile>`. It runs a minimal lifecycle (no
	// tray, no service/stack management) and the frontend boots straight into
	// the terminal canvas seeded from detachedSeedFile.
	detached         bool
	detachedSeedFile string
	detachedSeed     string

	// commandBar marks this process as the standalone Spotlight-style command
	// bar launched via `--command-bar`. It runs a minimal window showing only
	// the palette and forwards the chosen action to the main process.
	commandBar bool
}

// followRelocation rewrites the stored absolute paths that cannot re-derive
// themselves after the environment moved, and returns a line describing what
// happened (empty when nothing moved).
//
// The startup vhost sweep rebuilds vhosts, hosts entries and certificates.
// PHP's generated CA paths need an explicit relocation repair so custom trust
// choices stay intact. EnsureIni repairs the absolute extension_dir too.
func followRelocation(previousRoot string) string {
	if previousRoot == "" {
		return ""
	}
	current := paths.Root()
	moved := 0
	for _, f := range []string{paths.ProjectRoutesFile(), paths.AgentSessionsFile()} {
		n, err := relocate.JSONFile(f, previousRoot, current)
		if err != nil {
			fmt.Printf("[relocate] %s: %v\n", filepath.Base(f), err)
			continue
		}
		moved += n
	}

	inis := 0
	if versions, err := php.NewManager(paths.PHPRoot()).List(); err == nil {
		for _, v := range versions {
			if err := php.RebaseCaCertIni(v.Path, filepath.Join(previousRoot, "php", v.Version)); err != nil {
				fmt.Printf("[relocate] PHP CA paths %s: %v\n", v.Version, err)
			}
			if err := php.EnsureIni(v.Path); err != nil {
				fmt.Printf("[relocate] php.ini %s: %v\n", v.Version, err)
				continue
			}
			inis++
		}
	}

	// i18n.T is safe here: main.go calls LoadCatalogues before NewApp, and
	// NewApp sets the active language immediately after loading cfg — both
	// happen before followRelocation is ever called. See LoadCatalogues's and
	// NewApp's doc comments.
	return i18n.T("backend.app.relocationMessage", map[string]any{
		"previousRoot": previousRoot,
		"moved":        moved,
		"inis":         inis,
	})
}

func NewApp() *App {
	cfg, err := config.NewStore(paths.ConfigFile())
	if err != nil {
		panic(err)
	}
	// i18n.Global() is already populated — main.go calls LoadCatalogues before
	// NewApp (see LoadCatalogues's doc comment). Set the active language as
	// early as cfg allows so every translatable error constructed anywhere
	// else in NewApp, not just after it returns, renders in the user's actual
	// language instead of the English fallback.
	i18n.SetActive(resolveLanguage(cfg))
	// The whole environment was moved since it last ran - a portable copy
	// carried to another drive, or a renamed folder. config.NewStore already
	// followed the move for its own fields; the state files have to be fixed
	// before the stores below read them, or every session and route would
	// still point at a path that no longer exists.
	relocationNotice := followRelocation(cfg.RelocatedFrom())

	root := cfg.Get().Projects.Root
	phpMgr := php.NewManager(paths.PHPRoot())
	nodeMgr := nodejs.NewManager(paths.NodeRoot())
	scanner := projects.NewScanner(cfg)
	agentCatalog := pkgagents.NewCatalog()
	var agentStoreNotice *pkgagents.StoreNotice
	agentStore, err := pkgagents.NewSessionStore(paths.AgentSessionsFile(), func(notice pkgagents.StoreNotice) {
		agentStoreNotice = &notice
	})
	if err != nil {
		panic(err)
	}
	routeStore, err := projectroutes.NewStore(paths.ProjectRoutesFile())
	if err != nil {
		panic(err)
	}
	notificationStore, notifyStoreNotice, err := pkgnotifications.NewStore(paths.NotificationsFile())
	if err != nil {
		panic(err)
	}
	if relocationNotice != "" {
		// Silent would be wrong: paths the user may have typed themselves
		// changed underneath them. Deduped on the previous root so carrying
		// the same stick between the same two machines does not pile up.
		//
		// Title and Message (the latter built by followRelocation above) are
		// translated via i18n.T. This is safe even though it runs inside
		// NewApp: main.go loads catalogues into i18n.Global() before calling
		// NewApp, and NewApp sets the active language immediately after
		// loading cfg (both above this point) — see LoadCatalogues's and
		// NewApp's own doc comments for why that ordering matters.
		_, _, _ = notificationStore.Add(pkgnotifications.Entry{
			ID:        uuid.NewString(),
			DedupeKey: "relocated:" + cfg.RelocatedFrom(),
			Category:  pkgnotifications.CategoryCompletion,
			Severity:  "info",
			Title:     i18n.T("backend.app.relocationTitle", nil),
			Message:   relocationNotice,
			CreatedAt: time.Now().UTC(),
		})
	}
	a := &App{
		cfg:       cfg,
		services:  services.NewManager(cfg),
		php:       phpMgr,
		nodejs:    nodeMgr,
		hosts:     vhost.NewHostsEditor(),
		nginx:     vhost.NewNginxConfigurator(paths.NginxSites()),
		apache:    vhost.NewApacheConfigurator(paths.ApacheSites()),
		caddy:     vhost.NewCaddyConfigurator(paths.CaddySites()),
		terminals: pkgpty.NewRegistry(),
		projects:  scanner,
		creator: func() *projects.Creator {
			c := projects.NewCreator(root, paths.Cache(), paths.Bin(), phpMgr.ExeFor)
			c.SetNodeResolver(func() string {
				p, _ := nodeMgr.ActivePath()
				return p
			})
			return c
		}(),
		watcher: projects.NewWatcher(root),
		sysmet:  sysmetrics.NewSampler(),
		ssh: func() *pkgssh.Manager {
			m := pkgssh.NewManager(paths.SSHRoot(), paths.SSHKnownHosts(), paths.SSHHostDir)
			// Wire host lookup so ProxyJump can resolve other saved hosts
			// for chained connections. We read from `cfg` directly because
			// `a` doesn't exist yet inside this struct literal.
			m.SetHostLookup(func(name string) (config.SSHHost, bool) {
				for _, h := range cfg.ListSSHHosts() {
					if h.Name == name {
						return h, true
					}
				}
				return config.SSHHost{}, false
			})
			return m
		}(),
		updater:           updater.NewManager(filepath.Join(paths.Cache(), "updates"), version.Version, paths.IsPortable(), nil),
		cloud:             cloud.NewClient(cfg),
		agentStoreNotice:  agentStoreNotice,
		notifyStoreNotice: notifyStoreNotice,
		routeStoreNotice:  routeStore.Notice(),
		gitworkspaces:     gitworkspace.NewManager(paths.WorktreesRoot()),
	}
	a.localDeployments, a.localDeploymentsErr = deployments.NewStore(filepath.Join(paths.StateRoot(), "deployments"), credentials.System{})
	a.deploymentExecutor = sshDeploymentExecutor{manager: a.ssh}
	a.cloudSyncEmit = func(status cloud.SyncStatus) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "cloud:sync-status-changed", status)
		}
	}
	a.routes = projectroutes.NewManager(routeStore, a.resolveRouteProject, a.resolveRouteTarget, a.applyProjectRouteSet)
	// Keep notifications inside LaraEnv for now. The previous WinRT/PowerShell
	// toast dependency made the unsigned Windows binary match Defender's
	// Wacatac machine-learning heuristic. OS toasts can return after we have a
	// signed, independently scanned implementation.
	a.notifications = pkgnotifications.NewManager(notificationStore, nil, func(event string, data ...any) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, event, data...)
		}
	})
	a.initAgentActivity(paths.AgentActivityRoot())
	a.agents = pkgagents.NewManager(
		agentCatalog, agentStore, a.terminals,
		func(options pkgagents.AgentLaunchOptions) (string, string, string, error) {
			if options.Mode == "" {
				options.Mode = pkgagents.LaunchCurrent
			}
			list, listErr := scanner.List()
			if listErr != nil {
				return "", "", "", listErr
			}
			for _, project := range list {
				if project.Name == options.ProjectName {
					if options.Mode == pkgagents.LaunchBranch && a.agents != nil && a.agents.HasLiveSharedCheckout(project.Name) {
						return "", "", "", i18n.Errorf("errors.app.agentUsingMainCheckout", nil)
					}
					resolved, resolveErr := a.gitworkspaces.Resolve(context.Background(), project.Path, gitworkspace.ResolveRequest{
						Mode: gitworkspace.Mode(options.Mode), Branch: options.Branch,
						CreateBranch: options.CreateBranch, BaseRef: options.BaseRef, WorktreeName: options.WorktreeName,
					})
					if resolveErr != nil {
						return "", "", "", resolveErr
					}
					return resolved.Path, resolved.Branch, resolved.WorktreePath, nil
				}
			}
			return "", "", "", i18n.Errorf("errors.app.projectNotFound", map[string]any{"name": options.ProjectName})
		},
		func(projectName string) []string {
			var dirs []string
			if version := a.resolveProjectPhp(projectName); version != "" {
				dirs = append(dirs, a.php.VersionDir(version))
			}
			if nodeDir := a.nodejs.ResolvePath(a.resolveProjectNode(projectName)); nodeDir != "" {
				dirs = append(dirs, nodeDir)
			}
			if _, statErr := os.Stat(filepath.Join(paths.ComposerRoot(), "composer.bat")); statErr == nil {
				dirs = append(dirs, paths.ComposerRoot())
			}
			return dirs
		},
		func(event string, data ...any) {
			if event == "agent:session-changed" && len(data) > 0 {
				if session, ok := data[0].(pkgagents.AgentSession); ok {
					focused := a.agents != nil && a.agents.IsFocused(session.ID)
					a.notifications.ObserveSession(session, focused)
				}
			}
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, event, data...)
			}
		},
		a.foreignActivityRoot,
	)
	a.agentUsage = pkgagents.NewUsageManager(agentCatalog)
	a.workspaceFiles = workspacefiles.NewService(a.resolveWorkspaceSource)
	a.sourceControl = sourcecontrol.NewService(a.workspaceFiles.OpenContext, func(event sourcecontrol.OperationEvent) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "sourcecontrol:operation", event)
		}
	})
	if executable, executableErr := os.Executable(); executableErr == nil {
		a.agents.SetEventExecutable(executable)
	}
	a.agents.SetPathResolver(a.resolveAgentLocation)
	a.agents.SetRuntimePathResolver(a.projectRuntimePaths)

	// Cron manager — wires config persistence, project-aware PATH/cwd
	// resolvers. Hydration + engine start happens in startup() once ctx
	// is available.
	a.crons = pkgcron.NewManager(
		func(jobs []pkgcron.Job) error {
			raws := make([]map[string]any, 0, len(jobs))
			for _, j := range jobs {
				b, _ := json.Marshal(j)
				var m map[string]any
				_ = json.Unmarshal(b, &m)
				raws = append(raws, m)
			}
			return cfg.SetCrons(raws)
		},
		func(projectName, phpOverride string) []string {
			version := phpOverride
			if version == "" && projectName != "" {
				version = a.resolveProjectPhp(projectName)
			}
			var out []string
			if version != "" {
				out = append(out, phpMgr.VersionDir(version))
			}
			nodeOverride := ""
			if projectName != "" {
				nodeOverride = cfg.Get().Projects.NodeVersions[projectName]
			}
			if nodeDir := nodeMgr.ResolvePath(nodeOverride); nodeDir != "" {
				out = append(out, nodeDir)
			}
			return out
		},
		func(projectName, override string) string {
			if override != "" {
				return override
			}
			if projectName == "" {
				return ""
			}
			return filepath.Join(scanner.Root(), projectName)
		},
	)
	a.crons.SetRuntimePathResolver(a.cronRuntimePaths)
	a.devsrv = devserver.NewManager(
		func(event string, data ...any) {
			if event == "devserver:status" && len(data) > 0 {
				if instanceID, ok := data[0].(string); ok {
					if _, worktree := agentWorktreeSessionID(instanceID); worktree {
						go a.syncAgentWorktreeRoute(instanceID)
					}
				}
			}
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, event, data...)
			}
		},
		func(projectPath, prefer string) (string, string, error) {
			pm := prefer
			if pm == "" {
				pm = "npm"
			}
			name := a.projectNameForRuntimePath(projectPath)
			_, nodeRuntime := a.projectRuntimeSelection(name, projectPath)
			if err := runtimeRequirementError(name, "Node", nodeRuntime); err != nil {
				return "", pm, err
			}
			nodeDir := ""
			if nodeRuntime.Version != "" {
				nodeDir = nodeMgr.VersionDir(nodeRuntime.Version)
			}
			bin, err := projects.ResolvePackageManagerPublic(pm, nodeDir)
			return bin, pm, err
		},
		func(projectName string) []string {
			// PATH bin dirs in priority order: PHP → Node → Composer. This
			// mirrors the embedded terminal so custom dev commands can resolve
			// bare `php` / `composer` (issue #3) without full executable paths.
			var dirs []string
			if v := a.resolveProjectPhp(projectName); v != "" {
				if d := a.php.VersionDir(v); d != "" {
					dirs = append(dirs, d)
				}
			}
			if nd := nodeMgr.ResolvePath(a.resolveProjectNode(projectName)); nd != "" {
				dirs = append(dirs, nd)
			}
			if _, err := os.Stat(filepath.Join(paths.ComposerRoot(), "composer.bat")); err == nil {
				dirs = append(dirs, paths.ComposerRoot())
			}
			return dirs
		},
		func(projectName string) []string {
			// Hostnames the reverse proxy forwards with `Host:` — the main
			// vhost plus any worktree routes. Exported to Vite so it doesn't
			// 403 the proxied request ("This host is not allowed").
			tld := a.projects.TLD()
			if tld == "" {
				tld = "test"
			}
			hosts := []string{projectName + "." + tld}
			if a.routes != nil {
				if routes, err := a.routes.List(a.appContext(), projectName); err == nil {
					for _, r := range routes {
						if r.Hostname != "" {
							hosts = append(hosts, r.Hostname)
						}
					}
				}
			}
			return hosts
		},
	)
	a.queue = queueworker.NewManager(
		func(event string, data ...any) {
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, event, data...)
			}
		},
		func(projectName string) string {
			return a.GetResolvedPhpExe(projectName)
		},
	)
	a.devsrv.SetRuntimePathResolver(a.projectRuntimePaths)
	a.devsrv.SetProjectPackageManagerResolver(func(name, path, prefer string, dirs []string) (string, string, error) {
		pm := prefer
		if pm == "" {
			pm = "npm"
		}
		nodeDir := ""
		for _, dir := range dirs {
			if sameWindowsPath(filepath.Dir(dir), a.nodejs.VersionDir("")) {
				if info, err := os.Stat(filepath.Join(dir, "node.exe")); err != nil || !info.Mode().IsRegular() {
					return "", pm, fmt.Errorf("%s: selected Node executable is unavailable", name)
				}
				nodeDir = dir
				break
			}
		}
		bin, err := projects.ResolvePackageManagerPublic(pm, nodeDir)
		return bin, pm, err
	})
	a.creator.SetProjectNodeResolver(func(name, path string) (string, error) {
		_, resolution := a.projectRuntimeSelection(name, path)
		if err := runtimeRequirementError(name, "Node", resolution); err != nil {
			return "", err
		}
		if resolution.Version == "" {
			return "", nil
		}
		return a.nodejs.VersionDir(resolution.Version), nil
	})
	a.sftp = pkgssh.NewSFTPRegistry(func(event string, payload pkgssh.SFTPProgress) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, event, payload)
		}
	})
	a.sftp.SetRawEmitter(func(event string, data any) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, event, data)
		}
	})
	a.configureLocalTLS()
	return a
}

// SetTrayIcon receives the embedded icon bytes from main before startup.
func (a *App) SetTrayIcon(b []byte) { a.trayIcon = b }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startAgentActivity(ctx)
	go a.watchLanguage()
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			_ = pkgagents.DrainSessionSignals(paths.AgentEventsRoot(), a.agents.ObserveSignal)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	// Re-install agent hooks synchronously so it completes before the UI can toggle anything,
	// preventing a race where the user disables hooks while re-install is in flight.
	if a.agentHooksWanted() {
		if executable, err := os.Executable(); err == nil {
			pkgagents.InstallHooks(a.agentHookPaths(), executable)
		}
	}
	if a.agentStoreNotice != nil {
		notice := *a.agentStoreNotice
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1500 * time.Millisecond):
				wruntime.EventsEmit(ctx, "app:notice", i18n.T("backend.app.storeNoticePreservedAt", map[string]any{"message": notice.Message, "path": notice.PreservedPath}))
			}
		}()
	}
	if a.routeStoreNotice != nil {
		notice := *a.routeStoreNotice
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1700 * time.Millisecond):
				wruntime.EventsEmit(ctx, "app:notice", i18n.T("backend.app.storeNoticePreservedAt", map[string]any{"message": notice.Message, "path": notice.PreservedPath}))
			}
		}()
	}
	if a.notifyStoreNotice != nil {
		notice := *a.notifyStoreNotice
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1900 * time.Millisecond):
				wruntime.EventsEmit(ctx, "app:notice", i18n.T("backend.app.storeNoticePreservedAt", map[string]any{"message": notice.Message, "path": notice.PreservedPath}))
			}
		}()
	}

	// Clamp window to primary screen bounds so it never overflows on small
	// displays (e.g. 1366×768). Runs before tray init so the window is
	// correctly sized from the first visible paint.
	if screens, err := wruntime.ScreenGetAll(ctx); err == nil {
		for _, s := range screens {
			if s.IsPrimary || s.IsCurrent {
				const taskbarReserve = 80
				w, h := 1280, 760
				if maxH := s.Size.Height - taskbarReserve; h > maxH {
					h = maxH
				}
				if w > s.Size.Width {
					w = s.Size.Width
				}
				wruntime.WindowSetSize(ctx, w, h)
				wruntime.WindowCenter(ctx)
				break
			}
		}
	}

	// System tray with mirror of the most useful actions. Closing the main
	// window will only hide it (see beforeClose) so services keep running in
	// the background until the user explicitly picks Quit from the tray.
	tray.Run(ctx, a.trayIcon, tray.Callbacks{
		Services: func() []tray.Service {
			defs := a.services.Active()
			out := make([]tray.Service, len(defs))
			for i, d := range defs {
				out[i] = tray.Service{Name: d.Name, Label: d.Label}
			}
			return out
		},
		Status:       a.services.Status,
		StartService: a.services.Start,
		StopService:  a.services.Stop,
		StartAll: func() {
			// Mirrors StartManagedAll: one service failing (two exclusive
			// web servers both enabled, say) must not stop the cascade, but
			// it must not vanish either — without the event the user just
			// finds one service silently missing.
			for _, d := range a.services.Active() {
				if err := a.services.Start(d.Name); err != nil {
					wruntime.EventsEmit(a.ctx, "service:error", services.ErrorEvent{
						Service:   d.Name,
						Message:   err.Error(),
						RedistURL: vcruntime.URLFromError(err),
					})
				}
			}
		},
		StopAll: func() {
			for _, d := range a.services.Active() {
				_ = a.services.Stop(d.Name)
			}
		},
		Projects: func() []tray.Project {
			list, err := a.ListProjects()
			if err != nil {
				return nil
			}
			out := make([]tray.Project, len(list))
			for i, p := range list {
				out[i] = tray.Project{Name: p.Name, URL: p.URL, Tags: p.Tags}
			}
			return out
		},
		OpenFolder:   a.OpenProjectFolder,
		OpenEditor:   func(name string) error { return a.OpenProjectInEditor(name, "") },
		OpenTerminal: func(name string) error { return a.OpenProjectTerminal(name, "") },
		OpenBrowser:  a.OpenURL,
		Quit:         a.QuitApp,
		Labels:       func(key string) string { return a.T(key, nil) },
	})

	// Global hotkey: summon the command palette OS-wide (even when hidden to
	// tray). onTrigger runs on the hotkey thread; bring the window forward and
	// tell the frontend to open the palette.
	a.hotkeys = hotkey.New(func() { a.openCommandBar() })
	if hk := a.cfg.GetCommandCenter().GlobalHotkey; hk != "" {
		_ = a.hotkeys.Set(hk)
	}

	// Watch for command intents dropped by the standalone command-bar window.
	go a.watchCmdBarInbox()

	// Watch the re-attach inbox so detached terminal windows can drop a group
	// back into this main window. Main process only.
	go a.watchAttachInbox()

	// Reclaim dev-server ports held by stray php/node left over from a previous
	// instance (e.g. the app was killed without a clean shutdown). Strictly
	// scoped to ports LaraEnv itself reserved + our own stack executables, so it
	// can never touch unrelated software. The single-instance lock prevents the
	// common cause (a duplicate process); this covers crashes.
	go a.reclaimDevPortOrphans()

	// Auto-activate the highest installed PHP version if no `current` symlink
	// exists yet (or it dangles at a removed folder). Covers users who
	// installed PHP before the auto-activate-on-install fix landed and never
	// went through Dashboard → Runtimes manually. Best-effort.
	if picked, err := a.php.EnsureActive(); err != nil {
		fmt.Printf("[app] EnsureActive on startup: %v\n", err)
	} else if picked != "" {
		fmt.Printf("[app] auto-activated PHP %s on startup\n", picked)
	}

	// Run each installed PHP through CA-bundle setup so HTTPS works out of
	// the box (cURL error 60 fix). Best-effort, non-blocking — startup
	// shouldn't wait on a 230 KB download from curl.se.
	go func() {
		versions, err := a.php.List()
		if err != nil {
			return
		}
		for _, v := range versions {
			if err := php.EnsureCaCert(v.Path, vhost.LocalCACertPath(paths.NginxCerts())); err != nil {
				fmt.Printf("[app] EnsureCaCert(%s) on startup: %v\n", v.Version, err)
			}
		}
	}()

	// Registra um php-fpm por versão de PHP instalada e amarra ao web server
	// como dependência. Cada FPM escuta numa porta derivada da versão (8.4→9084).
	a.refreshPhpFpmServices()

	// Mata qualquer processo de serviço conhecido que tenha sobrado de uma
	// execução anterior do app (crash, fechamento abrupto, etc.). MUST run
	// after refreshPhpFpmServices: KillOrphans only reaps binaries that have a
	// registered definition, and the php-fpm defs are what put php-cgi.exe on
	// that list. When it ran earlier, orphan php-cgi workers from a killed
	// instance survived holding the FPM ports (9074/9081/…), so every fresh
	// replica failed to bind and crash-looped through the auto-restart backoff
	// — the "PHP processes dying and respawning" churn on restart.
	a.services.KillOrphans()

	// Garante que nginx.conf inclua nossos vhosts em conf\sites\*.conf.
	// Best-effort: silencioso se nginx ainda não foi instalado. Installing
	// nginx mid-session runs the same routine from afterNginxInstalled, so
	// it is startable without restarting the app.
	a.ensureNginxConfigured()

	// Same for Apache: only patches the file when Apache is actually
	// installed (the file exists). Loads required modules, includes our
	// per-project vhosts, and applies the user's tuning. Installing Apache
	// mid-session runs the same routine from afterApacheInstalled.
	a.ensureApacheConfigured()

	// Same for Caddy: only when it's installed. Installing it mid-session
	// runs the same routine from afterCaddyInstalled, so it is startable
	// without restarting the app.
	if _, err := os.Stat(paths.CaddyRoot()); err == nil {
		if err := a.ensureCaddyConfigured(); err != nil {
			fmt.Printf("[caddy] Caddyfile: %v\n", err)
		}
	}

	a.inst = installer.NewManager(paths.Cache(), func(p installer.Progress) {
		wruntime.EventsEmit(ctx, "install:progress", p)
	})

	a.updater.SetEmit(func(p updater.Progress) {
		wruntime.EventsEmit(ctx, "update:progress", p)
	})

	// Cron: hydrate persisted jobs and start the engine. Emit per-run
	// status snapshots on "cron:status" so the Cron page can refresh
	// without polling.
	if exe, err := os.Executable(); err == nil {
		a.crons.SetExePath(exe)
	}
	a.crons.SetEmit(func(j pkgcron.Job) {
		wruntime.EventsEmit(ctx, "cron:status", j)
	})
	a.crons.Start(loadPersistedCrons(a.cfg))

	// Backfill version markers for shared-folder installs that predate
	// the marker file (so old installs aren't reported as missing).
	installer.BackfillMarkers(installer.Catalog())

	// Materialize the bundled composer.phar + wrapper scripts into
	// C:\laraenv\composer\ so `composer` works in the embedded terminal as
	// soon as a PHP version is active — no manual download required. No-op
	// when the user has already installed Composer from the catalog.
	if err := installer.EnsureBundledComposer(paths.ComposerRoot()); err != nil {
		fmt.Printf("[app] EnsureBundledComposer failed: %v\n", err)
	}

	// Reconcile vhost + hosts entries for EVERY project on disk at startup.
	// The fsnotify watcher only sees folders created while the app is open —
	// projects cloned while it was closed (the normal new-machine flow) would
	// otherwise never get a vhost or hosts entry, no matter how many times the
	// app restarts. This sweep also refreshes SSL certs that predate the local
	// CA. Batch mode: no per-project web-server restart (the old per-project
	// Reload bounced nginx AND every php-fpm pool once per SSL project — with
	// dozens of projects that was ~2 minutes of services flapping right after
	// boot) and no FPM auto-start (services stay stopped until the user
	// starts the stack). One web-server restart at the end if it's running.
	a.rememberProjectRuntimes()
	go func() {
		list, err := a.projects.List()
		if err != nil {
			wruntime.EventsEmit(ctx, "project:error", i18n.T("backend.app.vhostSweepFailed", map[string]any{"label": "startup", "error": err}))
			return
		}
		touched := false
		pendingApplied := map[string]projectRuntimeSnapshot{}
		a.projectRuntimeMu.Lock()
		defer a.projectRuntimeMu.Unlock()
		for _, p := range list {
			snapshot := a.runtimeSnapshot(p.Name)
			if err := a.applyVhost(p.Name, false, false); err == nil {
				pendingApplied[p.Name] = snapshot
				touched = true
			} else {
				// Surface instead of swallowing — a failed hosts write here is
				// exactly the "site unreachable on a fresh machine" symptom.
				wruntime.EventsEmit(ctx, "project:error", i18n.T("backend.app.projectVhostError", map[string]any{"name": p.Name, "error": err}))
			}
		}
		if !touched {
			return
		}
		if err := a.restartRunningWebServers(); err != nil {
			wruntime.EventsEmit(ctx, "project:error", err.Error())
			return
		}
		for name, snapshot := range pendingApplied {
			a.projectRuntimeApplied[name] = snapshot
		}
	}()

	// System metrics: emit a sample every ~1.5s while the app is open. The
	// frontend listens on "sys:metrics" and renders the indicators/sparklines.
	go func() {
		t := time.NewTicker(1500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				wruntime.EventsEmit(ctx, "sys:metrics", a.sysmet.Sample(ctx))
			}
		}
	}()

	a.services.SetErrorHandler(func(e services.ErrorEvent) {
		wruntime.EventsEmit(ctx, "service:error", e)
	})

	go a.services.WatchStatus(ctx, func(name, status string) {
		wruntime.EventsEmit(ctx, "service:status", name, status)
	})
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !a.GetAgentSessionsEnabled() {
					continue
				}
				snapshots := a.agentUsage.Refresh(ctx)
				a.notifications.ObserveUsage(snapshots)
				wruntime.EventsEmit(ctx, "agent:usage-changed", snapshots)
			}
		}
	}()

	// Reconcile persisted route ownership after the stack is available. A
	// deleted/moved worktree must never leave the primary project domain broken.
	if list, err := a.projects.List(); err == nil {
		names := make([]string, 0, len(list))
		for _, project := range list {
			names = append(names, project.Name)
		}
		if repairs, repairErr := a.routes.RepairMissingTargets(ctx, names); repairErr != nil {
			wruntime.EventsEmit(ctx, "project:error", i18n.T("backend.app.repairProjectRoutesFailed", map[string]any{"error": repairErr}))
		} else {
			for _, repair := range repairs {
				wruntime.EventsEmit(ctx, "app:notice", i18n.T("backend.app.projectNotice", map[string]any{"name": repair.ProjectName, "message": repair.Message}))
			}
		}
	}

	a.startMCP()
	// Watcher do diretório de projetos: cria/remove vhost+hosts automaticamente.
	go func() {
		_ = a.watcher.Run(ctx,
			func(name string) {
				if err := a.applyVhostFor(name); err != nil {
					wruntime.EventsEmit(ctx, "project:error", err.Error())
				}
				a.refreshPhpFpmServicesWithPrune(false)
				wruntime.EventsEmit(ctx, "project:added", name)
				a.projectRuntimeChanged(name)
			},
			func(name string) {
				if err := a.removeVhostFor(name); err != nil {
					wruntime.EventsEmit(ctx, "project:error", err.Error())
					return
				}
				a.refreshPhpFpmServices() // removed project may free an FPM
				wruntime.EventsEmit(ctx, "project:removed", name)
			},
			a.projectRuntimeChanged,
		)
	}()
}

func (a *App) shutdown(ctx context.Context) {
	a.stopAgentActivity()
	a.cancelDoctorOperations()
	if a.sourceControl != nil {
		a.sourceControl.Close()
	}
	// The command bar owns nothing — never touch services/crons/terminals.
	if a.commandBar {
		return
	}
	// A detached terminal window owns only its own PTYs — it must never tear
	// down the main process's tray, crons, dev servers or services.
	if a.detached {
		for _, session := range a.agents.ListSessions("", true) {
			_ = a.agents.Stop(session.ID)
		}
		_ = a.agents.Flush()
		a.terminals.CloseAll()
		return
	}
	a.stopMCP()
	tray.Stop()
	if a.hotkeys != nil {
		a.hotkeys.Stop()
	}
	a.crons.Stop()
	_ = a.agents.Flush()
	a.terminals.CloseAll()
	if a.devsrv != nil {
		a.devsrv.StopAll()
	}
	if a.queue != nil {
		a.queue.StopAll()
	}
	a.projectRuntimeMu.Lock()
	a.services.StopAll()
	a.projectRuntimeMu.Unlock()
}

// startupDetached is the lifecycle for satellite terminal windows: it wires the
// context and opens the window maximized (the user wants max terminal area), but
// skips the tray, orphan-killing, PHP/FPM/web-server provisioning and cron
// engine that the main window owns.
func (a *App) startupDetached(ctx context.Context) {
	a.ctx = ctx
	a.startAgentActivity(ctx)
	go a.watchLanguage()
	wruntime.WindowMaximise(ctx)
}

// IsDetachedWindow tells the frontend whether it's running inside a satellite
// terminal window (true) or the main app shell (false).
func (a *App) IsDetachedWindow() bool { return a.detached }

// IsCommandBar tells the frontend whether it's the standalone command-bar window.
func (a *App) IsCommandBar() bool { return a.commandBar }

// startupCommandBar is the lifecycle for the standalone command bar: a small
// frameless always-on-top window centered near the top of the primary screen.
func (a *App) startupCommandBar(ctx context.Context) {
	a.ctx = ctx
	go a.watchLanguage()
	wruntime.WindowSetAlwaysOnTop(ctx, true)
	if screens, err := wruntime.ScreenGetAll(ctx); err == nil {
		for _, s := range screens {
			if s.IsPrimary || s.IsCurrent {
				const w = 680
				x := (s.Size.Width - w) / 2
				y := s.Size.Height / 6
				wruntime.WindowSetPosition(ctx, x, y)
				break
			}
		}
	}
	// Grab keyboard focus once the window/webview is up so the user can type
	// immediately, no click needed. The helper re-finds + retries until the
	// window is actually foreground (WebView2 init can take longer than a couple
	// of fixed attempts), then stops.
	go focusWindowByTitleUntilForeground("LaraEnv · Command", 2*time.Second)
}

// openCommandBar launches the standalone command-bar window in a new process
// (fresh process = renders cleanly, no WebView2 black-frame on show).
func (a *App) openCommandBar() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "--command-bar")
	if err := cmd.Start(); err != nil {
		return
	}
	// Let the new process steal foreground so its input gets keyboard focus.
	if cmd.Process != nil {
		allowForeground(cmd.Process.Pid)
	}
}

// cmdBarInboxDir is where the command-bar drops chosen intents and the main
// window watches (separate processes, no direct IPC).
func cmdBarInboxDir() string { return filepath.Join(os.TempDir(), "laraenv-cmdbar") }

// SendCommandIntent is called by the command-bar window to hand the chosen
// command intent (JSON) to the main process for execution.
func (a *App) SendCommandIntent(intentJSON string) error {
	dir := cmdBarInboxDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "tmp-*.json.part")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(intentJSON); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	final := strings.TrimSuffix(tmp.Name(), ".part")
	return os.Rename(tmp.Name(), final)
}

// watchCmdBarInbox (main window only) watches for intents dropped by the
// command-bar window. For actions that need the main UI (navigate, terminals)
// it brings the window forward; then it hands the intent to the frontend
// executor via an event. Backend-only actions (services, open folder/editor/
// browser) run without ever showing the window.
func (a *App) watchCmdBarInbox() {
	dir := cmdBarInboxDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer fw.Close()
	if err := fw.Add(dir); err != nil {
		return
	}
	for {
		select {
		case <-a.ctx.Done():
			return
		case ev, ok := <-fw.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 && strings.HasSuffix(ev.Name, ".json") {
				if b, err := os.ReadFile(ev.Name); err == nil && len(b) > 0 {
					_ = os.Remove(ev.Name)
					a.handleCommandIntent(string(b))
				}
			}
		case _, ok := <-fw.Errors:
			if !ok {
				return
			}
		}
	}
}

// handleCommandIntent runs an intent forwarded from the command bar: it shows
// the main window for UI-bound actions, then emits the intent to the frontend
// executor (which performs the actual work, reusing the in-app code path).
func (a *App) handleCommandIntent(intentJSON string) {
	var in struct {
		Action    string `json:"action"`
		Command   string `json:"command"`
		Shell     string `json:"shell"`
		Project   string `json:"project"`
		Service   string `json:"service"`
		ServiceOp string `json:"serviceOp"`
		URL       string `json:"url"`
		Host      string `json:"host"`
		Target    string `json:"target"`
	}
	_ = json.Unmarshal([]byte(intentJSON), &in)

	// The main window only hides to the tray on close, so this process stays
	// alive but its webview is hidden. Anything that can run externally (or in
	// Go) executes directly so it works whether LaraEnv is open or in the tray;
	// the in-app paths bring the window forward and run through the frontend.
	emit := func() {
		a.bringToFront()
		time.Sleep(120 * time.Millisecond)
		wruntime.EventsEmit(a.ctx, "commandcenter:intent", intentJSON)
	}

	switch in.Action {
	// External actions: always run directly in Go (no webview needed).
	case "openFolder":
		if in.Project != "" {
			_ = a.OpenProjectFolder(in.Project)
		}
	case "openEditor":
		if in.Project != "" {
			_ = a.OpenProjectInEditor(in.Project, "")
		}
	case "openBrowser":
		if in.URL != "" {
			a.OpenURL(in.URL)
		}

	// Service ops: always in Go (the main process owns the service manager).
	case "service":
		a.runServiceOp(in.ServiceOp, in.Service)

	// Terminal/ssh: native external by default; in-app xterm.js when the user
	// chose it (Shift+Enter → target "xterm"), which brings the window forward.
	case "openTerminal":
		if in.Target == "xterm" {
			emit()
		} else {
			_ = a.OpenProjectTerminal(in.Project, in.Shell)
		}
	case "terminal":
		if in.Target == "xterm" {
			emit()
		} else if in.Command != "" {
			_ = a.OpenProjectTerminalCmd(in.Project, in.Shell, in.Command)
		}
	case "ssh":
		if in.Target == "xterm" {
			emit()
		} else if in.Host != "" {
			_ = a.OpenExternalSSH(in.Host)
		}

	// UI-bound: always bring the window forward and let the frontend handle it.
	case "navigate", "newProject":
		emit()

	default:
		emit()
	}
}

// runServiceOp executes a command-center "service" intent entirely in Go,
// mirroring the frontend runService helper.
func (a *App) runServiceOp(op, name string) {
	switch op {
	case "start":
		if name != "" {
			_ = a.StartService(name)
		}
	case "stop":
		if name != "" {
			_ = a.StopService(name)
		}
	case "reload":
		if name != "" {
			_ = a.ReloadService(name)
		}
	case "startAll":
		_ = a.StartManagedAll()
	case "stopAll":
		_ = a.StopManagedAll()
	case "reloadStack":
		_ = a.ReloadStack()
	}
}

// GetDetachedSeed returns the serialized one-group workspace this satellite was
// launched with, then removes the temp seed file. Empty when not detached.
func (a *App) GetDetachedSeed() string {
	if a.detachedSeed != "" {
		return a.detachedSeed
	}
	if a.detachedSeedFile == "" {
		return ""
	}
	b, err := os.ReadFile(a.detachedSeedFile)
	if err != nil {
		return ""
	}
	_ = os.Remove(a.detachedSeedFile)
	a.detachedSeed = string(b)
	return a.detachedSeed
}

// attachInboxDir is the directory a detached window drops re-attach seeds into
// and the main window watches. Shared via the filesystem because the two run as
// separate processes with no direct IPC.
func attachInboxDir() string { return filepath.Join(os.TempDir(), "laraenv-attach") }

// attachDebugLog appends a timestamped line to %TEMP%\laraenv-attach-debug.log.
// Diagnostic instrumentation for the re-attach flow: both the main and the
// detached process write here (tagged by pid/role) so a failed hand-off can be
// traced across the process boundary. Cheap (a few lines per attach) and safe
// to keep in release builds.
func attachDebugLog(role string, format string, args ...any) {
	p := filepath.Join(os.TempDir(), "laraenv-attach-debug.log")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s [pid %d %s] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), os.Getpid(), role, fmt.Sprintf(format, args...))
}

// watchAttachInbox runs in the main window only. It watches the inbox dir and
// emits a "terminal:attach" event (with the seed JSON) whenever a detached
// window drops a group back in. Files are written via temp+rename so we only
// ever read complete JSON.
func (a *App) watchAttachInbox() {
	dir := attachInboxDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		attachDebugLog("main", "watch ABORT mkdir %s: %v", dir, err)
		return
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		attachDebugLog("main", "watch ABORT NewWatcher: %v", err)
		return
	}
	defer fw.Close()
	if err := fw.Add(dir); err != nil {
		attachDebugLog("main", "watch ABORT Add %s: %v", dir, err)
		return
	}
	attachDebugLog("main", "watch START on %s", dir)
	for {
		select {
		case <-a.ctx.Done():
			attachDebugLog("main", "watch STOP ctx done")
			return
		case ev, ok := <-fw.Events:
			if !ok {
				attachDebugLog("main", "watch STOP events channel closed")
				return
			}
			matched := ev.Op&(fsnotify.Create|fsnotify.Write) != 0 && strings.HasSuffix(ev.Name, ".json")
			attachDebugLog("main", "event op=%s name=%s matched=%v", ev.Op, ev.Name, matched)
			if matched {
				b, err := os.ReadFile(ev.Name)
				if err != nil {
					attachDebugLog("main", "read FAILED %s: %v", ev.Name, err)
				} else if len(b) == 0 {
					attachDebugLog("main", "read EMPTY %s (left in place)", ev.Name)
				} else {
					if err := os.Remove(ev.Name); err != nil {
						attachDebugLog("main", "remove FAILED %s: %v", ev.Name, err)
					}
					attachDebugLog("main", "emit terminal:attach (%d bytes)", len(b))
					wruntime.EventsEmit(a.ctx, "terminal:attach", string(b))
				}
			}
		case werr, ok := <-fw.Errors:
			if !ok {
				attachDebugLog("main", "watch STOP errors channel closed")
				return
			}
			attachDebugLog("main", "watch ERROR: %v", werr)
		}
	}
}

// DrainTerminalAttach returns (and removes) any seeds left in the inbox — used
// by the main window on mount to pick up re-attaches that arrived while it was
// busy starting, or while it was closed.
func (a *App) DrainTerminalAttach() []string {
	dir := attachInboxDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		attachDebugLog("main", "drain: readdir %s: %v", dir, err)
		return nil
	}
	attachDebugLog("main", "drain: %d entries in %s", len(entries), dir)
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			out = append(out, string(b))
		}
		_ = os.Remove(p)
	}
	return out
}

// SendTerminalAttach is called by a detached window to hand a serialized group
// back to the main window. It writes the seed to the inbox (temp+rename for
// atomicity); the main window's watcher/drain picks it up and re-spawns it.
func (a *App) SendTerminalAttach(seedJSON string) error {
	dir := attachInboxDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		attachDebugLog("detached", "send ABORT mkdir %s: %v", dir, err)
		return err
	}
	tmp, err := os.CreateTemp(dir, "tmp-*.json.part")
	if err != nil {
		attachDebugLog("detached", "send ABORT createtemp in %s: %v", dir, err)
		return err
	}
	if _, err := tmp.WriteString(seedJSON); err != nil {
		tmp.Close()
		attachDebugLog("detached", "send ABORT write %s: %v", tmp.Name(), err)
		return err
	}
	tmp.Close()
	final := strings.TrimSuffix(tmp.Name(), ".part") // → <name>.json
	err = os.Rename(tmp.Name(), final)
	attachDebugLog("detached", "send wrote %s (%d bytes), rename -> %s, err=%v", tmp.Name(), len(seedJSON), final, err)
	return err
}

// DetachWorkspace launches an independent terminal window in a new process,
// seeded with the given serialized workspace JSON (one group). The new window
// re-spawns the terminals from their saved spawn descriptors.
func (a *App) DetachWorkspace(seedJSON string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "laraenv-detach-*.json")
	if err != nil {
		return err
	}
	if _, err := f.WriteString(seedJSON); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return exec.Command(exe, "--terminal-window", f.Name()).Start()
}

// loadPersistedCrons converts the raw map[string]any blobs from config.json
// back into typed cron.Job values. Errors per row are silently dropped so
// one corrupt entry can't prevent the engine from starting.
func loadPersistedCrons(cfg *config.Store) []pkgcron.Job {
	raws := cfg.Get().Crons
	out := make([]pkgcron.Job, 0, len(raws))
	for _, m := range raws {
		b, err := json.Marshal(m)
		if err != nil {
			continue
		}
		var j pkgcron.Job
		if err := json.Unmarshal(b, &j); err != nil {
			continue
		}
		// Reset transient run-state — anything "running" was interrupted
		// by the previous shutdown.
		if j.LastStatus == "running" {
			j.LastStatus = ""
		}
		out = append(out, j)
	}
	return out
}

// beforeClose intercepts the window close (X button / Alt-F4). It hides the
// window and keeps the app — and all running services — alive in the tray.
// Returning true tells Wails to cancel the close. The user must pick "Quit"
// from the tray menu to actually exit.
// onSecondInstance fires when the user launches LaraEnv again while an instance
// is already running (typically because the window was hidden to the tray).
// Instead of a duplicate process, surface the existing window.
func (a *App) onSecondInstance(_ options.SecondInstanceData) {
	a.bringToFront()
}

func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	// Detached terminal windows and the command bar close for real (no
	// hide-to-tray); only the main window minimizes to the tray.
	if a.detached || a.commandBar || a.quitting.Load() {
		return false
	}
	a.hideMainWindow(ctx)
	return true
}

// QuitApp is exposed to the frontend so the in-app menu can fully exit.
func (a *App) QuitApp() {
	if a.GetSourceControlBusy().Busy {
		_ = a.sourceControlBusyNotice()
		return
	}
	if a.detached || a.commandBar {
		_ = a.quitAfterDocuments()
		return
	}
	_, _ = a.documentShutdownGate().Request("quit", a.quitAfterDocuments)
}

// HideToTray lets the frontend programmatically minimise to tray.
func (a *App) HideToTray() { wruntime.WindowHide(a.ctx) }

// bringToFront restores and focuses the main window (used by the global hotkey
// when the window is hidden/minimised). Uses Win32 SetForegroundWindow — the
// AlwaysOnTop toggle corrupts WebView2 and paints the window black.
func (a *App) bringToFront() { bringWindowToFront(a.ctx) }

// refreshPhpFpmServices does two things:
//  1. Registers a php-fpm:<version> service definition for *every* installed
//     PHP version, so they're available to start on demand.
//  2. Sets nginx/apache Deps to ONLY the versions actually used by configured
//     projects — avoids spinning up FPMs nobody needs.
func (a *App) refreshPhpFpmServices() {
	a.refreshPhpFpmServicesWithPrune(true)
}

func (a *App) refreshPhpFpmServicesWithPrune(prune bool) {
	versions, err := a.php.List()
	if err != nil || len(versions) == 0 {
		a.services.SetDeps("nginx", nil)
		a.services.SetDeps("apache", nil)
		a.services.SetDeps("caddy", nil)
		return
	}
	tuning := a.cfg.Get().Services.Php
	workers := phpWorkerCount(tuning.Workers)
	for _, v := range versions {
		// Make sure every installed version has a usable php.ini with the
		// default extensions enabled (openssl, pdo_mysql, etc.).
		_ = php.EnsureIni(v.Path)
		// Then apply the upload/runtime limits so large uploads aren't rejected
		// or starved (empty/zero fields fall back to php.PhpTuningDefaults).
		_ = php.EnsurePhpTuning(v.Path, tuning.UploadMaxFilesize, tuning.PostMaxSize, tuning.MemoryLimit, tuning.MaxExecutionTime, tuning.MaxInputTime)
		version := v.Version // capture for the closure below
		bin := a.php.CgiBinary(version)
		a.services.RegisterDef(services.Definition{
			Name:  "php-fpm:" + version,
			Label: "PHP-FPM " + version,
			Path:  bin,
			// Pool of `workers` php-cgi processes, each bound to its own port
			// (php.ReplicaPort). php-cgi handles one request at a time and
			// doesn't fork on Windows, so without a pool a single large upload
			// blocks the whole site. nginx load-balances across them via the
			// php_<ver> upstream (see EnsurePhpUpstreams below).
			Replicas:    workers,
			ReplicaArgs: func(i int) []string { return []string{"-b", fmt.Sprintf("127.0.0.1:%d", php.ReplicaPort(version, i))} },
			Internal:    true,
			// PHP-CGI defaults to terminating after 500 requests on Windows
			// (PHP_FCGI_MAX_REQUESTS), which left users with random 502s and
			// a dead php-cgi.exe until they hit Start again. Disable the
			// self-recycle; the supervisor still auto-restarts on real crashes.
			Env: map[string]string{"PHP_FCGI_MAX_REQUESTS": "0"},
			// RedistURLForPHPSeries resolves to "" (the vcruntime default)
			// for every series except ones the catalog flags as needing a
			// different toolchain (e.g. 8.6's vs18 build).
			RedistURL: installer.RedistURLForPHPSeries(version),
		})
	}
	// Build a set of FPM names we actually registered above. Any "needed"
	// version that doesn't match an installed folder must NOT end up in
	// nginx.Deps — otherwise Start nginx fails with "service not registered"
	// for a phantom php-fpm. A stale "8.4.20" override gets remapped to "8.4"
	// (or dropped if no major.minor match exists).
	registered := map[string]bool{}
	for _, v := range versions {
		registered["php-fpm:"+v.Version] = true
	}
	needed := a.neededPhpVersions()
	neededSet := map[string]bool{}
	deps := make([]string, 0, len(needed))
	for _, v := range needed {
		name := "php-fpm:" + v
		if !registered[name] {
			// Try major.minor fallback: "8.4.20" → "8.4".
			mapped := ""
			for _, inst := range versions {
				if strings.HasPrefix(v, inst.Version+".") {
					mapped = "php-fpm:" + inst.Version
					break
				}
			}
			if mapped == "" {
				continue
			}
			name = mapped
		}
		if neededSet[name] {
			continue
		}
		neededSet[name] = true
		deps = append(deps, name)
	}
	a.services.SetDeps("nginx", deps)
	a.services.SetDeps("apache", deps)
	a.services.SetDeps("caddy", deps)

	// Maintain one nginx upstream{} pool per installed version so any project's
	// `fastcgi_pass php_<ver>;` resolves. Ports mirror the FPM ReplicaArgs above.
	if nginxConf := paths.NginxConf(); workers > 0 {
		if _, err := os.Stat(nginxConf); err == nil {
			ups := make([]vhost.PhpUpstream, 0, len(versions))
			for _, v := range versions {
				ports := make([]int, 0, workers)
				for i := 0; i < workers; i++ {
					ports = append(ports, php.ReplicaPort(v.Version, i))
				}
				ups = append(ups, vhost.PhpUpstream{Name: php.UpstreamName(v.Version), Ports: ports})
			}
			_ = vhost.EnsurePhpUpstreams(nginxConf, ups)
		}
	}

	// Stop any FPM currently running that no project actually needs.
	if !prune {
		return
	}
	// A broken/new requirement must not stop the pool serving the last valid config.
	if list, err := a.projects.List(); err == nil {
		for _, project := range list {
			for _, path := range a.projectServingPaths(project.Name) {
				resolution, _ := a.projectRuntimeSelection(project.Name, path)
				if resolution.Version == "" && resolution.Status != "unmanaged" {
					return
				}
			}
		}
	}
	for _, v := range versions {
		name := "php-fpm:" + v.Version
		if !neededSet[name] && a.services.Status(name) == "running" {
			_ = a.services.Stop(name)
		}
	}
}

// phpWorkerCount clamps the configured php-cgi pool size to a sane range.
// Zero/unset → the PhpTuningDefaults worker count; otherwise 1..16.
func phpWorkerCount(n int) int {
	if n <= 0 {
		_, _, _, _, _, def := php.PhpTuningDefaults()
		return def
	}
	if n > 16 {
		return 16
	}
	return n
}

// neededPhpVersions returns the deduped set of PHP versions used by the
// currently configured projects (after resolving overrides + defaults).
//
// When there are no projects yet (fresh install, just bootstrapped), the
// fallback is the currently-active PHP — so Start All on a clean machine
// still wires nginx/apache Deps to one FPM and the user gets a working
// stack on first click. Without this, nginx.Deps would be empty and
// nginx would happily start without any PHP backend, leaving the
// PHP-FPM card on the Dashboard stuck at STOPPED.
func (a *App) neededPhpVersions() []string {
	seen := map[string]bool{}
	out := []string{}

	projectCount := 0
	if list, err := a.projects.List(); err == nil {
		projectCount = len(list)
		inventory, prefs := installedRuntimeInventory(a.php, a.nodejs), a.cfg.Get().Projects
		for _, p := range list {
			for _, path := range a.projectServingPaths(p.Name) {
				resolution, _ := resolveRuntimeSelection(p.Name, path, prefs, inventory)
				v := resolution.Version
				if v == "" || seen[v] {
					continue
				}
				seen[v] = true
				out = append(out, v)
			}
		}
	}

	if projectCount == 0 && len(out) == 0 {
		if active, err := a.php.ActivePath(); err == nil && active != "" {
			v := filepath.Base(active)
			if v != "" && !seen[v] {
				out = append(out, v)
			}
		}
	}
	return out
}

// ensurePhpFpmRunning makes sure the php-fpm:<version> service exists in the
// registry AND is currently running. Used right before generating a vhost so
// the project never points at a port with no listener.
func (a *App) ensurePhpFpmRunning(version string) error {
	return a.ensurePhpFpmRunningContext(context.Background(), version)
}

func (a *App) ensurePhpFpmRunningContext(ctx context.Context, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version == "" {
		return nil
	}
	a.refreshPhpFpmServicesWithPrune(false) // keep the previous serving pool during apply.
	name := "php-fpm:" + version
	if a.services.Status(name) != "running" {
		return a.services.StartContext(ctx, name)
	}
	return nil
}

// PhpFpmInfo is a diagnostic snapshot of one PHP-FPM instance.
type PhpFpmInfo struct {
	Version string `json:"version"`
	Port    int    `json:"port"`
	Status  string `json:"status"` // "running" | "stopped"
	Binary  string `json:"binary"`
}

// ===== PHP extensions / ini =====

// ListPhpExtensions returns all known extensions for a PHP version, with
// enabled/available flags.
func (a *App) ListPhpExtensions(version string) ([]php.Extension, error) {
	return php.ListExtensions(a.php.VersionDir(version))
}

// FetchPhpExtensionDLL downloads the PHP zip for `version` from the catalog
// and extracts the single bundled DLL `<extName>` into the installation's
// ext/ folder. Resolves the catalog entry by major.minor prefix so a "7.4"
// install matches the catalog's "7.4.33" zip — bundled DLLs are stable
// across patch versions.
//
// Errors when the extension isn't bundled in the zip (PECL-only extensions
// like xdebug, redis, imagick), the catalog has no entry for `version`, or
// the download fails. The caller (UI) shows the message via flash().
func (a *App) FetchPhpExtensionDLL(version, extName string) error {
	var src *installer.Source
	for _, s := range installer.Catalog() {
		if s.Kind != installer.KindPHP {
			continue
		}
		if s.Version == version || strings.HasPrefix(s.Version, version+".") {
			cp := s
			src = &cp
			break
		}
	}
	if src == nil {
		return i18n.Errorf("errors.app.noCatalogEntryForPhp", map[string]any{"version": version})
	}
	versionDir := a.php.VersionDir(version)
	extDir := filepath.Join(versionDir, "ext")
	if err := a.inst.FetchBundledExtension(*src, extName, extDir); err != nil {
		return err
	}
	a.restartFpm(version)
	return nil
}

// SetPhpExtension enables or disables one extension for a PHP version, then
// restarts the FPM for that version so the change takes effect.
func (a *App) SetPhpExtension(version, name string, enabled bool) error {
	if err := php.SetExtension(a.php.VersionDir(version), name, enabled); err != nil {
		return err
	}
	a.restartFpm(version)
	return nil
}

// GetPhpIni returns the raw php.ini contents for a version.
func (a *App) GetPhpIni(version string) (string, error) {
	return php.ReadIni(a.php.VersionDir(version))
}

// SavePhpIni overwrites php.ini for a version and restarts its FPM.
func (a *App) SavePhpIni(version, content string) error {
	if err := php.WriteIni(a.php.VersionDir(version), content); err != nil {
		return err
	}
	a.restartFpm(version)
	return nil
}

// PeclExtensionInfo is one row of the "available PECL extensions" table
// shown in the extensions modal. Combines catalog metadata with the install
// state on disk for the requested PHP version.
type PeclExtensionInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Zend        bool   `json:"zend"`
	Version     string `json:"version"` // pinned build version for this PHP series
	Installed   bool   `json:"installed"`
	Enabled     bool   `json:"enabled"`
}

// ListPeclExtensions returns the curated PECL catalog filtered to entries
// that have a Windows build pinned for the requested PHP version, annotated
// with install/enable state.
func (a *App) ListPeclExtensions(version string) []PeclExtensionInfo {
	versionDir := a.php.VersionDir(version)
	extDir := filepath.Join(versionDir, "ext")
	series := installer.PeclSeries(version)

	// Snapshot enable state from php.ini so we don't re-parse for each entry.
	enabledNames := map[string]bool{}
	if list, err := php.ListExtensions(versionDir); err == nil {
		for _, e := range list {
			if e.Enabled {
				enabledNames[e.Name] = true
			}
		}
	}

	out := make([]PeclExtensionInfo, 0)
	for _, ext := range installer.PeclCatalog() {
		_, build, ok := installer.FindPeclBuild(ext.Name, series)
		if !ok {
			continue
		}
		_, err := os.Stat(filepath.Join(extDir, "php_"+ext.Name+".dll"))
		out = append(out, PeclExtensionInfo{
			Name:        ext.Name,
			DisplayName: ext.DisplayName,
			Description: ext.Description,
			Zend:        ext.Zend,
			Version:     build.Version,
			Installed:   err == nil,
			Enabled:     enabledNames[ext.Name],
		})
	}
	return out
}

// InstallPeclExtension downloads and installs one PECL extension for a PHP
// version, then enables it in php.ini and restarts FPM. Re-runs cleanly on
// already-installed extensions (overwrites the DLLs and re-asserts the ini
// directive).
func (a *App) InstallPeclExtension(version, name string) error {
	versionDir := a.php.VersionDir(version)
	if _, err := os.Stat(versionDir); err != nil {
		return i18n.Errorf("errors.app.phpNotInstalled", map[string]any{"version": version})
	}
	go func() {
		if err := a.inst.InstallPecl(name, version, versionDir); err != nil {
			fmt.Printf("[app] InstallPecl(%s, %s): %v\n", name, version, err)
			return
		}
		if err := php.SetExtension(versionDir, name, true); err != nil {
			fmt.Printf("[app] SetExtension(%s, %s, true): %v\n", version, name, err)
			return
		}
		a.restartFpm(version)
	}()
	return nil
}

func (a *App) restartFpm(version string) {
	name := "php-fpm:" + version
	if a.services.Status(name) == "running" {
		_ = a.services.Reload(name)
	}
}

// ListPhpFpmStatus reports the state of every registered PHP-FPM instance.
// Useful for debugging "why is my project not loading?" issues.
func (a *App) ListPhpFpmStatus() []PhpFpmInfo {
	versions, _ := a.php.List()
	out := make([]PhpFpmInfo, 0, len(versions))
	for _, v := range versions {
		out = append(out, PhpFpmInfo{
			Version: v.Version,
			Port:    php.PortFor(v.Version),
			Status:  a.services.Status("php-fpm:" + v.Version),
			Binary:  a.php.CgiBinary(v.Version),
		})
	}
	return out
}

// resolveProjectPhp resolves the main checkout without persisting detection.
func (a *App) resolveProjectPhp(name string) string {
	resolution, _ := a.projectRuntimeSelection(name, filepath.Join(a.projects.Root(), name))
	return resolution.Version
}

// resolveProjectNode resolves the main checkout, including declared requirements.
func (a *App) resolveProjectNode(name string) string {
	_, resolution := a.projectRuntimeSelection(name, filepath.Join(a.projects.Root(), name))
	return resolution.Version
}

// applyVhostFor reads the project on disk, runs detection and writes the
// nginx vhost + hosts entry. Idempotent.
func (a *App) applyVhostFor(name string) error {
	return a.applyVhost(name, true, true)
}

// applyVhost writes the vhost/hosts entries for one project. reloadWeb bounces
// the web server so the new config takes effect — pass false when sweeping
// many projects (restart once at the end instead; see startup). ensureFpm
// starts the project's FPM pool — pass false at startup, where services must
// stay stopped until the user starts the stack.
func (a *App) applyVhost(name string, reloadWeb, ensureFpm bool) error {
	return a.applyVhostContext(context.Background(), name, reloadWeb, ensureFpm)
}

func (a *App) applyVhostContext(ctx context.Context, name string, reloadWeb, ensureFpm bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.cfg.Get().Projects.Paused[name] {
		return a.removePausedVhosts(name, reloadWeb)
	}
	root := a.projects.Root()
	tld := a.projects.TLD()
	if tld == "" {
		tld = "test"
	}
	host := name + "." + tld
	path := filepath.Join(root, name)
	if a.routes != nil {
		routes, routeErr := a.routes.List(ctx, name)
		if routeErr != nil {
			return i18n.Wrapf(routeErr, "errors.app.projectRoutesFailed", nil)
		}
		if routesNeedManagedApply(routes, path) {
			project, projectErr := a.resolveRouteProject(ctx, name)
			if projectErr != nil {
				return projectErr
			}
			if ensureFpm {
				for _, route := range routes {
					resolution, _ := a.projectRuntimeSelection(name, route.TargetPath)
					if err := runtimeRequirementError(name, "PHP", resolution); err != nil {
						return err
					}
					if err := a.ensurePhpFpmRunningContext(ctx, resolution.Version); err != nil {
						return err
					}
				}
			}
			return a.applyProjectRouteSet(ctx, project, routes, routes)
		}
	}
	det := projects.Detect(path)
	phpRuntime, _ := a.projectRuntimeSelection(name, path)
	if err := runtimeRequirementError(name, "PHP", phpRuntime); err != nil {
		return err
	}
	version := phpRuntime.Version
	port := php.PortFor(version)
	// Make sure that project's FPM is up — otherwise nginx will fastcgi_pass
	// to a dead port and the user gets a 502 instead of their site.
	if ensureFpm {
		if err := a.ensurePhpFpmRunningContext(ctx, version); err != nil {
			return err
		}
	}

	proxyPort := a.cfg.Get().Projects.ProxyPorts[name]
	// If the dev server is running on a different port than the configured
	// ProxyPort (e.g. because the configured port was already in use and we
	// slid up to the next free one), the vhost must target the *actual*
	// listening port — otherwise the proxy routes to the wrong project.
	if a.devsrv != nil {
		if st := a.devsrv.Status(name); st.Running && st.Port > 0 {
			proxyPort = st.Port
		}
	}
	spec := vhost.VhostSpec{
		ServerName:  host,
		DocRoot:     det.DocRoot,
		Kind:        string(det.Kind),
		LogPrefix:   name,
		FastCGIPort: port,
		// nginx routes PHP to the worker-pool upstream; Apache (which ignores
		// this field) still uses FastCGIPort = worker 0's base port.
		FastCGIUpstream: php.UpstreamName(version),
		ProxyPort:       proxyPort,
	}
	if a.cfg.Get().Projects.SSL[name] {
		// Auto-trust only for an MSI install: an installed copy owns its
		// machine, but a portable copy's CA must not be planted into the
		// Windows trust store without the user asking — see TrustLocalCA.
		certFile, keyFile, err := vhost.EnsureSelfSignedCert(paths.NginxCerts(), host, !paths.IsPortable())
		if err != nil {
			return i18n.Wrapf(err, "errors.app.sslCertFailed", nil)
		}
		spec.SSL = true
		spec.CertFile = certFile
		spec.KeyFile = keyFile
		a.refreshLocalPHPTrust()
	}
	// Every installed web server gets its config written, whichever one is
	// running: switching the active server then needs no regeneration. Only
	// nginx is fatal — it is the default, and a failure there is the
	// "site unreachable" symptom the user will report.
	for _, w := range []struct {
		name     string
		root     string
		write    func(string, vhost.VhostSpec) error
		required bool
	}{
		{"nginx", paths.NginxRoot(), a.nginx.WriteVhostSpec, true},
		{"apache", paths.ApacheRoot(), a.apache.WriteVhostSpec, false},
		{"caddy", paths.CaddyRoot(), a.caddy.WriteVhostSpec, false},
	} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !w.required {
			if _, err := os.Stat(w.root); err != nil {
				continue
			}
		}
		if err := w.write(name, spec); err != nil {
			if w.required {
				return i18n.Wrapf(err, "errors.app.vhostFailed", map[string]any{"name": w.name})
			}
			fmt.Printf("[%s] vhost: %v\n", w.name, err)
		}
	}
	if err := a.hosts.AddEntry("127.0.0.1", host); err != nil {
		return i18n.Wrapf(err, "errors.app.hostsEntryFailed", nil)
	}
	if reloadWeb {
		return a.restartRunningWebServersContext(ctx)
	}
	return nil
}

func (a *App) removeVhostFor(name string) error {
	tld := a.projects.TLD()
	if tld == "" {
		tld = "test"
	}
	host := name + "." + tld
	if a.routes != nil {
		for _, route := range a.routes.Stored(name) {
			folder := routeFolder(name, route)
			_ = a.nginx.RemoveVhost(folder)
			_ = a.apache.RemoveVhost(folder)
			_ = a.caddy.RemoveVhost(folder)
			vhost.RemoveCert(paths.NginxCerts(), route.Hostname)
			if err := a.hosts.RemoveEntry(route.Hostname); err != nil {
				return err
			}
		}
		if err := a.routes.Forget(name); err != nil {
			return i18n.Wrapf(err, "errors.app.removeProjectRouteStateFailed", nil)
		}
	}
	_ = a.nginx.RemoveVhost(name)
	_ = a.apache.RemoveVhost(name)
	_ = a.caddy.RemoveVhost(name)
	vhost.RemoveCert(paths.NginxCerts(), host)
	if err := a.hosts.RemoveEntry(host); err != nil {
		return err
	}
	if a.services.Status("nginx") == "running" {
		_ = a.services.Restart("nginx")
	}
	if a.services.Status("apache") == "running" {
		_ = a.services.Restart("apache")
	}
	if a.services.Status("caddy") == "running" {
		_ = a.services.Restart("caddy")
	}
	return nil
}

// ===== Installer / Sources =====

type SourceWithStatus struct {
	installer.Source
	Installed bool `json:"installed"`
}

func (a *App) ListSources() []SourceWithStatus {
	cat := installer.Catalog()
	out := make([]SourceWithStatus, 0, len(cat))
	for _, s := range cat {
		out = append(out, SourceWithStatus{Source: s, Installed: a.inst.IsInstalled(s)})
	}
	return out
}

// recommendedKinds is the canonical "first run" stack for a Laravel dev
// environment: a PHP, a web server, a database, a mail catcher, and Node
// for frontend tooling. Order is the order the bootstrap modal renders.
var recommendedKinds = []installer.Kind{
	installer.KindPHP,
	installer.KindNginx,
	installer.KindMySQL,
	installer.KindMailpit,
	installer.KindNodejs,
}

// recommendedSources walks the catalog and returns the first *stable* entry
// per recommended kind (which by convention is the newest stable version).
// Result follows recommendedKinds order, not catalog order.
//
// Sources with Prerelease set (e.g. a PHP beta/RC) are skipped even when
// they sit above a stable entry in the catalog — catalog order is a display
// concern, not a promise about install defaults. A prerelease must never be
// what "install the recommended stack" hands a new user; making the default
// depend on catalog ordering means the next entry added to the top silently
// changes what new users install. If a kind has no stable entry at all, it
// is simply omitted from the result rather than falling back to a
// prerelease.
func recommendedSources() []installer.Source {
	cat := installer.Catalog()
	out := make([]installer.Source, 0, len(recommendedKinds))
	for _, k := range recommendedKinds {
		for _, s := range cat {
			if s.Kind == k && !s.Prerelease {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// recommendedServiceName maps an installer.Kind to the services.Manager
// service name that runs that binary. PHP is excluded — its FPM definitions
// are registered dynamically by refreshPhpFpmServices() based on installed
// versions, not via Stack.Enabled. Node is excluded — it has no service.
func recommendedServiceName(k installer.Kind) string {
	switch k {
	case installer.KindNginx:
		return "nginx"
	case installer.KindMySQL:
		return "mysql"
	case installer.KindMailpit:
		return "mailpit"
	}
	return ""
}

// RecommendedStack returns the canonical first-run stack annotated with
// per-source installed status. The bootstrap modal renders this list.
func (a *App) RecommendedStack() []SourceWithStatus {
	sources := recommendedSources()
	out := make([]SourceWithStatus, 0, len(sources))
	for _, s := range sources {
		out = append(out, SourceWithStatus{Source: s, Installed: a.inst.IsInstalled(s)})
	}
	return out
}

// IsStackBootstrapped reports whether every recommended kind is already
// installed. Cheap; called by the Dashboard before deciding whether
// "Start all" can run directly or needs to open the bootstrap modal.
func (a *App) IsStackBootstrapped() bool {
	for _, s := range recommendedSources() {
		if !a.inst.IsInstalled(s) {
			return false
		}
	}
	return true
}

// BootstrapRecommended kicks off parallel installs of any missing
// recommended-stack sources, then enables the resulting services in
// cfg.Stack.Enabled so Start All has something to iterate. Per-source
// progress flows through the existing "install:progress" event channel
// (one stream per kind); a final "bootstrap:done" event carries an
// {ok, errors} summary so the UI can dismiss its modal and proceed.
//
// Doesn't abort on a single install failure — successful installs are
// still enabled, so the user can recover by re-running the bootstrap
// or installing the failed kind manually from Downloads.
func (a *App) BootstrapRecommended() error {
	sources := recommendedSources()
	var wg sync.WaitGroup
	errs := make(map[string]string)
	var mu sync.Mutex

	for _, s := range sources {
		if a.inst.IsInstalled(s) {
			continue
		}
		wg.Add(1)
		go func(src installer.Source) {
			defer wg.Done()
			if err := a.inst.Install(src); err != nil {
				mu.Lock()
				errs[string(src.Kind)] = err.Error()
				mu.Unlock()
				return
			}
			switch src.Kind {
			case installer.KindPHP:
				a.afterPhpInstalled(src)
			case installer.KindCaddy:
				a.afterCaddyInstalled()
			case installer.KindNginx:
				a.afterNginxInstalled()
			case installer.KindApache:
				a.afterApacheInstalled()
			}
			a.notifyInstalled(src)
		}(s)
	}
	wg.Wait()

	for _, s := range sources {
		if name := recommendedServiceName(s.Kind); name != "" {
			_ = a.cfg.SetServiceEnabled(name, true)
		}
	}
	a.refreshPhpFpmServices()

	// Start the php-fpm worker for the just-installed PHP. Even though
	// nginx's Deps now include this service (so starting nginx pulls it
	// in), we kick it off explicitly so the PHP-FPM card on the
	// Dashboard lights up immediately and a user who hasn't started
	// nginx yet still has PHP available for `php artisan serve` etc.
	if active, err := a.php.ActivePath(); err == nil && active != "" {
		fpmName := "php-fpm:" + filepath.Base(active)
		if startErr := a.services.Start(fpmName); startErr != nil {
			fmt.Printf("[app] bootstrap start %s: %v\n", fpmName, startErr)
		}
	}

	wruntime.EventsEmit(a.ctx, "bootstrap:done", map[string]any{
		"ok":     len(errs) == 0,
		"errors": errs,
	})
	return nil
}

func (a *App) InstallSource(kind string, version string) error {
	for _, s := range installer.Catalog() {
		if string(s.Kind) == kind && s.Version == version {
			go func(src installer.Source) {
				if err := a.inst.Install(src); err == nil {
					switch src.Kind {
					case installer.KindPHP:
						a.afterPhpInstalled(src)
					case installer.KindCaddy:
						a.afterCaddyInstalled()
					case installer.KindNginx:
						a.afterNginxInstalled()
					case installer.KindApache:
						a.afterApacheInstalled()
					}
					a.notifyInstalled(src)
				}
			}(s)
			return nil
		}
	}
	return i18n.Errorf("errors.app.sourceNotFound", map[string]any{"kind": kind, "version": version})
}

func (a *App) OpenSourcePage(kind string, version string) error {
	for _, s := range installer.Catalog() {
		if string(s.Kind) == kind && s.Version == version {
			url := s.BrowserURL
			if url == "" {
				url = s.URL
			}
			wruntime.BrowserOpenURL(a.ctx, url)
			return nil
		}
	}
	return i18n.Errorf("errors.app.sourceNotFound", map[string]any{"kind": kind, "version": version})
}

func (a *App) InstallFromFile(kind string, version string) error {
	var src *installer.Source
	for _, s := range installer.Catalog() {
		if string(s.Kind) == kind && s.Version == version {
			s := s
			src = &s
			break
		}
	}
	if src == nil {
		return i18n.Errorf("errors.app.sourceNotFound", map[string]any{"kind": kind, "version": version})
	}

	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: i18n.T("backend.app.pickZipTitle", map[string]any{"kind": src.Kind, "version": src.Version}),
		Filters: []wruntime.FileFilter{
			{DisplayName: "Zip archives (*.zip)", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}

	go func(s installer.Source, picked string) {
		if err := a.inst.InstallFromFile(s, picked); err == nil {
			switch s.Kind {
			case installer.KindPHP:
				a.afterPhpInstalled(s)
			case installer.KindCaddy:
				a.afterCaddyInstalled()
			case installer.KindNginx:
				a.afterNginxInstalled()
			case installer.KindApache:
				a.afterApacheInstalled()
			}
			a.notifyInstalled(s)
		}
	}(*src, path)
	return nil
}

// phpEagerExtensions is the curated list of bundled extension DLLs that get
// extracted into ext/ right after a PHP install completes — so the user can
// flip them on/off in the UI without ever hitting "no dll". Superset of the
// defaults enabled in php.ini (defaultExts), trimmed to entries that ship in
// the windows.php.net zip and are commonly needed by modern PHP/Laravel apps.
var phpEagerExtensions = []string{
	"openssl", "curl", "mbstring", "fileinfo", "intl", "gd",
	"pdo_mysql", "pdo_pgsql", "pdo_sqlite", "sqlite3", "mysqli", "pgsql",
	"zip", "exif", "sockets", "soap", "bcmath", "gmp",
	"sodium", "xsl", "ldap", "ftp", "calendar", "shmop", "tidy",
}

// ensureCaddyConfigured creates the pieces the Caddy release does not ship.
// Unlike the nginx and Apache archives, which carry a working default config,
// the Caddy zip is a bare binary: without a Caddyfile at the path the service
// definition points at, `caddy run --config …` exits immediately. The logs
// directory has to exist too — Caddy refuses to start when it cannot open its
// log file, which is exactly how nginx failed in the portable build.
func (a *App) ensureCaddyConfigured() error {
	if err := os.MkdirAll(paths.CaddyLogs(), 0755); err != nil {
		return err
	}
	return vhost.EnsureCaddyfile(paths.CaddyFile(), paths.CaddySites())
}

// ensureNginxConfigured patches nginx.conf so its http {} block includes our
// per-project vhosts, applies the user's tuning, and neutralizes the
// bundled default server block. Best-effort and silent — a no-op if nginx
// isn't installed (the conf file doesn't exist yet).
//
// Shared by startup() and afterNginxInstalled so the two call sites can
// never drift apart. They used to: this patching only ran from startup,
// guarded on nginx.conf already existing, so installing nginx mid-session
// left the archive's pristine config untouched — `nginx -t` passed, but
// there was no `include sites/*.conf` anywhere in it, so every project's
// vhost silently never loaded and nginx served its own default page for
// every hostname until the app was closed and reopened.
func (a *App) ensureNginxConfigured() {
	nginxConf := paths.NginxConf()
	if _, err := os.Stat(nginxConf); err != nil {
		return
	}
	_ = vhost.EnsureSitesIncluded(nginxConf)
	_ = vhost.ApplyNginxTuning(nginxConf, a.cfg.Get().Services.Nginx)
	_ = vhost.EnsureDefaultServerName(nginxConf)
}

// ensureApacheConfigured is the Apache counterpart of ensureNginxConfigured:
// loads the modules our templates rely on, includes our per-project vhosts,
// and applies the user's tuning. Best-effort and silent — a no-op if Apache
// isn't installed. Shared by startup() and afterApacheInstalled for the same
// reason as ensureNginxConfigured.
func (a *App) ensureApacheConfigured() {
	apacheConf := paths.ApacheConf()
	if _, err := os.Stat(apacheConf); err != nil {
		return
	}
	_ = vhost.EnsureApacheSitesIncluded(apacheConf)
	_ = vhost.ApplyApacheTuning(apacheConf, a.cfg.Get().Services.Apache)
}

// notifyInstalled emits a generic "install:installed" event carrying the
// kind (and version) of whatever was just installed. php:installed stays as
// the dedicated PHP event — other code may already depend on it — but it is
// the only kind with one, so installing Node, Cmder, Git Bash, or anything
// else from Downloads/the bootstrap modal left every page that shows
// installed-runtime or installed-terminal state (Projects' Node dropdown and
// terminal-kind dropdown, the Dashboard Runtimes card, the terminal
// workspace's per-terminal picker) stale until a full reload. This event
// lets every one of those listen once and refresh regardless of kind.
// Called from every successful-install path: a single InstallSource, the
// BootstrapRecommended bulk install, and InstallFromFile.
func (a *App) notifyInstalled(src installer.Source) {
	if src.Kind == installer.KindPHP || src.Kind == installer.KindNodejs {
		a.refreshProjectRuntimes()
	}
	wruntime.EventsEmit(a.ctx, "install:installed", map[string]string{
		"kind":    string(src.Kind),
		"version": src.Version,
	})
}

// afterCaddyInstalled runs the post-install routine for a freshly extracted
// Caddy, the counterpart of afterPhpInstalled. Startup does this too, but
// only for an install that already existed when the app launched: without it
// here, Caddy installed during a session has no Caddyfile and no site files
// until the app is restarted, so it exits the moment the user starts it and
// serves nothing if it does come up. Idempotent — safe on reinstalls.
func (a *App) afterCaddyInstalled() {
	if err := a.ensureCaddyConfigured(); err != nil {
		fmt.Printf("[caddy] Caddyfile: %v\n", err)
		return
	}

	// Fill caddy\sites\ with the projects that already exist. applyVhost is
	// the one mechanism that writes a project's config for every installed
	// server, so it now picks Caddy up as well. Batch mode, exactly like the
	// startup sweep: no per-project web-server restart and no FPM auto-start.
	list, err := a.projects.List()
	if err != nil {
		wruntime.EventsEmit(a.ctx, "project:error", i18n.T("backend.app.vhostSweepFailed", map[string]any{"label": "caddy", "error": err}))
		return
	}
	for _, p := range list {
		if err := a.applyVhost(p.Name, false, false); err != nil {
			wruntime.EventsEmit(a.ctx, "project:error", i18n.T("backend.app.projectVhostError", map[string]any{"name": p.Name, "error": err}))
		}
	}
}

// afterNginxInstalled runs the post-install routine for a freshly extracted
// nginx, the counterpart of afterCaddyInstalled. Startup does this too, but
// only for an install that already existed when the app launched: without
// it here, nginx installed mid-session keeps the archive's pristine
// nginx.conf — no `include sites/*.conf` — so it passes `nginx -t` clean
// and starts fine, but serves its own default page for every project until
// the app is restarted. Idempotent — safe on reinstalls.
//
// Restarts nginx if it's already running: a reinstall/upgrade of an
// already-running nginx would otherwise keep serving the config it loaded
// at its last start, not the one just patched on disk.
func (a *App) afterNginxInstalled() {
	a.ensureNginxConfigured()

	// Fill nginx\conf\sites\ with the projects that already exist. applyVhost
	// is the one mechanism that writes a project's config for every installed
	// server, so it picks nginx up here too. Batch mode, exactly like the
	// startup sweep and afterCaddyInstalled: no per-project web-server
	// restart and no FPM auto-start.
	list, err := a.projects.List()
	if err != nil {
		wruntime.EventsEmit(a.ctx, "project:error", i18n.T("backend.app.vhostSweepFailed", map[string]any{"label": "nginx", "error": err}))
		return
	}
	for _, p := range list {
		if err := a.applyVhost(p.Name, false, false); err != nil {
			wruntime.EventsEmit(a.ctx, "project:error", i18n.T("backend.app.projectVhostError", map[string]any{"name": p.Name, "error": err}))
		}
	}
	if a.services.Status("nginx") == "running" {
		_ = a.services.Restart("nginx")
	}
}

// afterApacheInstalled is the Apache counterpart of afterNginxInstalled —
// same reasoning, same shape: patch httpd.conf, sweep existing projects into
// conf\sites\, and restart Apache if a session-old instance is already
// running so it picks up the freshly patched config.
func (a *App) afterApacheInstalled() {
	a.ensureApacheConfigured()

	list, err := a.projects.List()
	if err != nil {
		wruntime.EventsEmit(a.ctx, "project:error", i18n.T("backend.app.vhostSweepFailed", map[string]any{"label": "apache", "error": err}))
		return
	}
	for _, p := range list {
		if err := a.applyVhost(p.Name, false, false); err != nil {
			wruntime.EventsEmit(a.ctx, "project:error", i18n.T("backend.app.projectVhostError", map[string]any{"name": p.Name, "error": err}))
		}
	}
	if a.services.Status("apache") == "running" {
		_ = a.services.Restart("apache")
	}
}

// afterPhpInstalled runs the post-install routine for a freshly extracted
// PHP version: registers FPM, eagerly fills in any missing bundled DLLs,
// seeds php.ini with sane defaults, auto-activates if no version is currently
// active, and finally emits the "php:installed" event. The event is emitted
// last so listeners that refetch state see the post-activation truth.
// Idempotent — safe to invoke on reinstalls.
func (a *App) afterPhpInstalled(src installer.Source) {
	// The catalog Source.Version is the full patch ("8.4.20") but PHP installs
	// into a major.minor folder ("...\php\8.4"). Every on-disk operation —
	// VersionDir, SetActive, the event payload — must use the folder name,
	// or SetActive fails (no \8.4.20\ dir), VersionDir returns a bogus path,
	// and the frontend gets an event with a version List() doesn't have.
	folder := filepath.Base(src.InstallTo)

	a.refreshPhpFpmServices()

	versionDir := a.php.VersionDir(folder)
	extDir := filepath.Join(versionDir, "ext")
	if err := a.inst.EnsureBundledExtensions(src, phpEagerExtensions, extDir); err != nil {
		fmt.Printf("[app] EnsureBundledExtensions(%s): %v\n", src.Version, err)
	}
	if err := php.EnsureIni(versionDir); err != nil {
		fmt.Printf("[app] EnsureIni(%s): %v\n", src.Version, err)
	}

	// Drop Mozilla's CA bundle and wire it into php.ini so any HTTPS call
	// (cURL, OpenSSL streams, Composer fetching packages) trusts the right
	// roots out of the box. Without this every fresh PHP fails with
	// "cURL error 60: SSL certificate problem".
	if err := php.EnsureCaCert(versionDir, vhost.LocalCACertPath(paths.NginxCerts())); err != nil {
		fmt.Printf("[app] EnsureCaCert(%s): %v\n", src.Version, err)
	}

	// Auto-activate when nothing is active yet (first install, or stale
	// `current` symlink pointing at a removed folder). Never override an
	// existing active version — installing a second PHP keeps the user's
	// choice. Activation failure is logged, not fatal: the install itself
	// already succeeded.
	if active, err := a.php.ActivePath(); err != nil || active == "" {
		if err := a.php.SetActive(folder); err != nil {
			fmt.Printf("[app] auto-SetActive(%s): %v\n", folder, err)
		}
	} else if _, err := os.Stat(active); err != nil {
		// Stale symlink (target folder gone) — replace with the freshly
		// installed version.
		if err := a.php.SetActive(folder); err != nil {
			fmt.Printf("[app] auto-SetActive(%s) after stale symlink: %v\n", folder, err)
		}
	}

	// Re-register FPM defs *after* activation so the just-created `current`
	// symlink is visible to neededPhpVersions() — otherwise nginx.Deps stays
	// empty until the next refresh trigger and Start All races a stopped FPM.
	a.refreshPhpFpmServices()

	wruntime.EventsEmit(a.ctx, "php:installed", map[string]string{"version": folder})
}

// ===== Services =====

type ServiceInfo struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

func (a *App) StartService(name string) error   { return a.services.Start(name) }
func (a *App) StopService(name string) error    { return a.services.Stop(name) }
func (a *App) ServiceStatus(name string) string { return a.services.Status(name) }

// GetServiceLogs returns the captured stdout/stderr tail for a service.
func (a *App) GetServiceLogs(name string) []string { return a.services.Logs(name) }

// ListLoggedServices lists services that have produced output this session.
func (a *App) ListLoggedServices() []string { return a.services.ServicesWithLogs() }

// ReloadStack restarts running web-stack services: nginx, apache, and any
// php-fpm:{version}. Services that are stopped are skipped. Returns the first
// error encountered (if any) but always attempts every running target.
func (a *App) ReloadStack() error {
	var firstErr error
	for _, n := range a.services.RunningNames() {
		if n != "nginx" && n != "apache" && n != "caddy" && !strings.HasPrefix(n, "php-fpm:") {
			continue
		}
		if err := a.services.Reload(n); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// StartManagedAll brings up the user's enabled stack plus the php-fpm
// worker for the active PHP. We refresh the FPM registry first (so the
// def for the active version exists even if it was registered before
// the user installed the redist or recovered from a stale state), then
// start FPM explicitly, then iterate the rest. One service failing to
// start (e.g. two exclusive-group web servers both enabled from before
// that rule existed) must not stop the rest of the cascade — postgres
// and redis still need to come up even if nginx and apache conflict —
// so every failure is surfaced back to the UI through service:error and
// the loop keeps going; the first error is still returned to callers
// that check it.
func (a *App) StartManagedAll() error {
	a.refreshPhpFpmServices()

	if active, err := a.php.ActivePath(); err == nil && active != "" {
		fpm := "php-fpm:" + filepath.Base(active)
		if a.services.Status(fpm) != "running" {
			if startErr := a.services.Start(fpm); startErr != nil {
				wruntime.EventsEmit(a.ctx, "service:error", services.ErrorEvent{
					Service:   fpm,
					Message:   startErr.Error(),
					RedistURL: vcruntime.URLFromError(startErr),
				})
			}
		}
	}
	var firstErr error
	for _, d := range a.services.Active() {
		if a.services.Status(d.Name) == "running" {
			continue
		}
		if err := a.services.Start(d.Name); err != nil {
			wruntime.EventsEmit(a.ctx, "service:error", services.ErrorEvent{
				Service:   d.Name,
				Message:   err.Error(),
				RedistURL: vcruntime.URLFromError(err),
			})
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
	}
	return firstErr
}

// StopManagedAll mirrors StartManagedAll: stops the user's enabled stack
// AND every running php-fpm worker (regardless of which one is "active",
// in case the user has switched versions while services were live).
// services.Stop on the web server cascades to its Deps, but we don't
// trust Deps to be up-to-date in every reachable state.
func (a *App) StopManagedAll() error {
	for _, d := range a.services.Active() {
		if a.services.Status(d.Name) != "running" {
			continue
		}
		if err := a.services.Stop(d.Name); err != nil {
			return err
		}
	}
	for _, name := range a.services.RunningNames() {
		if !strings.HasPrefix(name, "php-fpm:") {
			continue
		}
		_ = a.services.Stop(name)
	}
	return nil
}

func (a *App) ListServices() []ServiceInfo {
	defs := a.services.Active()
	out := make([]ServiceInfo, len(defs))
	for i, d := range defs {
		out[i] = ServiceInfo{Name: d.Name, Label: d.Label}
	}
	return out
}

// ===== Per-service tuning =====

// GetServicesConfig returns the persisted per-service tuning block.
func (a *App) GetServicesConfig() config.Services {
	return a.cfg.Get().Services
}

// GetCommandCenter returns the persisted command-palette config.
func (a *App) GetCommandCenter() config.CommandCenter {
	return a.cfg.GetCommandCenter()
}

// SetCommandCenter persists the command-palette config (palette hotkey +
// custom commands) and re-applies the OS-wide global hotkey.
func (a *App) SetCommandCenter(cc config.CommandCenter) error {
	if err := a.cfg.SetCommandCenter(cc); err != nil {
		return err
	}
	if a.hotkeys != nil {
		_ = a.hotkeys.Set(cc.GlobalHotkey)
	}
	return nil
}

// ReloadService restarts a single service (stop + start with a short grace
// period). Used by command-palette "reload" actions.
func (a *App) ReloadService(name string) error {
	return a.services.Reload(name)
}

// SetNginxTuning persists nginx tuning, rewrites the live nginx.conf to
// reflect the new values, and returns. The caller (UI) is expected to ask
// the user to restart nginx for the change to take effect.
func (a *App) SetNginxTuning(t config.NginxTuning) error {
	if err := a.cfg.SetNginxTuning(t); err != nil {
		return err
	}
	nginxConf := paths.NginxConf()
	if _, err := os.Stat(nginxConf); err == nil {
		if err := vhost.ApplyNginxTuning(nginxConf, t); err != nil {
			return err
		}
	}
	if a.services.Status("nginx") == "running" {
		_ = a.services.Reload("nginx")
	}
	return nil
}

// SetApacheTuning persists apache tuning, rewrites httpd.conf, and reloads
// Apache when it's running.
func (a *App) SetApacheTuning(t config.ApacheTuning) error {
	if err := a.cfg.SetApacheTuning(t); err != nil {
		return err
	}
	apacheConf := paths.ApacheConf()
	if _, err := os.Stat(apacheConf); err == nil {
		if err := vhost.ApplyApacheTuning(apacheConf, t); err != nil {
			return err
		}
	}
	if a.services.Status("apache") == "running" {
		_ = a.services.Reload("apache")
	}
	return nil
}

// SetPhpTuning persists the php.ini upload/runtime limits and worker-pool size,
// re-applies the limits to every installed version's php.ini, rebuilds the
// php-cgi pool + nginx upstreams (refreshPhpFpmServices), and reloads the live
// php-fpm pools + nginx so the change takes effect without a manual restart.
func (a *App) SetPhpTuning(t config.PhpTuning) error {
	if err := a.cfg.SetPhpTuning(t); err != nil {
		return err
	}
	// Re-apply limits across all installed versions immediately.
	if versions, err := a.php.List(); err == nil {
		for _, v := range versions {
			_ = php.EnsureIni(v.Path)
			_ = php.EnsurePhpTuning(v.Path, t.UploadMaxFilesize, t.PostMaxSize, t.MemoryLimit, t.MaxExecutionTime, t.MaxInputTime)
		}
	}
	// Re-register FPM defs with the new worker count and rewrite nginx upstreams.
	a.refreshPhpFpmServices()
	// Restart the php-cgi pools that are currently up so the new worker count
	// and php.ini limits are picked up (php-cgi reads php.ini only at startup).
	for _, name := range a.services.RunningNames() {
		if strings.HasPrefix(name, "php-fpm:") {
			_ = a.services.Reload(name)
		}
	}
	if a.services.Status("nginx") == "running" {
		_ = a.services.Reload("nginx")
	}
	return nil
}

// ===== Dashboard service visibility =====

// GetEnabledServices returns the map of service-name → enabled used by
// the Dashboard to decide which service cards to show.
func (a *App) GetEnabledServices() map[string]bool {
	out := map[string]bool{}
	for k, v := range a.cfg.Get().Stack.Enabled {
		out[k] = v
	}
	return out
}

// SetServiceEnabled toggles a single service's visibility on the Dashboard.
// `name` matches the services.Manager registration (e.g. "nginx", "mysql").
func (a *App) SetServiceEnabled(name string, enabled bool) error {
	if err := a.cfg.SetServiceEnabled(name, enabled); err != nil {
		return err
	}
	wruntime.EventsEmit(a.ctx, "stack:changed")
	return nil
}

// ===== PHP =====

func (a *App) ListPhpVersions() ([]php.Version, error) { return a.php.List() }
func (a *App) SetActivePhp(version string) error {
	if err := a.php.SetActive(version); err != nil {
		return err
	}
	// New version installed/activated may add a new php-fpm service.
	a.refreshPhpFpmServicesWithPrune(false)
	a.refreshProjectRuntimes()
	return nil
}

// ===== Node.js =====

func (a *App) ListNodeVersions() ([]nodejs.Version, error) { return a.nodejs.List() }

func (a *App) SetActiveNode(version string) error {
	if err := a.nodejs.SetActive(version); err != nil {
		return err
	}
	a.refreshProjectRuntimes()
	return nil
}

func (a *App) ActiveNodeVersion() string {
	versions, err := a.nodejs.List()
	if err != nil {
		return ""
	}
	for _, v := range versions {
		if v.Active {
			return v.Version
		}
	}
	return ""
}

// vhostFileFor maps a project and a web server to the generated config file,
// delegating to that server's configurator so the path is derived in exactly
// one place — the same place that writes it during generation. An unknown
// server is an error rather than a default: silently editing the nginx
// config when the user asked for another one is worse than failing.
func (a *App) vhostFileFor(name, server string) (string, error) {
	switch server {
	case "nginx":
		return a.nginx.VhostPath(name), nil
	case "apache":
		return a.apache.VhostPath(name), nil
	case "caddy":
		return a.caddy.VhostPath(name), nil
	default:
		return "", i18n.Errorf("errors.app.unknownWebServer", map[string]any{"server": server})
	}
}

// GetProjectVhostPath returns the config file path for a project and web
// server, without reading it. Lets callers (e.g. the frontend) display the
// real path without re-deriving it themselves.
func (a *App) GetProjectVhostPath(name, server string) (string, error) {
	return a.vhostFileFor(name, server)
}

// GetProjectVhost returns the current vhost config text for a project and
// web server.
func (a *App) GetProjectVhost(name, server string) (string, error) {
	p, err := a.vhostFileFor(name, server)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SaveProjectVhost writes user-edited content directly to the config file and
// reloads that server if it is running. This bypasses the template — manual
// edits stay until you click Reset (or change project PHP/TLD, which
// regenerates).
func (a *App) SaveProjectVhost(name, server, content string) error {
	p, err := a.vhostFileFor(name, server)
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		return err
	}
	if a.services.Status(server) == "running" {
		_ = a.services.Reload(server)
	}
	return nil
}

// ResetProjectVhost regenerates the vhost from the template, discarding any
// manual edits.
func (a *App) ResetProjectVhost(name string) error {
	return a.applyVhostFor(name)
}

// SetProjectSSL toggles HTTPS for a project. When enabling, generates a
// self-signed cert (if missing) and rewrites the vhost with a 443 listener.
func (a *App) SetProjectSSL(name string, enabled bool) error {
	if err := a.applyProjectSSL(name, enabled); err != nil {
		return err
	}
	if enabled {
		// An MSI install's best-effort auto-trust can fail (not elevated,
		// group policy) even though it isn't portable, leaving it in the
		// same "cert issued, CA not trusted" state a portable copy is
		// always in. Only a genuine, checked "not trusted" — status.Exists
		// and a nil CheckError — reaches here; a failed check isn't
		// something the user can act on.
		//
		// A portable copy gets the in-place Trust prompt instead, right on
		// this click (Projects page, immediately after SetProjectSSL
		// returns) — see shouldPromptTrustLocalCA. Notifying here too would
		// be the same nudge twice for one click, so it's suppressed for
		// that case; NotifyLocalCAUntrusted lets the frontend raise it
		// explicitly if the user declines the prompt or TrustLocalCA fails.
		if status, statusErr := vhost.LocalCAStatus(paths.NginxCerts()); statusErr == nil && status.Exists && status.CheckError == "" && !status.Trusted {
			if !shouldPromptTrustLocalCA(paths.IsPortable(), status) {
				a.notifications.NotifyLocalCAUntrusted(name, status.Thumbprint)
			}
		}
	}
	wruntime.EventsEmit(a.ctx, "project:ssl-changed", name, enabled)
	return nil
}

// LocalCAStatus reports the local root CA's identity and whether this
// machine currently trusts it. Settings renders this so a portable copy's
// user can see — and act on — the fact that HTTPS certs it issues aren't
// auto-trusted the way an installed copy's are.
func (a *App) LocalCAStatus() (vhost.CAStatus, error) {
	return vhost.LocalCAStatus(paths.NginxCerts())
}

// TrustLocalCA installs the local root CA into Windows' Trusted Root store.
// Handles both the Settings "Trust" button and the in-place prompt the
// Projects page offers when enabling SSL on a portable copy (see
// shouldPromptTrustLocalCA) — either way it's an explicit user action, so
// unlike the best-effort install an MSI copy performs automatically, a
// failure here is returned rather than swallowed.
func (a *App) TrustLocalCA() error {
	if err := vhost.TrustLocalCA(paths.NginxCerts()); err != nil {
		return err
	}
	a.refreshLocalPHPTrust()
	return nil
}

// shouldPromptTrustLocalCA reports whether enabling SSL should offer, right
// there on the Projects page, to trust the local CA in place instead of
// sending the user hunting for the Settings card. Reachable only for a
// portable copy — an installed copy trusts its CA automatically (see
// EnsureSelfSignedCert's autoTrust parameter) and must never gain this
// friction — whose CA already exists on disk, was actually checked, and is
// genuinely not trusted. A CheckError means the check itself didn't run
// (e.g. certutil unresolvable), so this must not read that as "not
// trusted" and offer a Trust button that can't fix anything.
func shouldPromptTrustLocalCA(portable bool, status vhost.CAStatus) bool {
	return portable && status.Exists && status.CheckError == "" && !status.Trusted
}

// NotifyLocalCAUntrusted records the Settings-pointing nudge for a portable
// copy's in-place trust prompt (see shouldPromptTrustLocalCA) after the user
// declines it, or TrustLocalCA fails when they accept it. SetProjectSSL
// deliberately does not raise this notification itself for that case —
// asking twice for one click would be noise — so the Projects page calls
// this explicitly for the one outcome that still needs a trace the user can
// act on later. A best-effort no-op when no CA exists to report on.
func (a *App) NotifyLocalCAUntrusted(name string) error {
	status, err := vhost.LocalCAStatus(paths.NginxCerts())
	if err != nil {
		return err
	}
	if status.Exists {
		a.notifications.NotifyLocalCAUntrusted(name, status.Thumbprint)
	}
	return nil
}

// SetProjectPhp records a per-project PHP version override and rewrites the
// vhost so subsequent requests hit the right FPM port.
func (a *App) SetProjectPhp(name, version string) error {
	if err := a.cfg.SetProjectPhp(name, version); err != nil {
		return err
	}
	a.projectRuntimeChanged(name)
	wruntime.EventsEmit(a.ctx, "project:php-changed", name, version)
	return nil
}

// SetProjectNode records a per-project Node version override. No vhost
// rewrite needed — Node version only affects terminal/cron/dev-server PATH.
func (a *App) SetProjectNode(name, version string) error {
	if err := a.cfg.SetProjectNode(name, version); err != nil {
		return err
	}
	a.projectRuntimeChanged(name)
	wruntime.EventsEmit(a.ctx, "project:node-changed", name, version)
	return nil
}

// ===== Dev server (npm/pnpm/yarn/bun run dev) =====

// StartDevServer launches the project's `dev` (or `start`) script in the
// background. Returns the OS PID. Streams logs via "devserver:log" events
// and status changes via "devserver:status".
func (a *App) StartDevServer(name string) (int, error) {
	if a.cfg.Get().Projects.Paused[name] {
		return 0, i18n.Errorf("errors.app.projectPaused", nil)
	}
	if name == "" {
		return 0, i18n.Errorf("errors.app.nameRequired", nil)
	}
	projectPath := filepath.Join(a.projects.Root(), name)
	det := projects.Detect(projectPath)
	pm := det.PackageManager
	if pm == "" {
		pm = a.cfg.Get().Projects.DefaultPackageManager
	}
	if pm == "" {
		pm = "npm"
	}
	preferred := a.cfg.Get().Projects.ProxyPorts[name]
	customCmd := a.cfg.ProjectDevCommand(name)
	pid, err := a.devsrv.Start(name, projectPath, "", pm, preferred, customCmd)
	if err != nil {
		return pid, err
	}
	// If the actual listening port differs from the configured ProxyPort,
	// rewrite the vhost so the reverse proxy targets the real port. This
	// lets two projects with the same configured proxy port coexist —
	// the second one slides up to the next free port and its vhost follows.
	if status := a.devsrv.Status(name); status.Running && status.Port > 0 && status.Port != preferred {
		if err := a.applyVhostFor(name); err != nil {
			fmt.Printf("[app] vhost re-apply after port shift for %s: %v\n", name, err)
		}
	}
	return pid, nil
}

// StopDevServer kills the dev server process tree.
func (a *App) StopDevServer(name string) error {
	if err := a.devsrv.Stop(name); err != nil {
		return err
	}
	// Restore the vhost to the configured ProxyPort now that the dev server
	// is no longer overriding it with its actual listening port.
	if a.cfg.Get().Projects.ProxyPorts[name] > 0 {
		if err := a.applyVhostFor(name); err != nil {
			fmt.Printf("[app] vhost restore after dev stop for %s: %v\n", name, err)
		}
	}
	return nil
}

// GetDevServerStatus returns running/PID/port/script for the project.
func (a *App) GetDevServerStatus(name string) devserver.Status { return a.devsrv.Status(name) }

// GetDevServerLogs returns the latest ~500 lines of buffered output.
func (a *App) GetDevServerLogs(name string) []string { return a.devsrv.Logs(name) }

// ===== Ports panel =====

// PortEntry is one TCP port LaraEnv is responsible for, for the Ports panel.
type PortEntry struct {
	Owner     string `json:"owner"`   // project name or service label
	Kind      string `json:"kind"`    // "dev" | "service" | "orphan"
	Port      int    `json:"port"`    //
	PID       int    `json:"pid"`     //
	Process   string `json:"process"` // exe base name, e.g. "php.exe"
	Listening bool   `json:"listening"`
	Orphan    bool   `json:"orphan"` // listening on a reserved port we don't own
}

// ListPorts reports every listening TCP port that belongs to a LaraEnv process
// tree (dev servers + services), plus any "orphan" listener squatting on a
// reserved project proxy port that we don't currently own (e.g. a stray
// process left over from a previous run).
func (a *App) ListPorts() []PortEntry {
	type ownerInfo struct{ owner, kind string }
	roots := map[int]ownerInfo{}
	for _, r := range a.devsrv.ListRunning() {
		owner := r.Project
		if r.Instance != r.Project && r.Path != "" {
			owner += " · " + filepath.Base(r.Path)
		}
		roots[r.PID] = ownerInfo{owner: owner, kind: "dev"}
	}
	for _, tp := range a.services.TrackedPIDs() {
		roots[tp.PID] = ownerInfo{owner: tp.Label, kind: "service"}
	}
	rootPIDs := make([]int, 0, len(roots))
	for pid := range roots {
		rootPIDs = append(rootPIDs, pid)
	}
	owners := netports.TreeOwners(rootPIDs) // descendant PID -> root PID

	listeners, _ := netports.Listeners()
	out := []PortEntry{}
	seen := map[string]bool{}    // dedupe by port|pid
	ownedPorts := map[int]bool{} // ports already attributed to us
	for _, l := range listeners {
		root, ok := owners[int(l.PID)]
		if !ok {
			continue
		}
		key := fmt.Sprintf("%d|%d", l.Port, l.PID)
		if seen[key] {
			continue
		}
		seen[key] = true
		ownedPorts[l.Port] = true
		info := roots[root]
		out = append(out, PortEntry{
			Owner:     info.owner,
			Kind:      info.kind,
			Port:      l.Port,
			PID:       int(l.PID),
			Process:   netports.ProcessName(l.PID),
			Listening: true,
		})
	}

	// Orphans: a reserved proxy port that something is listening on but which we
	// don't own (left behind by a crash or an old instance).
	for name, port := range a.cfg.Get().Projects.ProxyPorts {
		if port <= 0 || ownedPorts[port] {
			continue
		}
		for _, l := range listeners {
			if l.Port != port {
				continue
			}
			out = append(out, PortEntry{
				Owner:     name,
				Kind:      "orphan",
				Port:      port,
				PID:       int(l.PID),
				Process:   netports.ProcessName(l.PID),
				Listening: true,
				Orphan:    true,
			})
			break
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// reclaimDevPortOrphans frees reserved project proxy ports still held by stray
// php/node processes from a previous run. Best-effort; emits a single notice
// listing what was reclaimed so the user knows why ports were freed.
func (a *App) reclaimDevPortOrphans() {
	var freed []string
	for name, port := range a.cfg.Get().Projects.ProxyPorts {
		if port <= 0 {
			continue
		}
		// Don't touch a port we currently own (a dev server we just started).
		if st := a.devsrv.Status(name); st.Running {
			continue
		}
		if len(netports.PIDsOnPort(port)) == 0 {
			continue
		}
		if killed, _ := netports.KillPort(port, devserver.StackExes...); len(killed) > 0 {
			freed = append(freed, fmt.Sprintf("%s (:%d)", name, port))
		}
	}
	if len(freed) == 0 || a.ctx == nil {
		return
	}
	// Delay the notice so the frontend toast listener is mounted.
	time.Sleep(1500 * time.Millisecond)
	wruntime.EventsEmit(a.ctx, "app:notice",
		i18n.T("backend.app.reclaimedPorts", map[string]any{"ports": strings.Join(freed, ", ")}))
}

// KillPort terminates whatever process tree is listening on the given port.
// Invoked explicitly by the user from the Ports panel, so it is unrestricted.
func (a *App) KillPort(port int) error {
	_, err := netports.KillPort(port)
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "devserver:status", "")
		wruntime.EventsEmit(a.ctx, "service:status", "")
	}
	return err
}

// ===== Queue worker (php artisan queue:work) =====

// StartQueueWorker launches `php artisan queue:work` in the background using
// the project's resolved PHP version. Returns the OS PID.
func (a *App) StartQueueWorker(name string) (int, error) {
	return a.StartQueueWorkerWithArgs(name, "")
}

// StartQueueWorkerWithArgs is StartQueueWorker plus a free-form string of
// extra command-line flags (e.g. `--queue=high --tries=3 --timeout=60`).
// The args are persisted per-project so the modal can pre-fill on next open.
func (a *App) StartQueueWorkerWithArgs(name, extraArgs string) (int, error) {
	if a.cfg.Get().Projects.Paused[name] {
		return 0, i18n.Errorf("errors.app.projectPaused", nil)
	}
	if name == "" {
		return 0, i18n.Errorf("errors.app.nameRequired", nil)
	}
	phpRuntime, _ := a.projectRuntimeSelection(name, filepath.Join(a.projects.Root(), name))
	if err := runtimeRequirementError(name, "PHP", phpRuntime); err != nil {
		return 0, err
	}
	args, err := queueworker.SplitArgs(extraArgs)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(extraArgs) != "" {
		if err := a.cfg.SetProjectQueueArgs(name, extraArgs); err != nil {
			fmt.Printf("[app] persist queue args for %s: %v\n", name, err)
		}
	}
	return a.queue.Start(name, filepath.Join(a.projects.Root(), name), args)
}

// StopQueueWorker kills the queue worker process tree for the project.
func (a *App) StopQueueWorker(name string) error { return a.queue.Stop(name) }

// RestartQueueWorker stops and re-spawns the worker with the last persisted
// args (or none, if nothing was saved).
func (a *App) RestartQueueWorker(name string) (int, error) {
	if a.cfg.Get().Projects.Paused[name] {
		return 0, i18n.Errorf("errors.app.projectPaused", nil)
	}
	if name == "" {
		return 0, i18n.Errorf("errors.app.nameRequired", nil)
	}
	phpRuntime, _ := a.projectRuntimeSelection(name, filepath.Join(a.projects.Root(), name))
	if err := runtimeRequirementError(name, "PHP", phpRuntime); err != nil {
		return 0, err
	}
	last := a.cfg.Get().Projects.QueueArgs[name]
	args, _ := queueworker.SplitArgs(last)
	return a.queue.Restart(name, filepath.Join(a.projects.Root(), name), args)
}

// GetQueueWorkerStatus returns running/PID/args for the project.
func (a *App) GetQueueWorkerStatus(name string) queueworker.Status {
	return a.queue.Status(name)
}

// GetQueueWorkerLogs returns the latest ~500 lines of buffered output.
func (a *App) GetQueueWorkerLogs(name string) []string { return a.queue.Logs(name) }

// GetLastQueueArgs returns the last extra-args string saved for the project,
// or "" if none.
func (a *App) GetLastQueueArgs(name string) string {
	return a.cfg.Get().Projects.QueueArgs[name]
}

// GetProjectDevCommand returns the project's own custom dev command (raw,
// without falling back to the global default), or "" if none — used to
// pre-fill the configure-command modal.
func (a *App) GetProjectDevCommand(name string) string {
	return a.cfg.Get().Projects.DevCommands[name]
}

// SetProjectDevCommand saves (or clears, when empty) the custom dev command
// for a project. Takes effect on the next dev-server start.
func (a *App) SetProjectDevCommand(name, cmd string) error {
	return a.cfg.SetProjectDevCommand(name, cmd)
}

// MakeDevCommandWindowsSafe rewrites the project's composer.json `dev` script to
// drop `php artisan pail` (which needs the Windows-unavailable pcntl extension),
// so `composer run dev` runs on Windows. Explicit, user-triggered, idempotent.
// Returns a human-readable summary of what changed.
func (a *App) MakeDevCommandWindowsSafe(name string) (string, error) {
	if name == "" {
		return "", i18n.Errorf("errors.app.nameRequired", nil)
	}
	projectPath := filepath.Join(a.projects.Root(), name)
	_, summary, err := projects.StripPailFromDevScript(projectPath)
	return summary, err
}

// GetResolvedPhpExe returns the absolute path to the php.exe that would be
// used for the given project (per-project override → active → first installed).
func (a *App) GetResolvedPhpExe(name string) string {
	version := a.resolveProjectPhp(name)
	if version == "" {
		return ""
	}
	return a.php.ExeFor(version)
}

// ProjectCommands is the autocomplete vocabulary for a project's command
// launcher: every `php artisan` subcommand (including the project's custom
// ones) and every npm script declared in package.json.
type ProjectCommands struct {
	Artisan    []string `json:"artisan"`
	NpmScripts []string `json:"npmScripts"`
}

// ListProjectCommands inspects a project on disk and returns the artisan
// commands (via `php artisan list --format=json`) and npm scripts (from
// package.json) available to it. Used by the command palette's launcher to
// offer dynamic, project-accurate autocompletion. Missing tooling (no artisan
// file, no package.json) simply yields an empty slice for that category.
func (a *App) ListProjectCommands(name string) ProjectCommands {
	out := ProjectCommands{Artisan: []string{}, NpmScripts: []string{}}
	if name == "" {
		return out
	}
	projectPath := filepath.Join(a.projects.Root(), name)
	if _, err := os.Stat(projectPath); err != nil {
		return out
	}

	// --- artisan commands -------------------------------------------------
	if _, err := os.Stat(filepath.Join(projectPath, "artisan")); err == nil {
		phpExe := a.GetResolvedPhpExe(name)
		if phpExe != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			cmd := exec.CommandContext(ctx, phpExe, "artisan", "list", "--format=json")
			cmd.Dir = projectPath
			// Hide the console window php would otherwise flash on Windows.
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			if data, err := cmd.Output(); err == nil {
				var parsed struct {
					Commands []struct {
						Name string `json:"name"`
					} `json:"commands"`
				}
				if json.Unmarshal(data, &parsed) == nil {
					for _, c := range parsed.Commands {
						if c.Name != "" {
							out.Artisan = append(out.Artisan, c.Name)
						}
					}
				}
			}
			cancel()
		}
	}

	// --- npm scripts ------------------------------------------------------
	if scripts, err := devserver.ListScripts(projectPath); err == nil {
		out.NpmScripts = scripts
	}

	return out
}

// SetProjectTags replaces the tag list for a single project. Tags are
// free-form labels surfaced in the Projects page filter chips.
func (a *App) SetProjectTags(name string, tags []string) error {
	if err := a.cfg.SetProjectTags(name, tags); err != nil {
		return err
	}
	wruntime.EventsEmit(a.ctx, "project:tags-changed", name)
	return nil
}

// SetProjectProxyPort sets (port>0) or clears (port<=0) the reverse-proxy
// target for a project. Regenerates the vhost so nginx serves the new mode.
func (a *App) SetProjectProxyPort(name string, port int) error {
	if err := a.cfg.SetProjectProxyPort(name, port); err != nil {
		return err
	}
	if err := a.applyVhostFor(name); err != nil {
		return err
	}
	wruntime.EventsEmit(a.ctx, "project:proxy-changed", name, port)
	return nil
}

// ===== Projects =====

func (a *App) ListProjects() ([]projects.Project, error) {
	list, err := a.projects.List()
	if err != nil {
		return nil, err
	}
	inventory := installedRuntimeInventory(a.php, a.nodejs)
	prefs := a.cfg.Get().Projects
	for i := range list {
		list[i].PhpRuntime, list[i].NodeRuntime = resolveRuntimeSelection(list[i].Name, list[i].Path, prefs, inventory)
	}
	return list, nil
}

func (a *App) GetProjectsConfig() config.Projects { return a.cfg.Get().Projects }

// SetProjectsTLD updates the TLD and regenerates every existing vhost + hosts
// entry to use the new suffix. Old entries are removed first.
func (a *App) SetProjectsTLD(tld string) error {
	tld = sanitizeTLD(tld)
	if tld == "" {
		return i18n.Errorf("errors.app.invalidTld", nil)
	}
	old := a.cfg.Get().Projects
	managed := map[string]bool{}
	// Remove vhosts/hosts entries with the previous TLD.
	if list, _ := a.projects.List(); list != nil {
		oldTLD := old.TLD
		if oldTLD == "" {
			oldTLD = "test"
		}
		for _, p := range list {
			if a.routes != nil && len(a.routes.Stored(p.Name)) > 0 {
				managed[p.Name] = true
				continue
			}
			_ = a.nginx.RemoveVhost(p.Name)
			_ = a.apache.RemoveVhost(p.Name)
			_ = a.hosts.RemoveEntry(p.Name + "." + oldTLD)
		}
	}
	old.TLD = tld
	if err := a.cfg.SetProjects(old); err != nil {
		return err
	}
	// Regenerate with the new TLD.
	if list, _ := a.projects.List(); list != nil {
		for _, p := range list {
			if managed[p.Name] {
				if _, err := a.routes.MigrateTLD(context.Background(), p.Name, old.TLD); err != nil {
					return i18n.Wrapf(err, "errors.app.migrateRoutesFailed", map[string]any{"name": p.Name})
				}
			} else if err := a.applyVhostFor(p.Name); err != nil {
				return err
			}
		}
	}
	wruntime.EventsEmit(a.ctx, "project:tld-changed", tld)
	return nil
}

func sanitizeTLD(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '.' {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// ProjectCreateError is the "project:create-error" event payload. RedistURL
// carries the resolved Visual C++ redistributable URL (via
// vcruntime.URLFromError) when the failure was a vcruntime.Check miss —
// "" otherwise — so the frontend can show the exact redistributable this
// PHP needs instead of parsing it back out of Message.
type ProjectCreateError struct {
	Message   string `json:"message"`
	RedistURL string `json:"redistUrl,omitempty"`
}

// CreateProject runs the requested template (blank/laravel/wordpress/git).
// Output lines stream as "project:create-progress" events; final
// "project:create-done" or "project:create-error" event signals completion.
//
// After scaffolding, re-applies the vhost (so detection sees the now-populated
// folder) and runs Laravel-specific post-setup (DB creation, .env patch, npm).
func (a *App) CreateProject(opts projects.CreateOptions) error {
	go func() {
		emit := func(line string) {
			wruntime.EventsEmit(a.ctx, "project:create-progress", line)
		}
		if err := a.creator.Create(a.ctx, opts, emit); err != nil {
			wruntime.EventsEmit(a.ctx, "project:create-error", ProjectCreateError{
				Message:   err.Error(),
				RedistURL: vcruntime.URLFromError(err),
			})
			return
		}

		// Pin the project to the PHP it was scaffolded with. Laravel 12/13
		// require PHP 8.2/8.3+, and composer installs the deps via
		// --ignore-platform-reqs regardless of the active PHP. Without this
		// pin the vhost/FPM pool (and migrate below) would use the active
		// PHP — if that's older, every request and `artisan` call fatals on
		// composer's platform_check.php.
		if opts.Kind == "laravel" && opts.PhpVersion != "" {
			_ = a.cfg.SetProjectPhp(opts.Name, opts.PhpVersion)
		}

		// Re-apply vhost: the watcher fired a vhost while the folder was
		// still empty (kind=plain). Now that scaffolding is done, detection
		// will return the right Kind/DocRoot.
		emit("regenerating vhost with detected project type…")
		a.projectRuntimeChanged(opts.Name)

		// Laravel-specific post-setup: DB + .env + npm build.
		if opts.Kind == "laravel" {
			enabled := a.cfg.Get().Stack.Enabled
			db := ""
			switch {
			case enabled["mysql"]:
				db = "mysql"
			case enabled["postgres"]:
				db = "postgres"
			}
			phpRuntime, nodeRuntime := a.projectRuntimeSelection(opts.Name, filepath.Join(a.projects.Root(), opts.Name))
			nodeDir := ""
			if nodeRuntime.Version != "" {
				nodeDir = a.nodejs.VersionDir(nodeRuntime.Version)
			}
			phpExe := ""
			if phpRuntime.Version != "" {
				phpExe = a.php.ExeFor(phpRuntime.Version)
			}
			projects.PostSetupLaravel(a.ctx, projects.PostSetupConfig{
				ProjectsRoot: a.projects.Root(),
				Database:     db,
				MySQLBin:     paths.MySQLBin(),
				PostgresBin:  paths.PostgresBin(),
				NodeDir:      nodeDir,
				SkipFrontend: runtimeRequirementError(opts.Name, "Node", nodeRuntime) != nil,
				PHPExe:       phpExe,
			}, opts.Name, emit)
		}

		wruntime.EventsEmit(a.ctx, "project:create-done", opts.Name)
	}()
	return nil
}

// RemoveProject removes only the vhost + hosts entry for a project.
// The folder on disk is preserved.
func (a *App) RemoveProject(name string) error {
	return a.removeVhostFor(name)
}

// DeleteProject removes vhost + hosts entry AND deletes the project folder
// from disk. Also cleans up any per-project PHP override. When dropDatabase
// is true, the project's database (read from its .env, falling back to the
// active stack + name-derivation used at creation) is dropped first —
// best-effort, so a drop failure does not block folder deletion.
func (a *App) DeleteProject(name string, dropDatabase bool) error {
	if name == "" {
		return i18n.Errorf("errors.app.nameRequired", nil)
	}
	if err := a.removeVhostFor(name); err != nil {
		return err
	}
	target := filepath.Join(a.projects.Root(), name)

	// Drop the database while the folder (and its .env) still exists.
	if dropDatabase {
		enabled := a.cfg.Get().Stack.Enabled
		stackEngine := ""
		switch {
		case enabled["mysql"]:
			stackEngine = "mysql"
		case enabled["postgres"]:
			stackEngine = "postgres"
		}
		dbName, err := projects.DropProjectDatabase(a.ctx, projects.PostSetupConfig{
			MySQLBin:    paths.MySQLBin(),
			PostgresBin: paths.PostgresBin(),
		}, target, name, stackEngine, nil)
		if err != nil {
			fmt.Printf("[app] DeleteProject: drop database %q failed: %v\n", dbName, err)
		}
	}

	// Drop the per-project PHP override, if any.
	_ = a.cfg.SetProjectPhp(name, "")
	_ = a.cfg.SetProjectSSL(name, false)
	_ = a.cfg.SetProjectTags(name, nil)
	if err := os.RemoveAll(target); err != nil {
		return i18n.Wrapf(err, "errors.app.deleteFolderFailed", map[string]any{"target": target})
	}
	return nil
}

func (a *App) OpenProjectFolder(name string) error {
	path := filepath.Join(a.projects.Root(), name)
	return exec.Command("explorer.exe", path).Start()
}

// ===== Editor picker =====

type EditorsInfo struct {
	Profiles          []editor.Profile  `json:"profiles"`
	DefaultKind       string            `json:"defaultKind"`
	DefaultCustomCmd  string            `json:"defaultCustomCmd"`
	ProjectKinds      map[string]string `json:"projectKinds"`
	ProjectCustomCmds map[string]string `json:"projectCustomCmds"`
}

// ListEditors returns detected editor profiles plus persisted per-project and
// global default selections, so the UI can render a dropdown.
func (a *App) ListEditors() EditorsInfo {
	profs := editor.Detect()
	cfg := a.cfg.Get().Projects
	out := EditorsInfo{
		Profiles:          profs,
		DefaultKind:       cfg.DefaultEditor,
		DefaultCustomCmd:  cfg.DefaultCustomEditor,
		ProjectKinds:      map[string]string{},
		ProjectCustomCmds: map[string]string{},
	}
	for k, v := range cfg.Editors {
		out.ProjectKinds[k] = v
	}
	for k, v := range cfg.CustomEditors {
		out.ProjectCustomCmds[k] = v
	}
	if out.DefaultKind == "" {
		for _, p := range profs {
			if p.Available && p.Kind != editor.KindCustom {
				out.DefaultKind = string(p.Kind)
				break
			}
		}
	}
	return out
}

func (a *App) SetProjectEditor(name, kind string) error {
	return a.cfg.SetProjectEditor(name, kind)
}

func (a *App) SetDefaultEditor(kind string) error {
	return a.cfg.SetDefaultEditor(kind)
}

func (a *App) SetProjectCustomEditor(name, cmd string) error {
	return a.cfg.SetProjectCustomEditor(name, cmd)
}

func (a *App) SetDefaultCustomEditor(cmd string) error {
	return a.cfg.SetDefaultCustomEditor(cmd)
}

// OpenProjectInEditor opens the project folder in the resolved editor.
// Resolves: explicit kind → per-project saved → global default → first available.
func (a *App) OpenProjectInEditor(name, kind string) error {
	if name == "" {
		return i18n.Errorf("errors.app.nameRequired", nil)
	}
	projectPath := filepath.Join(a.projects.Root(), name)
	if _, err := os.Stat(projectPath); err != nil {
		return i18n.Wrapf(err, "errors.app.projectFolderNotFound", nil)
	}

	picked, customCmd, err := a.resolveEditor(name, kind)
	if err != nil {
		return err
	}
	return editor.Open(picked, projectPath, customCmd)
}

func (a *App) resolveEditor(name, kind string) (editor.Profile, string, error) {
	profs := editor.Detect()
	cfg := a.cfg.Get().Projects

	resolved := kind
	if resolved == "" {
		resolved = cfg.Editors[name]
	}
	if resolved == "" {
		resolved = cfg.DefaultEditor
	}
	if resolved == "" {
		for _, p := range profs {
			if p.Available && p.Kind != editor.KindCustom {
				resolved = string(p.Kind)
				break
			}
		}
	}
	if resolved == "" {
		return editor.Profile{}, "", i18n.Errorf("errors.app.noEditorAvailable", nil)
	}

	// Persist explicit pick.
	if kind != "" {
		_ = a.cfg.SetProjectEditor(name, kind)
	}

	var picked editor.Profile
	for _, p := range profs {
		if string(p.Kind) == resolved {
			picked = p
			break
		}
	}

	customCmd := cfg.CustomEditors[name]
	if customCmd == "" {
		customCmd = cfg.DefaultCustomEditor
	}

	return picked, customCmd, nil
}

// ListTerminals returns the available terminal profiles plus the resolved
// per-project / global default selections, so the UI can render a dropdown.
type TerminalsInfo struct {
	Profiles     []terminal.Profile `json:"profiles"`
	DefaultKind  string             `json:"defaultKind"`
	ProjectKinds map[string]string  `json:"projectKinds"`
}

func (a *App) ListTerminals() TerminalsInfo {
	profs := terminal.Detect(paths.CmderRoot())
	cfg := a.cfg.Get().Projects
	out := TerminalsInfo{
		Profiles:     profs,
		DefaultKind:  cfg.DefaultTerminal,
		ProjectKinds: map[string]string{},
	}
	for k, v := range cfg.Terminals {
		out.ProjectKinds[k] = v
	}
	if out.DefaultKind == "" {
		// First available wins as the implicit default.
		for _, p := range profs {
			if p.Available {
				out.DefaultKind = string(p.Kind)
				break
			}
		}
	}
	return out
}

// SetProjectTerminal records the user's preferred terminal for a project.
// Pass kind="" to clear the override and fall back to the global default.
func (a *App) SetProjectTerminal(name, kind string) error {
	return a.cfg.SetProjectTerminal(name, kind)
}

// SetDefaultTerminal sets the global fallback terminal kind.
func (a *App) SetDefaultTerminal(kind string) error {
	return a.cfg.SetDefaultTerminal(kind)
}

// OpenProjectTerminal launches an external terminal at the project folder,
// with PATH prefixed by the project's resolved PHP version (and the active
// Node, if any). When kind is empty, falls back to the per-project saved
// choice, then to the global default, then to the first available terminal.
func (a *App) OpenProjectTerminal(name, kind string) error {
	prof, prefix, dir, banner, err := a.resolveProjectTerminal(name, kind)
	if err != nil {
		return err
	}
	return terminal.Open(prof, dir, name, banner, prefix, a.localTerminalTLSEnvironment)
}

// OpenProjectTerminalCmd opens an external terminal at the project folder (same
// profile/PATH resolution as OpenProjectTerminal) and runs an initial command,
// keeping the shell open. Used by the command-bar routing when LaraEnv is in the
// tray and the user runs "<project> <command>".
func (a *App) OpenProjectTerminalCmd(name, kind, command string) error {
	prof, prefix, dir, banner, err := a.resolveProjectTerminal(name, kind)
	if err != nil {
		return err
	}
	return terminal.OpenWithCommand(prof, dir, name, banner, command, prefix, a.localTerminalTLSEnvironment)
}

// OpenExternalSSH opens an external terminal running the ssh CLI for the saved
// host (looked up by Name). Reuses a.ssh.BuildCommandLine for the args (port,
// -i key path, ProxyJump/-J). Password-auth hosts simply let ssh prompt in the
// window. Used by the command-bar routing when LaraEnv is in the tray.
func (a *App) OpenExternalSSH(hostName string) error {
	if hostName == "" {
		return i18n.Errorf("errors.app.hostRequired", nil)
	}
	var found *config.SSHHost
	for _, h := range a.cfg.ListSSHHosts() {
		if h.Name == hostName {
			hh := h
			found = &hh
			break
		}
	}
	if found == nil {
		return i18n.Errorf("errors.app.sshHostNotFound", map[string]any{"host": hostName})
	}
	cmdLine, err := a.ssh.BuildCommandLine(*found)
	if err != nil {
		return err
	}
	prof, err := a.pickTerminal("", "")
	if err != nil {
		return err
	}
	dir, _ := os.UserHomeDir()
	return terminal.OpenWithCommand(prof, dir, "ssh:"+found.Name, "lara4ever - ssh "+found.Name, cmdLine, nil)
}

// pickTerminal resolves a terminal.Profile from an explicit kind, falling back
// to the per-project saved terminal (when project != ""), then the global
// default, then the first available terminal.
func (a *App) pickTerminal(kind, project string) (terminal.Profile, error) {
	profs := terminal.Detect(paths.CmderRoot())
	cfg := a.cfg.Get().Projects

	resolved := kind
	if resolved == "" && project != "" {
		resolved = cfg.Terminals[project]
	}
	if resolved == "" {
		resolved = cfg.DefaultTerminal
	}
	if resolved == "" {
		for _, p := range profs {
			if p.Available {
				resolved = string(p.Kind)
				break
			}
		}
	}
	if resolved == "" {
		return terminal.Profile{}, i18n.Errorf("errors.app.noTerminalAvailable", nil)
	}
	var picked terminal.Profile
	for _, p := range profs {
		if string(p.Kind) == resolved {
			picked = p
			break
		}
	}
	if !picked.Available {
		return terminal.Profile{}, i18n.Errorf("errors.app.terminalNotInstalled", map[string]any{"kind": resolved})
	}
	return picked, nil
}

// resolveProjectTerminal bundles everything needed to launch an external
// terminal for a project: the picked profile, the PATH prefix (project PHP +
// active Node), the project directory, and the banner. Persists an explicit
// kind pick, matching OpenProjectTerminal's original behavior.
func (a *App) resolveProjectTerminal(name, kind string) (prof terminal.Profile, pathPrefix []string, projectDir, banner string, err error) {
	if name == "" {
		return terminal.Profile{}, nil, "", "", i18n.Errorf("errors.app.nameRequired", nil)
	}
	projectDir = filepath.Join(a.projects.Root(), name)
	if _, statErr := os.Stat(projectDir); statErr != nil {
		return terminal.Profile{}, nil, "", "", i18n.Wrapf(statErr, "errors.app.projectFolderNotFound", nil)
	}
	prof, err = a.pickTerminal(kind, name)
	if err != nil {
		return terminal.Profile{}, nil, "", "", err
	}
	// Persist the explicit user pick so the next click defaults to it.
	if kind != "" {
		_ = a.cfg.SetProjectTerminal(name, kind)
	}
	pathPrefix, err = a.projectRuntimePaths(name, projectDir)
	if err != nil {
		return terminal.Profile{}, nil, "", "", err
	}
	version := a.resolveProjectPhp(name)
	banner = "lara4ever - " + name
	if version != "" {
		banner += " (PHP " + version + ")"
	}
	return prof, pathPrefix, projectDir, banner, nil
}

func (a *App) OpenURL(url string) {
	wruntime.BrowserOpenURL(a.ctx, url)
}

// ===== Agent sessions =====

func (a *App) GetAgentSessionsEnabled() bool {
	return a.cfg.Get().Experimental.AgentSessions
}

func (a *App) SetAgentSessionsEnabled(enabled bool) error {
	unlockActivity := a.lockAgentActivityLifecycle()
	defer unlockActivity()
	if !enabled {
		if err := a.activityMutationAllowed(); err != nil {
			return err
		}
	}
	if !enabled && a.agents.HasLiveSessions() {
		return i18n.Errorf("errors.app.stopLiveAgentSessionsFirst", nil)
	}
	if !enabled && a.devsrv != nil {
		for _, status := range a.devsrv.ListRunning() {
			if _, worktree := agentWorktreeSessionID(status.Instance); worktree {
				return i18n.Errorf("errors.app.stopWorktreeDevServersFirst", nil)
			}
		}
	}
	if err := a.cfg.SetAgentSessionsEnabled(enabled); err != nil {
		return err
	}
	if enabled {
		a.startAgentActivity(a.activityContext())
		if !a.cfg.Get().Experimental.AgentHooksDisabled {
			if executable, err := os.Executable(); err == nil {
				pkgagents.InstallHooks(a.agentHookPaths(), executable)
			}
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "agent:hooks-changed", true)
			}
		}
	} else {
		a.stopAgentActivity()
		if !a.cfg.Get().Experimental.AgentHooksDisabled {
			pkgagents.UninstallHooks(a.agentHookPaths())
		}
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "agent:hooks-changed", false)
		}
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "agent:feature-changed", enabled)
	}
	return nil
}

func (a *App) ResetAgentMetadata() (string, error) {
	unlockActivity := a.lockAgentActivityLifecycle()
	defer unlockActivity()
	if err := a.activityMutationAllowed(); err != nil {
		return "", err
	}
	archived, err := a.agents.ResetMetadata()
	if err == nil {
		a.stopAgentActivity()
		a.startAgentActivity(a.activityContext())
	}
	return archived, err
}

func (a *App) requireAgentSessionsEnabled() error {
	if !a.GetAgentSessionsEnabled() {
		return i18n.Errorf("errors.app.agentSessionsDisabled", nil)
	}
	return nil
}

func (a *App) agentHookPaths() pkgagents.HookPaths {
	home, _ := os.UserHomeDir()
	return pkgagents.DefaultHookPaths(os.Getenv, home)
}

// agentHooksWanted reports whether the provider lifecycle hooks should be
// installed: they are on by default whenever Agent sessions is enabled,
// unless the user has explicitly opted out.
func (a *App) agentHooksWanted() bool {
	exp := a.cfg.Get().Experimental
	return exp.AgentSessions && !exp.AgentHooksDisabled
}

func (a *App) GetAgentHooksEnabled() bool {
	return a.agentHooksWanted()
}

func (a *App) GetAgentHooksStatus() []pkgagents.HookInstallStatus {
	return pkgagents.HookStatus(a.agentHookPaths())
}

// SetAgentHooksEnabled installs or removes the provider hooks that let LaraEnv
// adopt agents started inside its own terminals. Providers whose config
// folder is missing are reported, not treated as failures.
func (a *App) SetAgentHooksEnabled(enabled bool) ([]pkgagents.HookInstallStatus, error) {
	if enabled {
		if err := a.requireAgentSessionsEnabled(); err != nil {
			return nil, err
		}
	}
	var statuses []pkgagents.HookInstallStatus
	if enabled {
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		statuses = pkgagents.InstallHooks(a.agentHookPaths(), executable)
	} else {
		statuses = pkgagents.UninstallHooks(a.agentHookPaths())
	}
	if err := a.cfg.SetAgentHooksDisabled(!enabled); err != nil {
		return statuses, err
	}
	a.agents.SetActivityHooksEnabled(enabled)
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "agent:hooks-changed", a.agentHooksWanted())
	}
	return statuses, nil
}

// resolveAgentLocation maps a hook's cwd to a LaraEnv project checkout or one
// of its git worktrees. Anything else is not adopted.
func (a *App) resolveAgentLocation(cwd string) (pkgagents.ProjectLocation, bool) {
	list, err := a.projects.List()
	if err != nil {
		return pkgagents.ProjectLocation{}, false
	}
	ctx := a.appContext()
	for _, project := range list {
		if pkgagents.PathWithin(project.Path, cwd) {
			info := a.gitworkspaces.Inspect(ctx, project.Path)
			return pkgagents.ProjectLocation{
				ProjectName: project.Name, ProjectPath: project.Path, Branch: info.CurrentBranch, Mode: pkgagents.LaunchCurrent,
			}, true
		}
	}
	for _, project := range list {
		info := a.gitworkspaces.Inspect(ctx, project.Path)
		for _, worktree := range info.Worktrees {
			if pkgagents.NormalizeProjectPath(worktree.Path) == pkgagents.NormalizeProjectPath(info.Root) {
				continue
			}
			// Resume rebuilds this workspace from session.WorktreeName under
			// paths.WorktreesRoot() (see Manager.Resume / gitworkspace.Resolve
			// with WorktreeName), so only adopt a worktree LaraEnv itself
			// created there. A hand-made worktree living elsewhere would
			// resume into a different checkout entirely, so it is left
			// unadopted rather than silently mismatched later.
			if !pkgagents.PathWithin(paths.WorktreesRoot(), worktree.Path) {
				continue
			}
			if pkgagents.PathWithin(worktree.Path, cwd) {
				return pkgagents.ProjectLocation{
					ProjectName: project.Name, ProjectPath: worktree.Path, WorktreePath: worktree.Path,
					Branch: worktree.Branch, Mode: pkgagents.LaunchWorktree,
				}, true
			}
		}
	}
	return pkgagents.ProjectLocation{}, false
}

func (a *App) ListAgentProfiles() []pkgagents.AgentProfile {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.agents.ListProfiles(ctx)
}

func (a *App) RefreshAgentProfiles() []pkgagents.AgentProfile {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.agents.RefreshProfiles(ctx)
}

func (a *App) ListAgentSessions(projectName string, includeArchived bool) []pkgagents.AgentSession {
	return a.agents.ListSessions(projectName, includeArchived)
}

func (a *App) GetAgentGitWorkspace(projectName string) (gitworkspace.RepositoryInfo, error) {
	projectsList, err := a.projects.List()
	if err != nil {
		return gitworkspace.RepositoryInfo{}, err
	}
	for _, project := range projectsList {
		if project.Name == projectName {
			ctx := a.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			return a.gitworkspaces.Inspect(ctx, project.Path), nil
		}
	}
	return gitworkspace.RepositoryInfo{}, i18n.Errorf("errors.app.projectNotFound", map[string]any{"name": projectName})
}

func (a *App) LaunchAgentSession(options pkgagents.AgentLaunchOptions) (pkgagents.AgentSession, error) {
	if err := a.requireAgentSessionsEnabled(); err != nil {
		return pkgagents.AgentSession{}, err
	}
	a.startAgentActivity(a.activityContext())
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	session, err := a.agents.Launch(ctx, options)
	if err == nil && a.watcher != nil {
		_ = a.watcher.AddRuntimePath(session.ProjectName, session.ProjectPath)
	}
	return session, err
}

func (a *App) ResumeAgentSession(sessionID string) (pkgagents.AgentSession, error) {
	if err := a.requireAgentSessionsEnabled(); err != nil {
		return pkgagents.AgentSession{}, err
	}
	a.startAgentActivity(a.activityContext())
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	session, err := a.agents.Resume(ctx, sessionID)
	if err == nil && a.watcher != nil {
		_ = a.watcher.AddRuntimePath(session.ProjectName, session.ProjectPath)
	}
	return session, err
}

func (a *App) StopAgentSession(sessionID string) error {
	return a.agents.Stop(sessionID)
}

func (a *App) RenameAgentSession(sessionID, title string) (pkgagents.AgentSession, error) {
	return a.agents.Rename(sessionID, title)
}

func (a *App) ArchiveAgentSession(sessionID string) (pkgagents.AgentSession, error) {
	return a.agents.Archive(sessionID)
}

func (a *App) MarkAgentSessionRead(sessionID string) (pkgagents.AgentSession, error) {
	return a.agents.MarkRead(sessionID)
}

func (a *App) SetFocusedAgentSession(sessionID string) error {
	if sessionID == "" {
		a.agents.SetFocused("")
		return nil
	}
	_, err := a.agents.MarkRead(sessionID)
	return err
}

func (a *App) ListAgentUsage() []pkgagents.UsageSnapshot {
	if !a.GetAgentSessionsEnabled() {
		return []pkgagents.UsageSnapshot{}
	}
	ctx := a.appContext()
	snapshots := a.agentUsage.List(ctx)
	a.notifications.ObserveUsage(snapshots)
	return snapshots
}

func (a *App) RefreshAgentUsage() []pkgagents.UsageSnapshot {
	if !a.GetAgentSessionsEnabled() {
		return []pkgagents.UsageSnapshot{}
	}
	ctx := a.appContext()
	snapshots := a.agentUsage.Refresh(ctx)
	a.notifications.ObserveUsage(snapshots)
	wruntime.EventsEmit(ctx, "agent:usage-changed", snapshots)
	return snapshots
}

func (a *App) ListNotifications() []pkgnotifications.Entry {
	return a.notifications.List()
}

func (a *App) UnreadNotificationCount() int {
	return a.notifications.UnreadCount()
}

func (a *App) NotificationPreferences() pkgnotifications.Preferences {
	return a.notifications.Preferences()
}

func (a *App) SetNotificationPreferences(preferences pkgnotifications.Preferences) error {
	return a.notifications.SetPreferences(preferences)
}

func (a *App) MarkNotificationRead(id string) error {
	return a.notifications.MarkRead(id)
}

func (a *App) MarkAllNotificationsRead() error {
	return a.notifications.MarkAllRead()
}

// ===== PTY (multi-sessão) =====

func (a *App) StartPty(shell string, cols, rows int) (string, error) {
	return a.startPtyCmd(shell, "", nil, cols, rows)
}

// StartPtyKind starts an in-app PTY using a terminal kind ("powershell",
// "cmd", "gitbash", "cmder"). Empty kind falls back to the saved global
// default. Returns the new PTY id.
func (a *App) StartPtyKind(kind string, cols, rows int) (string, error) {
	picked, err := a.resolveTerminal(kind, "")
	if err != nil {
		return "", err
	}
	cmdLine := ptyCommandLine(picked)
	if cmdLine == "" {
		return "", i18n.Errorf("errors.app.terminalKindNotEmbeddable", map[string]any{"kind": picked.Kind})
	}
	return a.startPtyCmd(cmdLine, "", nil, cols, rows)
}

// StartPtyForProject is StartPtyKind but scoped to a project: cwd is the
// project's folder and PATH is prefixed with the project's resolved PHP
// version. Used by the "open terminal" action in Projects to spawn a tab
// in the dedicated Terminal page.
func (a *App) StartPtyForProject(name, kind string, cols, rows int) (string, error) {
	return startProjectPty(name, kind, a.projects.Root(), cols, rows, a.startProjectShell, a.cfg.SetProjectTerminal)
}

// StartPtyForProjectCommand opens a project shell for a one-off command without
// changing the user's preferred terminal. Runtime and shell resolution are the
// same as StartPtyForProject, including saved preferences when kind is empty.
func (a *App) StartPtyForProjectCommand(name, kind string, cols, rows int) (string, error) {
	return startProjectPty(name, kind, a.projects.Root(), cols, rows, a.startProjectShell, nil)
}

// resolveTerminal picks a terminal profile given an explicit kind, the
// per-project preference (when projectName is non-empty), the global default,
// then the first available profile.
func (a *App) resolveTerminal(kind, projectName string) (terminal.Profile, error) {
	resolved := kind
	cfg := a.cfg.Get().Projects
	if resolved == "" && projectName != "" {
		resolved = cfg.Terminals[projectName]
	}
	if resolved == "" {
		resolved = cfg.DefaultTerminal
	}
	profs := terminal.Detect(paths.CmderRoot())
	if resolved == "" {
		for _, p := range profs {
			if p.Available {
				resolved = string(p.Kind)
				break
			}
		}
	}
	for _, p := range profs {
		if string(p.Kind) == resolved {
			if !p.Available {
				return terminal.Profile{}, i18n.Errorf("errors.app.terminalNotInstalled", map[string]any{"kind": resolved})
			}
			return p, nil
		}
	}
	return terminal.Profile{}, i18n.Errorf("errors.app.noTerminalAvailable", nil)
}

// ptyCommandLine maps a detected terminal profile to a command line that
// ConPTY can spawn inline. Cmder is special: its launcher opens its own
// window, so we instead spawn cmd.exe and source Cmder's init.bat to get
// the same prompt/env inside our embedded PTY.
func ptyCommandLine(p terminal.Profile) string {
	switch p.Kind {
	case terminal.KindPowerShell:
		return p.Path
	case terminal.KindCmd:
		return p.Path
	case terminal.KindGitBash:
		return fmt.Sprintf(`"%s" --login -i`, p.Path)
	case terminal.KindCmder:
		// p.Path is .../cmder/Cmder.exe → vendor\init.bat sits next to it.
		init := filepath.Join(filepath.Dir(p.Path), "vendor", "init.bat")
		if _, err := os.Stat(init); err != nil {
			return ""
		}
		return fmt.Sprintf(`cmd.exe /K "%s"`, init)
	}
	return ""
}

func (a *App) startPtyCmd(cmdLine, cwd string, extraPath []string, cols, rows int) (string, error) {
	if active, _ := a.php.ActivePath(); active != "" {
		extraPath = append(extraPath, active)
	}
	if active, _ := a.nodejs.ActivePath(); active != "" {
		extraPath = append(extraPath, active)
	}
	// Composer wrapper lives next to composer.phar; ensure it's resolvable
	// without polluting the system PATH.
	if _, err := os.Stat(filepath.Join(paths.ComposerRoot(), "composer.bat")); err == nil {
		extraPath = append(extraPath, paths.ComposerRoot())
	}
	id, sess, err := a.terminals.New(cmdLine, cols, rows, extraPath, cwd)
	if err != nil {
		return "", err
	}
	go func(id string, sess pkgpty.Session) {
		buf := make([]byte, 4096)
		dataEvent := "term:data:" + id
		exitEvent := "term:exit:" + id
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				wruntime.EventsEmit(a.ctx, dataEvent, string(buf[:n]))
			}
			if err != nil {
				wruntime.EventsEmit(a.ctx, exitEvent, err.Error())
				if a.agents != nil {
					a.agents.TerminalClosed(id)
				}
				return
			}
		}
	}(id, sess)
	return id, nil
}

func (a *App) WritePty(id string, data string) error {
	s, ok := a.terminals.Get(id)
	if !ok {
		return i18n.Errorf("errors.app.ptyNotFound", map[string]any{"id": id})
	}
	_, err := s.Write([]byte(data))
	return err
}

func (a *App) ResizePty(id string, cols, rows int) error {
	s, ok := a.terminals.Get(id)
	if !ok {
		return nil
	}
	return s.Resize(cols, rows)
}

func (a *App) StopPty(id string) error { return a.terminals.Close(id) }

// StartPtyKindIn is StartPtyKind but spawns the shell in an explicit working
// directory. Used by the terminal-workspace restore flow to bring a plain local
// pane back in the folder it was originally opened in. Empty cwd behaves like
// StartPtyKind.
func (a *App) StartPtyKindIn(kind, cwd string, cols, rows int) (string, error) {
	picked, err := a.resolveTerminal(kind, "")
	if err != nil {
		return "", err
	}
	cmdLine := ptyCommandLine(picked)
	if cmdLine == "" {
		return "", i18n.Errorf("errors.app.terminalKindNotEmbeddable", map[string]any{"kind": picked.Kind})
	}
	return a.startPtyCmd(cmdLine, cwd, nil, cols, rows)
}

// StartPtyKindRuntime is StartPtyKindIn plus explicit PHP/Node version overrides
// prepended to PATH, mirroring StartPtyForProject's path logic but for local
// (non-project) terminals. An empty version means "leave it to the global active
// version" (current behavior). Used by the per-terminal runtime picker so a
// detached/local shell can pin the PHP and Node it loads.
func (a *App) StartPtyKindRuntime(kind, cwd, phpVersion, nodeVersion string, cols, rows int) (string, error) {
	picked, err := a.resolveTerminal(kind, "")
	if err != nil {
		return "", err
	}
	cmdLine := ptyCommandLine(picked)
	if cmdLine == "" {
		return "", i18n.Errorf("errors.app.terminalKindNotEmbeddable", map[string]any{"kind": picked.Kind})
	}
	var pathPrefix []string
	if phpVersion != "" {
		if dir := a.php.VersionDir(phpVersion); dir != "" {
			pathPrefix = append(pathPrefix, dir)
		}
	}
	if nodeVersion != "" {
		if dir := a.nodejs.ResolvePath(nodeVersion); dir != "" {
			pathPrefix = append(pathPrefix, dir)
		}
	}
	return a.startPtyCmd(cmdLine, cwd, pathPrefix, cols, rows)
}

// SaveTerminalWorkspace persists the frontend's serialized terminal-canvas
// layout (groups/tabs/splits + spawn descriptors). The backend treats it as
// opaque JSON.
func (a *App) SaveTerminalWorkspace(raw string) error {
	if raw == "" {
		return a.cfg.SetTerminalWorkspace(nil)
	}
	return a.cfg.SetTerminalWorkspace(json.RawMessage(raw))
}

// LoadTerminalWorkspace returns the persisted terminal-canvas layout JSON, or
// "" when none was saved (fresh install / first run).
func (a *App) LoadTerminalWorkspace() string {
	raw := a.cfg.GetTerminalWorkspace()
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}

// GetSystemMetrics returns one synchronous sample. Useful for an immediate
// paint before the periodic event stream warms up.
func (a *App) GetSystemMetrics() sysmetrics.Sample {
	return a.sysmet.Sample(a.ctx)
}

// ===== SSH =====

// ListSSHHosts returns all saved SSH hosts. UI calls this on mount + after
// any "ssh:hosts-changed" event to refresh.
func (a *App) ListSSHHosts() []config.SSHHost {
	hosts := a.cfg.ListSSHHosts()
	// Stable sort by name so the UI doesn't reshuffle on every refresh.
	sortHostsByName(hosts)
	return hosts
}

// SaveSSHHost upserts a host (creates if Name is new, updates otherwise).
// If sync is enabled and the user is Pro, also pushes the host to the
// cloud. Cloud failures are non-fatal — local save always wins.
func (a *App) SaveSSHHost(h config.SSHHost) error {
	if h.Port == 0 {
		h.Port = 22
	}
	if err := a.cfg.SetSSHHost(h); err != nil {
		return err
	}
	wruntime.EventsEmit(a.ctx, "ssh:hosts-changed", h.Name)
	// Use canonical saved state: persistence may clear an old recovery flag,
	// or retain an unavailable portable password that must never be uploaded empty.
	if saved, ok := a.cfg.Get().Projects.SSHHosts[h.Name]; ok {
		a.maybeSyncPushSSH(saved)
	}
	return nil
}

// DeleteSSHHost removes a host from config. Key files on disk are kept on
// purpose. If sync is enabled, also tells the cloud to forget it.
func (a *App) DeleteSSHHost(name string) error {
	if err := a.cfg.RemoveSSHHost(name); err != nil {
		return err
	}
	wruntime.EventsEmit(a.ctx, "ssh:hosts-changed", name)
	a.maybeSyncDeleteResource(cloud.KindSSHHost, name)
	return nil
}

// syncShouldRun reports whether we should attempt cloud writes: user is logged
// in, has a sync key, has Pro plan, and the toggle is on.
func (a *App) syncShouldRun() bool {
	cl := a.cfg.Get().Cloud
	return cl.Enabled && cl.Token != "" && cl.SyncKey != "" && cl.Plan == "pro" && cl.SyncEnabled
}

// maybeSyncPushSSH pushes one SSH host to the cloud in the background if
// sync is enabled. Best-effort — errors are logged, never surfaced to the UI.
func (a *App) maybeSyncPushSSH(h config.SSHHost) {
	if !a.syncShouldRun() {
		return
	}
	if h.PasswordUnavailable {
		fmt.Printf("[cloud] skipped ssh_host %s: saved password unavailable on this Windows account\n", h.Name)
		return
	}
	opCtx, cancel, err := a.beginCloudSyncSession()
	if err != nil {
		return
	}
	go func() {
		defer cancel()
		defer a.reportCloudSyncStatus()
		b, err := json.Marshal(h)
		if err != nil {
			fmt.Printf("[cloud] marshal ssh_host: %v\n", err)
			return
		}
		if err := a.cloud.PutResourceWithRetry(opCtx, cloud.KindSSHHost, h.Name, b); err != nil {
			fmt.Printf("[cloud] push ssh_host %s: %v\n", h.Name, err)
		}
	}()
}

// TestSSHConnection performs a real SSH dial + auth using the values from
// the form (without persisting), then closes the session immediately. Returns
// the elapsed milliseconds on success, or a friendly error message.
type SSHTestResult struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	ElapsedMs int64  `json:"elapsedMs"`
}

func (a *App) TestSSHConnection(h config.SSHHost) SSHTestResult {
	if h.Port == 0 {
		h.Port = 22
	}
	start := time.Now()
	sess, err := a.ssh.Connect(h, 80, 24, nil)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return SSHTestResult{OK: false, Message: err.Error(), ElapsedMs: elapsed}
	}
	_ = sess.Close()
	return SSHTestResult{OK: true, Message: i18n.T("backend.app.sshConnectionOk", nil), ElapsedMs: elapsed}
}

// GenerateSSHKey creates a new keypair for a host under C:\laraenv\ssh\<host>\.
// The .pub contents are returned so the UI can show a "copy to clipboard" view
// for pasting into the remote authorized_keys.
func (a *App) GenerateSSHKey(host, keyName, kind, comment string) (*pkgssh.GeneratedKey, error) {
	if host == "" {
		return nil, i18n.Errorf("errors.app.hostRequired", nil)
	}
	dir := paths.SSHHostDir(host)
	gk, err := pkgssh.Generate(dir, keyName, kind, comment)
	if err != nil {
		return nil, err
	}
	return gk, nil
}

// ImportSSHKey opens a file picker for the user to choose an existing
// private-key file (e.g. ~/.ssh/id_rsa from another machine) and copies it
// into C:\laraenv\ssh\<host>\. Returns the imported key metadata, or nil if
// the user cancels the dialog.
func (a *App) ImportSSHKey(host string) (*pkgssh.GeneratedKey, error) {
	if host == "" {
		return nil, i18n.Errorf("errors.app.hostRequired", nil)
	}
	src, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: i18n.T("backend.app.pickPrivateKeyTitle", map[string]any{"host": host}),
		Filters: []wruntime.FileFilter{
			// Pattern is the OS-level glob Wails passes to the native dialog —
			// never translated, must stay byte-identical. Only DisplayName
			// (the label the user reads in the file-type dropdown) is prose.
			{DisplayName: i18n.T("backend.app.sshKeyFilterLabel", nil), Pattern: "*;id_*;*.pem"},
			{DisplayName: i18n.T("backend.app.allFilesFilterLabel", nil), Pattern: "*.*"},
		},
	})
	if err != nil {
		return nil, err
	}
	if src == "" {
		return nil, nil // user cancelled
	}
	return pkgssh.Import(paths.SSHHostDir(host), src, "")
}

// PickAndReadSSHKeyFile opens the file picker and returns the file's
// contents as a string. The frontend uses this to pre-fill the Import Key
// modal's textareas (so the user can review/edit before saving).
type PickedKeyFile struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

func (a *App) PickAndReadSSHKeyFile() (*PickedKeyFile, error) {
	src, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: i18n.T("backend.app.pickPrivateKeyFileTitle", nil),
		Filters: []wruntime.FileFilter{
			// Same filter labels as ImportSSHKey's dialog above — one key
			// each, reused rather than duplicated, since the text is
			// identical between the two pickers.
			{DisplayName: i18n.T("backend.app.sshKeyFilterLabel", nil), Pattern: "*;id_*;*.pem"},
			{DisplayName: i18n.T("backend.app.allFilesFilterLabel", nil), Pattern: "*.*"},
		},
	})
	if err != nil {
		return nil, err
	}
	if src == "" {
		return nil, nil
	}
	content, err := pkgssh.ReadFileText(src)
	if err != nil {
		return nil, err
	}
	return &PickedKeyFile{Filename: filepath.Base(src), Content: content}, nil
}

// SaveSSHKeyText writes a pasted (or file-loaded) keypair into the host's
// directory. publicKey is optional — when empty, derived from privateKey.
func (a *App) SaveSSHKeyText(host, name, privateKey, publicKey string) (*pkgssh.GeneratedKey, error) {
	if host == "" {
		return nil, i18n.Errorf("errors.app.hostRequired", nil)
	}
	return pkgssh.SaveText(paths.SSHHostDir(host), name, privateKey, publicKey)
}

// ListSSHKeys returns the private-key filenames stored under a host's dir.
func (a *App) ListSSHKeys(host string) ([]string, error) {
	if host == "" {
		return []string{}, nil
	}
	return pkgssh.ListKeys(paths.SSHHostDir(host))
}

// ReadSSHPublicKey returns the OpenSSH-format public key for a host's keyName.
func (a *App) ReadSSHPublicKey(host, keyName string) (string, error) {
	return pkgssh.ReadPublicKey(paths.SSHHostDir(host), keyName)
}

// StartPtyForSSH opens a native SSH connection (via golang.org/x/crypto/ssh)
// to the given saved host and exposes it as a PTY tab in the integrated
// terminal. Going native lets us use the saved password directly — the
// Windows ssh.exe never accepts passwords on the command line, so spawning
// it would always re-prompt the user.
//
// reqId is an opaque string the frontend sends so it can subscribe to the
// per-connection progress channel "ssh:progress:<reqId>" before the call —
// without it we'd have a race between the first emit and the JS listener.
func (a *App) StartPtyForSSH(hostName, reqId string, cols, rows int) (string, error) {
	fmt.Printf("[app] StartPtyForSSH called: host=%q reqId=%q cols=%d rows=%d\n", hostName, reqId, cols, rows)
	if hostName == "" {
		return "", i18n.Errorf("errors.app.hostRequired", nil)
	}
	hosts := a.cfg.ListSSHHosts()
	var found *config.SSHHost
	for i := range hosts {
		if hosts[i].Name == hostName {
			found = &hosts[i]
			break
		}
	}
	if found == nil {
		return "", i18n.Errorf("errors.app.sshHostNotFound", map[string]any{"host": hostName})
	}

	progressEvent := ""
	if reqId != "" {
		progressEvent = "ssh:progress:" + reqId
	}
	onStage := func(stage string) {
		if progressEvent != "" {
			wruntime.EventsEmit(a.ctx, progressEvent, stage)
		}
	}

	sess, err := a.ssh.Connect(*found, cols, rows, onStage)
	if err != nil {
		if progressEvent != "" {
			wruntime.EventsEmit(a.ctx, progressEvent, "error")
		}
		return "", err
	}
	id := a.terminals.Register(sess)
	// Track the underlying SSH client so SFTP can layer on top of it
	// without re-authenticating.
	a.sshClients.Store(id, sess.Client())

	// Mirror the data-pump used by startPtyCmd so the frontend gets the
	// same term:data:<id> / term:exit:<id> events as a local PTY.
	go func(id string, s pkgpty.Session) {
		buf := make([]byte, 4096)
		dataEvent := "term:data:" + id
		exitEvent := "term:exit:" + id
		for {
			n, err := s.Read(buf)
			if n > 0 {
				wruntime.EventsEmit(a.ctx, dataEvent, string(buf[:n]))
			}
			if err != nil {
				// Tear down any SFTP client riding on this connection.
				a.sftp.Close(id)
				a.sshClients.Delete(id)
				wruntime.EventsEmit(a.ctx, exitEvent, err.Error())
				if a.agents != nil {
					a.agents.TerminalClosed(id)
				}
				return
			}
		}
	}(id, sess)

	return id, nil
}

// ===== SFTP =====

// resolveSSHClient returns the live *xssh.Client for a terminal id, or an
// error if the terminal is unknown / not an SSH session.
func (a *App) resolveSSHClient(termID string) (*xssh.Client, error) {
	v, ok := a.sshClients.Load(termID)
	if !ok {
		return nil, i18n.Errorf("errors.app.noSshConnectionForTerminal", map[string]any{"termId": termID})
	}
	c, _ := v.(*xssh.Client)
	return c, nil
}

// SFTPOpen lazily creates the SFTP client for the given SSH terminal and
// returns the remote working directory (typically the user's home).
func (a *App) SFTPOpen(termID string) (string, error) {
	cli, err := a.sftp.Open(termID, func() (*xssh.Client, error) {
		return a.resolveSSHClient(termID)
	})
	if err != nil {
		return "", err
	}
	wd, err := cli.Getwd()
	if err != nil {
		return "/", nil
	}
	return wd, nil
}

// SFTPClose tears down the SFTP client for a terminal (the SSH session
// itself stays open). Auto-called when the PTY exits.
func (a *App) SFTPClose(termID string) {
	a.sftp.Close(termID)
}

// SFTPListLocal lists local filesystem entries. termID is unused but kept
// for symmetry with the remote method on the frontend side.
func (a *App) SFTPListLocal(p string) ([]pkgssh.SFTPEntry, error) {
	return pkgssh.ListLocal(p)
}

// SFTPHomeLocal returns the default starting directory for the local pane.
// Prefers the LaraEnv root (C:\laraenv) since that's where users keep their
// projects; falls back to the OS home dir if it doesn't exist.
func (a *App) SFTPHomeLocal() (string, error) {
	if _, err := os.Stat(paths.Root()); err == nil {
		return paths.Root(), nil
	}
	return os.UserHomeDir()
}

func (a *App) SFTPListRemote(termID, p string) ([]pkgssh.SFTPEntry, error) {
	cli, err := a.sftp.Open(termID, func() (*xssh.Client, error) {
		return a.resolveSSHClient(termID)
	})
	if err != nil {
		return nil, err
	}
	return a.sftp.ListRemote(cli, p)
}

func (a *App) SFTPMkdirRemote(termID, p string) error {
	cli, ok := a.sftp.Get(termID)
	if !ok {
		return i18n.Errorf("errors.app.sftpNotOpenForTerminal", map[string]any{"termId": termID})
	}
	return pkgssh.MkdirRemote(cli, p)
}

func (a *App) SFTPRenameRemote(termID, from, to string) error {
	cli, ok := a.sftp.Get(termID)
	if !ok {
		return i18n.Errorf("errors.app.sftpNotOpenForTerminal", map[string]any{"termId": termID})
	}
	return pkgssh.RenameRemote(cli, from, to)
}

func (a *App) SFTPRemoveRemote(termID, p string) error {
	cli, ok := a.sftp.Get(termID)
	if !ok {
		return i18n.Errorf("errors.app.sftpNotOpenForTerminal", map[string]any{"termId": termID})
	}
	return pkgssh.RemoveRemote(cli, p)
}

// SFTPUpload kicks off an asynchronous upload. The frontend should subscribe
// to "sftp:progress:<opId>" before calling. Returns immediately.
func (a *App) SFTPUpload(termID, opID, localPath, remotePath string) error {
	cli, err := a.sftp.Open(termID, func() (*xssh.Client, error) {
		return a.resolveSSHClient(termID)
	})
	if err != nil {
		return err
	}
	go a.sftp.Upload(cli, opID, localPath, remotePath)
	return nil
}

func (a *App) SFTPDownload(termID, opID, remotePath, localPath string) error {
	cli, err := a.sftp.Open(termID, func() (*xssh.Client, error) {
		return a.resolveSSHClient(termID)
	})
	if err != nil {
		return err
	}
	go a.sftp.Download(cli, opID, remotePath, localPath)
	return nil
}

func (a *App) SFTPCancel(opID string) {
	a.sftp.Cancel(opID)
}

// sftpMaxEditBytes caps the size of files openable in the built-in editor.
// Anything larger forces the user into the "open with system editor" flow.
const sftpMaxEditBytes int64 = 5 * 1024 * 1024

// SFTPReadFile reads a remote text file into UTF-8 for the in-app editor.
// Refuses files >5 MiB or files containing NUL bytes in the first 8 KiB —
// frontend matches on the "file too large" / "binary file" prefixes to offer
// the external-editor fallback.
func (a *App) SFTPReadFile(termID, remotePath string) (string, error) {
	cli, err := a.sftp.Open(termID, func() (*xssh.Client, error) {
		return a.resolveSSHClient(termID)
	})
	if err != nil {
		return "", err
	}
	data, err := pkgssh.ReadRemoteFile(cli, remotePath, sftpMaxEditBytes)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SFTPWriteFile saves content back to remotePath, preserving the existing
// file's permissions (uses O_TRUNC, not Create).
func (a *App) SFTPWriteFile(termID, remotePath, content string) error {
	cli, err := a.sftp.Open(termID, func() (*xssh.Client, error) {
		return a.resolveSSHClient(termID)
	})
	if err != nil {
		return err
	}
	return pkgssh.WriteRemoteFile(cli, remotePath, []byte(content))
}

// SFTPOpenExternal downloads the remote file to a fresh temp dir and launches
// the OS default app for that file type. Returns the local temp path so the
// frontend can offer "Reenviar" / cleanup later.
func (a *App) SFTPOpenExternal(termID, remotePath string) (string, error) {
	cli, err := a.sftp.Open(termID, func() (*xssh.Client, error) {
		return a.resolveSSHClient(termID)
	})
	if err != nil {
		return "", err
	}
	local, err := pkgssh.DownloadRemoteToTemp(cli, remotePath)
	if err != nil {
		return "", err
	}
	if err := openWithDefaultApp(local); err != nil {
		// Best effort — surface the path even if launching the app failed,
		// the user can still open it manually.
		return local, i18n.Wrapf(err, "errors.app.launchEditorAfterDownloadFailed", map[string]any{"local": local})
	}
	return local, nil
}

// SFTPReuploadFromTemp pushes the (potentially edited) temp file back to the
// remote path. Used by both the manual "Reenviar" button and the autosave
// watcher event.
func (a *App) SFTPReuploadFromTemp(termID, tempPath, remotePath string) error {
	cli, err := a.sftp.Open(termID, func() (*xssh.Client, error) {
		return a.resolveSSHClient(termID)
	})
	if err != nil {
		return err
	}
	return pkgssh.UploadLocalToRemote(cli, tempPath, remotePath)
}

// SFTPCleanupTemp removes the temp dir created by SFTPOpenExternal. Safe to
// call multiple times.
func (a *App) SFTPCleanupTemp(tempPath string) error {
	return pkgssh.CleanupTemp(tempPath)
}

// SFTPWatchTemp starts polling the temp file's mtime; each advance emits
// "sftp:external-saved:<sessionID>" so the React side can auto-reupload.
func (a *App) SFTPWatchTemp(sessionID, tempPath string) error {
	return a.sftp.WatchTemp(sessionID, tempPath)
}

// SFTPUnwatchTemp stops the watcher for a session.
func (a *App) SFTPUnwatchTemp(sessionID string) {
	a.sftp.UnwatchTemp(sessionID)
}

// OpenFileInDefaultApp launches the OS default handler for a local file path
// (notepad, vscode, etc.). Used by the in-app editors when the user prefers
// the system editor over the built-in Monaco modal.
func (a *App) OpenFileInDefaultApp(path string) error {
	if path == "" {
		return i18n.Errorf("errors.app.emptyPath", nil)
	}
	return openWithDefaultApp(path)
}

// PhpIniPath returns the absolute path of the php.ini for a given PHP version,
// so the UI can launch it in the OS default editor.
func (a *App) PhpIniPath(version string) string {
	return filepath.Join(paths.PHPRoot(), version, "php.ini")
}

// openWithDefaultApp launches the OS default handler for the file. Windows
// uses the URL FileProtocolHandler trick; other platforms use open/xdg-open.
func openWithDefaultApp(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// ===== Updater =====

// UpdateCheckResult is what CheckForUpdate returns to the frontend so the UI
// can decide whether to show the "update available" card without doing its
// own version comparison.
type UpdateCheckResult struct {
	Current     string           `json:"current"`
	Latest      *updater.Release `json:"latest"`
	UpdateAvail bool             `json:"updateAvail"`

	// Portable says which kind of update this is, so the card can name it.
	// The two flavours do very different things to the machine and the wrong
	// one is not obviously wrong until afterwards.
	Portable bool `json:"portable"`
}

// AppVersion returns the version string this binary was built with (set via
// ldflags -X lara4ever/internal/version.Version).
func (a *App) AppVersion() string { return version.Version }

// CheckForUpdate hits GitHub Releases for the latest version and reports
// whether it's newer than what we're running. Synchronous — quick HTTP call.
func (a *App) CheckForUpdate() (*UpdateCheckResult, error) {
	rel, err := a.updater.Check(a.ctx)
	if err != nil {
		return nil, err
	}
	return &UpdateCheckResult{
		Current:     a.updater.CurrentVersion(),
		Latest:      rel,
		UpdateAvail: a.updater.IsNewer(rel),
		Portable:    a.updater.IsPortable(),
	}, nil
}

// DownloadUpdate fetches the MSI for the given release into the cache and
// verifies its sha256. Runs async; progress streams via "update:progress".
// Frontend should subscribe before calling.
func (a *App) DownloadUpdate(r updater.Release) error {
	go func() {
		_, _ = a.updater.Download(a.ctx, &r)
	}()
	return nil
}

// InstallUpdate hands the cached MSI to msiexec and quits the app so the
// installer can replace the binary.
func (a *App) InstallUpdate(downloaded string) error {
	if a.GetSourceControlBusy().Busy {
		return a.sourceControlBusyNotice()
	}
	if a.detached || a.commandBar {
		return a.applyUpdateAfterDocuments(downloaded)
	}
	_, err := a.documentShutdownGate().Request("update", func() error {
		return a.applyUpdateAfterDocuments(downloaded)
	})
	return err
}

// ===== Cloud / Account =====

// CloudStatus returns the cached session info — cheap, no network call.
// Frontend reads this on mount and after every cloud action to refresh
// the Account tab.
func (a *App) CloudStatus() cloud.Status { return a.cloud.Status() }

// CloudSetBaseURL changes the server URL. Logs the user out (token is
// per-server). Returns nothing because errors are persistence-only.
func (a *App) CloudSetBaseURL(url string) error { return a.cloud.SetBaseURL(url) }

// CloudHealth pings the configured server. Used by the Account tab when
// the user changes the URL to verify reachability.
func (a *App) CloudHealth() error { return a.cloud.Health(a.ctx) }

// CloudRandomQuote fetches a random motivational quote from the server.
// Public endpoint — works without auth. Returns nil silently on any error
// or when the server has no quotes; the Dashboard falls back to a hardcoded
// phrase so the UI never breaks because of a server hiccup.
func (a *App) CloudRandomQuote() *cloud.Quote {
	q, err := a.cloud.RandomQuote(a.ctx)
	if err != nil {
		return nil
	}
	return q
}

// CloudRegister creates a new email/password account.
func (a *App) CloudRegister(name, email, password string) (*cloud.User, error) {
	return a.cloud.Register(a.ctx, name, email, password)
}

// CloudLogin swaps email/password for a Sanctum token.
func (a *App) CloudLogin(email, password string) (*cloud.User, error) {
	return a.cloud.Login(a.ctx, email, password)
}

// CloudLogout revokes the token and clears local session.
func (a *App) CloudLogout() error { return a.cloud.Logout(a.ctx) }

// CloudMe refreshes plan/email/name from the server. Called on app startup
// and after Stripe checkout returns to detect plan upgrades.
func (a *App) CloudMe() (*cloud.MeResponse, error) { return a.cloud.Me(a.ctx) }

// CloudLoginWithGithub kicks off the OAuth loopback flow: opens the user's
// default browser, listens on 127.0.0.1:<random> for the redirect, captures
// the token, and persists the session. Blocks for up to 5 minutes.
func (a *App) CloudLoginWithGithub() (*cloud.User, error) {
	return a.cloud.LoginWithGithub(a.ctx)
}

// CloudCheckout starts a Stripe Checkout flow: opens the browser to a fresh
// session URL minted by the server, listens on 127.0.0.1:33001 for the
// return, then polls /me to detect the plan upgrade.
func (a *App) CloudCheckout() error { return a.cloud.StartCheckout(a.ctx) }

// CloudCheckoutPayPal is the PayPal counterpart — opens the PayPal approval
// URL and finalizes the subscription server-side after the user comes back.
func (a *App) CloudCheckoutPayPal() error { return a.cloud.StartPayPalCheckout(a.ctx) }

// ===== Deployments =====

// CloudListDeployments returns all the user's saved deployment targets.
func (a *App) CloudListDeployments() ([]cloud.Deployment, error) {
	return a.cloud.ListDeployments(a.ctx)
}

// CloudGetDeployment fetches one deployment with its run history.
func (a *App) CloudGetDeployment(id int) (*cloud.Deployment, error) {
	return a.cloud.GetDeployment(a.ctx, id)
}

// CloudCreateDeployment creates a new deployment target. The name is the
// user-facing label; sshHostExternalID is the name of one of the user's
// saved SSH hosts (those names are stable, used as the resource external_id).
func (a *App) CloudCreateDeployment(name, sshHostExternalID string) (*cloud.Deployment, error) {
	return a.cloud.CreateDeployment(a.ctx, name, sshHostExternalID)
}

// CloudDeleteDeployment removes a deployment target (and its run history,
// via cascade on the server).
func (a *App) CloudDeleteDeployment(id int) error {
	return a.cloud.DeleteDeployment(a.ctx, id)
}

// deployStepInfo is the JSON shape the deployment plan events send to the
// frontend for one step: Title (plain English, unused by the GUI but kept
// for parity with what cmd/deploy-tui prints) plus TitleKey/TitleVars, which
// Deployments.tsx renders via t(titleKey, titleVars), falling back to Title
// when TitleKey is empty.
type deployStepInfo struct {
	Title     string         `json:"title"`
	TitleKey  string         `json:"titleKey,omitempty"`
	TitleVars map[string]any `json:"titleVars,omitempty"`
}

// deployStepInfos converts a built plan into the wire shape for the
// "deployment:*:plan:<id>" events. Sent once, upfront, so the UI can render
// the full step list at 0% before any step actually runs; per-step status
// updates afterward only need idx/status, since the frontend already has
// title/titleKey/titleVars from this.
func deployStepInfos(steps []cloud.Step) []deployStepInfo {
	out := make([]deployStepInfo, len(steps))
	for i, st := range steps {
		out[i] = deployStepInfo{Title: st.Title, TitleKey: st.TitleKey, TitleVars: st.TitleVars}
	}
	return out
}

// CloudDeploymentApply executes the user-confirmed apply plan against the
// remote. Builds the plan from the saved deployment config + the latest
// detection (taken from deployment.config.detection), then runs each step
// via SSH, streaming the combined log. Persists the encrypted log + status
// in deployment_runs at the end.
//
// Async — returns immediately. UI subscribes to:
//
//	"deployment:apply:log:<id>"   per-line stream
//	"deployment:apply:done:<id>"  payload = nil
//	"deployment:apply:error:<id>" payload = error message string
func (a *App) CloudDeploymentApply(id int, applyCfg cloud.ApplyConfig) error {
	release, err := a.acquireDeploymentOperation()
	if err != nil {
		return err
	}
	opCtx, cancel, session, err := a.cloud.BeginSession(a.ctx)
	if err != nil {
		release()
		return err
	}
	launched := false
	defer func() {
		if !launched {
			release()
			cancel()
		}
	}()
	applyCfg, err = snapshotApply(applyCfg)
	if err != nil {
		return err
	}
	dep, err := a.cloud.GetDeployment(opCtx, id)
	if err != nil {
		return err
	}

	var host *config.SSHHost
	for _, h := range a.cfg.ListSSHHosts() {
		if h.Name == dep.SSHHostExternalID {
			h := h
			host = &h
			break
		}
	}
	if host == nil {
		return i18n.Errorf("errors.app.sshHostNotFoundLocally", map[string]any{"host": dep.SSHHostExternalID})
	}

	sshManager, snapshotHosts, err := a.ssh.Snapshot(*host)
	if err != nil {
		return err
	}
	redact := deploymentRedactor(applyCfg, snapshotHosts...)
	// Credentials are used only by the executor; hosted config receives a URL
	// without userinfo and never receives the PAT field.
	persistedCfg := applyCfg
	persistedCfg.GitPAT = ""
	applyCfg.GitURL = cloud.SpliceGitPAT(applyCfg.GitURL, strings.TrimSpace(applyCfg.GitPAT))

	// Persist the user's config back to the server first so we have a record
	// of *what* we tried to apply, even if the run fails.
	cfgPatch := map[string]any{
		"project_kind": applyCfg.ProjectKind,
		"config": map[string]any{
			"detection": dep.Config["detection"], // preserve previous detection
			"apply":     persistedCfg,
		},
	}
	if _, err := a.cloud.PatchDeployment(opCtx, id, cfgPatch); err != nil {
		return i18n.Wrapf(err, "errors.app.saveConfigFailed", nil)
	}

	// Pull the detection result back out of the saved config so the plan can
	// know which packages are already installed.
	var det *cloud.DetectionResult
	if dep.Config != nil {
		if d, ok := dep.Config["detection"].(map[string]any); ok {
			det = decodeDetection(d)
		}
	}

	// Resolve + render the deploy pipeline (preset / override / legacy
	// build script) up front so a missing preset fails fast, before any SSH.
	rendered, err := cloud.RenderDeployPipeline(applyCfg, a.presetScriptLookup)
	if err != nil {
		return err
	}

	startedAt := time.Now().UTC().Format(time.RFC3339)
	logEvt := fmt.Sprintf("deployment:apply:log:%d", id)
	stepEvt := fmt.Sprintf("deployment:apply:step:%d", id)
	planEvt := fmt.Sprintf("deployment:apply:plan:%d", id)
	doneEvt := fmt.Sprintf("deployment:apply:done:%d", id)
	errEvt := fmt.Sprintf("deployment:apply:error:%d", id)

	launched = true
	go func() {
		defer release()
		defer cancel()
		emit := func(line string) { a.emitHostedDeployment(opCtx, logEvt, redact(line)) }
		emitStep := func(idx, total int, title, status string) {
			a.emitHostedDeployment(opCtx, stepEvt, map[string]any{
				"idx":    idx,
				"total":  total,
				"title":  title,
				"status": status,
			})
		}

		emit(i18n.T("backend.app.deployConnecting", map[string]any{"user": host.User, "address": host.Address, "port": host.Port}))
		client, err := sshManager.Dial(*host)
		if err != nil {
			emit("ERR " + err.Error())
			a.emitHostedDeployment(opCtx, errEvt, err.Error())
			_ = a.cloud.StoreDeploymentRun(opCtx, id, "apply", "error", "", err.Error(), startedAt, time.Now().UTC().Format(time.RFC3339))
			return
		}
		defer client.Close()

		plan := cloud.BuildPlan(applyCfg, det, rendered.Pipeline, rendered.Legacy)
		// Emit the plan upfront so the UI can render the step list at 0%
		// before any individual step actually runs.
		a.emitHostedDeployment(opCtx, planEvt, deployStepInfos(plan))
		emit(i18n.T("backend.app.deployPlan", map[string]any{"count": len(plan)}))

		log, runErr := cloud.RunApply(client, plan, emit, emitStep)
		log = redact(log)
		finishedAt := time.Now().UTC().Format(time.RFC3339)

		// Encrypt log with the sync key (fall back to plain if no key set).
		var logB64 string
		if cl := session; cl.SyncKey != "" {
			if key, derr := cloud.DecodeKey(cl.SyncKey); derr == nil {
				if blob, eerr := cloud.Encrypt(key, []byte(log)); eerr == nil {
					logB64 = base64.StdEncoding.EncodeToString(blob)
				}
			}
		}

		status := "ok"
		var errMsg string
		if runErr != nil {
			status = "error"
			errMsg = redact(runErr.Error())
		}
		_ = a.cloud.StoreDeploymentRun(opCtx, id, "apply", status, logB64, errMsg, startedAt, finishedAt)

		if runErr != nil {
			a.emitHostedDeployment(opCtx, errEvt, errMsg)
			return
		}
		a.emitHostedDeployment(opCtx, doneEvt, applyCfg.EffectiveDomain())
	}()

	return nil
}

// decodeDetection turns the loose map[string]any (from JSON-roundtripped
// deployment.config.detection) back into a typed DetectionResult so the
// plan builder can read .Installed without map gymnastics.
func decodeDetection(m map[string]any) *cloud.DetectionResult {
	out := &cloud.DetectionResult{Installed: map[string]string{}}
	if v, ok := m["os_family"].(string); ok {
		out.OSFamily = v
	}
	if v, ok := m["os_version"].(string); ok {
		out.OSVersion = v
	}
	if v, ok := m["arch"].(string); ok {
		out.Arch = v
	}
	if v, ok := m["installed"].(map[string]any); ok {
		for k, val := range v {
			if s, ok := val.(string); ok {
				out.Installed[k] = s
			}
		}
	}
	return out
}

// ActivityEntry is one parsed line from the remote auto-pull log
// (/tmp/laraenv-<name>.log on the VPS). Newest first when returned.
type ActivityEntry struct {
	At     string `json:"at"`     // RFC3339 from the autopull log
	Before string `json:"before"` // git SHA before the pull
	After  string `json:"after"`  // git SHA after the pull
}

// CloudDeploymentActivity opens an SSH session, tails the autopull log on
// the remote, parses each line into a typed entry, and returns the latest
// 200. Newest first. When the file doesn't exist (auto-deploy off, or
// cron hasn't fired yet), returns an empty slice — UI shows "no activity".
func (a *App) CloudDeploymentActivity(id int) ([]ActivityEntry, error) {
	opCtx, cancel, _, err := a.cloud.BeginSession(a.ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	dep, err := a.cloud.GetDeployment(opCtx, id)
	if err != nil {
		return nil, err
	}
	var host *config.SSHHost
	for _, h := range a.cfg.ListSSHHosts() {
		if h.Name == dep.SSHHostExternalID {
			h := h
			host = &h
			break
		}
	}
	if host == nil {
		return nil, i18n.Errorf("errors.app.sshHostNotFoundLocally", map[string]any{"host": dep.SSHHostExternalID})
	}

	sshManager, _, err := a.ssh.Snapshot(*host)
	if err != nil {
		return nil, err
	}
	client, err := sshManager.Dial(*host)
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.app.sshDialFailed", nil)
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.app.openSessionFailed", nil)
	}
	defer sess.Close()

	// safeName is unexported in cloud — replicate the simple cleanup here.
	logPath := "/tmp/laraenv-" + sanitizeName(dep.Name) + ".log"
	out, _ := sess.CombinedOutput("tail -n 200 " + shQuote(logPath) + " 2>/dev/null || true")

	// Parse: "[laraenv-autopull] 2025-...T...Z BEFORE -> AFTER"
	lines := strings.Split(string(out), "\n")
	entries := make([]ActivityEntry, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- { // newest first
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// Expect prefix "[laraenv-autopull] " then 3 space-separated tokens.
		const prefix = "[laraenv-autopull] "
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		body := strings.TrimPrefix(line, prefix)
		parts := strings.Fields(body)
		if len(parts) != 4 || parts[2] != "->" {
			continue
		}
		entries = append(entries, ActivityEntry{
			At:     parts[0],
			Before: parts[1],
			After:  parts[3],
		})
	}
	return entries, nil
}

// CloudDeploymentDestroy executes the inverse of Apply against the remote
// host: drops cron, vhost, certbot cert, MySQL schema, and the deploy
// directory. Streams the same step/log events as Apply (namespaced under
// `deployment:destroy:*`), then deletes the deployment row from the server.
//
// If the deployment was never Applied (no saved config), skips the SSH
// dance and just deletes the row.
//
// Async — returns immediately. UI subscribes to:
//
//	deployment:destroy:plan:<id>   payload: []deployStepInfo
//	deployment:destroy:step:<id>   payload: { idx, total, title, status }
//	deployment:destroy:log:<id>    payload: string (one log line)
//	deployment:destroy:done:<id>   payload: nil
//	deployment:destroy:error:<id>  payload: string (error message)
func (a *App) CloudDeploymentDestroy(id int) error {
	release, err := a.acquireDeploymentOperation()
	if err != nil {
		return err
	}
	opCtx, cancel, _, err := a.cloud.BeginSession(a.ctx)
	if err != nil {
		release()
		return err
	}
	launched := false
	defer func() {
		if !launched {
			release()
			cancel()
		}
	}()
	dep, err := a.cloud.GetDeployment(opCtx, id)
	if err != nil {
		return err
	}

	// No apply config means nothing was provisioned — fast-path delete.
	var applyCfg cloud.ApplyConfig
	hasConfig := false
	if dep.Config != nil {
		if a, ok := dep.Config["apply"].(map[string]any); ok && len(a) > 0 {
			b, _ := json.Marshal(a)
			if json.Unmarshal(b, &applyCfg) == nil && applyCfg.Name != "" {
				hasConfig = true
			}
		}
	}
	if !hasConfig {
		return a.cloud.DeleteDeployment(opCtx, id)
	}

	var host *config.SSHHost
	for _, h := range a.cfg.ListSSHHosts() {
		if h.Name == dep.SSHHostExternalID {
			h := h
			host = &h
			break
		}
	}
	if host == nil {
		return i18n.Errorf("errors.app.sshHostNotFoundCannotDestroy", map[string]any{"host": dep.SSHHostExternalID})
	}

	sshManager, _, err := a.ssh.Snapshot(*host)
	if err != nil {
		return err
	}
	logEvt := fmt.Sprintf("deployment:destroy:log:%d", id)
	stepEvt := fmt.Sprintf("deployment:destroy:step:%d", id)
	planEvt := fmt.Sprintf("deployment:destroy:plan:%d", id)
	doneEvt := fmt.Sprintf("deployment:destroy:done:%d", id)
	errEvt := fmt.Sprintf("deployment:destroy:error:%d", id)

	launched = true
	go func() {
		defer release()
		defer cancel()
		emit := func(line string) { a.emitHostedDeployment(opCtx, logEvt, line) }
		emitStep := func(idx, total int, title, status string) {
			a.emitHostedDeployment(opCtx, stepEvt, map[string]any{
				"idx": idx, "total": total, "title": title, "status": status,
			})
		}

		emit(i18n.T("backend.app.deployConnecting", map[string]any{"user": host.User, "address": host.Address, "port": host.Port}))
		client, err := sshManager.Dial(*host)
		if err != nil {
			a.emitHostedDeployment(opCtx, errEvt, err.Error())
			return
		}
		defer client.Close()

		plan := cloud.BuildDestroyPlan(applyCfg)
		a.emitHostedDeployment(opCtx, planEvt, deployStepInfos(plan))
		emit(i18n.T("backend.app.deployPlan", map[string]any{"count": len(plan)}))

		_, runErr := cloud.RunApply(client, plan, emit, emitStep)
		if runErr != nil {
			a.emitHostedDeployment(opCtx, errEvt, runErr.Error())
			return
		}

		// Remote scrubbed — now drop the server-side row.
		if err := a.cloud.DeleteDeployment(opCtx, id); err != nil {
			a.emitHostedDeployment(opCtx, errEvt, i18n.T("backend.app.remoteWipedServerDeleteFailed", map[string]any{"error": err}))
			return
		}

		emit("--- Deployment destroyed ---")
		a.emitHostedDeployment(opCtx, doneEvt, nil)
	}()

	return nil
}

// sanitizeName mirrors the (unexported) safeName helper in internal/cloud:
// strip every char that isn't [a-zA-Z0-9._-], replace with underscore.
// Used for deriving the autopull log filename from a deployment name.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// shQuote duplicates the (unexported) helper in internal/cloud for use in
// app.go's CloudDeploymentActivity SSH command.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CloudDeploymentDetect runs the detection probe pass against the SSH host
// linked to this deployment. Streams progress via "deployment:detect:log:<id>"
// events (per probe line) and emits a final "deployment:detect:done:<id>"
// (or :error:<id>) when finished. PATCHes the deployment with the detected
// fields and persists a run record on the server.
//
// Async — returns immediately. UI subscribes to the events.
func (a *App) CloudDeploymentDetect(id int) error {
	release, err := a.acquireDeploymentOperation()
	if err != nil {
		return err
	}
	opCtx, cancel, session, err := a.cloud.BeginSession(a.ctx)
	if err != nil {
		release()
		return err
	}
	launched := false
	defer func() {
		if !launched {
			release()
			cancel()
		}
	}()
	dep, err := a.cloud.GetDeployment(opCtx, id)
	if err != nil {
		return err
	}

	// Look up the SSH host config. The deployment stores the host's external_id
	// which equals the host's name (config.SSHHost.Name).
	var host *config.SSHHost
	for _, h := range a.cfg.ListSSHHosts() {
		if h.Name == dep.SSHHostExternalID {
			h := h
			host = &h
			break
		}
	}
	if host == nil {
		return i18n.Errorf("errors.app.sshHostNotFoundSyncOrRecreate", map[string]any{"host": dep.SSHHostExternalID})
	}

	sshManager, _, err := a.ssh.Snapshot(*host)
	if err != nil {
		return err
	}
	startedAt := time.Now().UTC().Format(time.RFC3339)
	logEvt := fmt.Sprintf("deployment:detect:log:%d", id)
	doneEvt := fmt.Sprintf("deployment:detect:done:%d", id)
	errEvt := fmt.Sprintf("deployment:detect:error:%d", id)

	launched = true
	go func() {
		defer release()
		defer cancel()
		emit := func(line string) { a.emitHostedDeployment(opCtx, logEvt, line) }

		emit(i18n.T("backend.app.deployConnecting", map[string]any{"user": host.User, "address": host.Address, "port": host.Port}))
		client, err := sshManager.Dial(*host)
		if err != nil {
			emit("ERR " + err.Error())
			a.emitHostedDeployment(opCtx, errEvt, err.Error())
			return
		}
		defer client.Close()
		emit("--- Connected. Running probes ---")

		result, err := cloud.RunDetection(client, emit)
		finishedAt := time.Now().UTC().Format(time.RFC3339)
		if err != nil {
			emit("ERR " + err.Error())
			a.emitHostedDeployment(opCtx, errEvt, err.Error())
			_ = a.cloud.StoreDeploymentRun(opCtx, id, "detect", "error", "", err.Error(), startedAt, finishedAt)
			return
		}

		// PATCH the deployment with detected fields. Plain text — these
		// are non-sensitive (OS family, kernel, list of installed package
		// names). The deploy config (passwords / paths / scripts) lives
		// in the encrypted resources table, not here.
		//
		// The server replaces the config blob wholesale, so the saved apply
		// config MUST ride along — without it, re-running Detect silently
		// erased preset/domain/git settings and the Deploy button vanished
		// (mirror of how Apply preserves the previous detection).
		cfgBlob := map[string]any{"detection": result}
		if dep.Config != nil {
			if ap, ok := dep.Config["apply"]; ok && ap != nil {
				cfgBlob["apply"] = ap
			}
		}
		patch := map[string]any{
			"os_family":  result.OSFamily,
			"os_version": result.OSVersion,
			"config":     cfgBlob,
		}
		if _, err := a.cloud.PatchDeployment(opCtx, id, patch); err != nil {
			emit("WARN: PATCH deployment failed: " + err.Error())
		}

		// Persist run + encrypted log (zero-knowledge — log can contain
		// hostnames/usernames that the user may not want server-side admins
		// to see). Falls back to plain log if no sync key is set.
		var logB64 string
		if cl := session; cl.SyncKey != "" {
			if key, derr := cloud.DecodeKey(cl.SyncKey); derr == nil {
				if blob, eerr := cloud.Encrypt(key, []byte(result.Log)); eerr == nil {
					logB64 = base64.StdEncoding.EncodeToString(blob)
				}
			}
		}
		if err := a.cloud.StoreDeploymentRun(opCtx, id, "detect", "ok", logB64, "", startedAt, finishedAt); err != nil {
			emit("WARN: storeRun failed: " + err.Error())
		}

		emit(i18n.T("backend.app.deployDetectionComplete", map[string]any{"osFamily": result.OSFamily, "osVersion": result.OSVersion, "count": len(result.Installed)}))
		a.emitHostedDeployment(opCtx, doneEvt, result)
	}()

	return nil
}

// ===== Deploy script presets =====

// presetScriptLookup resolves a preset ID (built-in or custom) to its script
// body. Passed into the cloud render functions so they stay config-store
// agnostic.
func (a *App) presetScriptLookup(id string) (string, bool) {
	p, ok := cloud.FindPreset(a.cfg, id)
	if !ok {
		return "", false
	}
	return p.Script, true
}

// ListDeployPresets returns built-ins (read-only, BuiltIn=true) followed by
// the user's custom presets sorted by name.
func (a *App) ListDeployPresets() []config.DeployPreset {
	out := cloud.BuiltinPresets()
	custom := a.cfg.ListDeployPresets()
	sort.Slice(custom, func(i, j int) bool {
		return strings.ToLower(custom[i].Name) < strings.ToLower(custom[j].Name)
	})
	return append(out, custom...)
}

// SaveDeployPreset upserts a custom preset. Empty ID = new (gets a UUID).
// Built-in IDs are rejected — duplicate them into a custom preset instead.
// Pushes to the cloud when sync is on.
func (a *App) SaveDeployPreset(p config.DeployPreset) (config.DeployPreset, error) {
	if strings.TrimSpace(p.Name) == "" {
		return p, i18n.Errorf("errors.app.presetNameRequired", nil)
	}
	if strings.HasPrefix(p.ID, "builtin:") {
		return p, i18n.Errorf("errors.app.builtinPresetReadOnly", nil)
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	p.BuiltIn = false
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := a.cfg.SetDeployPreset(p); err != nil {
		return p, err
	}
	wruntime.EventsEmit(a.ctx, "deploypresets:changed", "")
	a.maybeSyncPushPreset(p)
	return p, nil
}

// DeleteDeployPreset removes a custom preset locally and (best-effort) from
// the cloud. Deployments referencing it keep working until their next config
// edit — the render step reports "preset not found" at that point.
func (a *App) DeleteDeployPreset(id string) error {
	if strings.HasPrefix(id, "builtin:") {
		return i18n.Errorf("errors.app.builtinPresetNotDeletable", nil)
	}
	if err := a.cfg.RemoveDeployPreset(id); err != nil {
		return err
	}
	wruntime.EventsEmit(a.ctx, "deploypresets:changed", "")
	a.maybeSyncDeleteResource(cloud.KindDeployPreset, id)
	return nil
}

// maybeSyncPushPreset pushes one custom preset to the cloud in the background
// when sync is on. Best-effort — errors are logged, never surfaced to the UI.
// If the server doesn't know the deploy_preset kind yet, pushes fail silently
// and presets simply stay local-only.
func (a *App) maybeSyncPushPreset(p config.DeployPreset) {
	if !a.syncShouldRun() {
		return
	}
	opCtx, cancel, err := a.beginCloudSyncSession()
	if err != nil {
		return
	}
	go func() {
		defer cancel()
		defer a.reportCloudSyncStatus()
		p.BuiltIn = false // computed flag — never sync it
		b, err := json.Marshal(p)
		if err != nil {
			fmt.Printf("[cloud] marshal deploy_preset: %v\n", err)
			return
		}
		if err := a.cloud.PutResourceWithRetry(opCtx, cloud.KindDeployPreset, p.ID, b); err != nil {
			fmt.Printf("[cloud] push deploy_preset %s: %v\n", p.ID, err)
		}
	}()
}

// RenderDeployScriptPreview returns the exact script the given config would
// execute, so the UI can show it before anything runs. forDeploy=true renders
// the manual "Deploy now" variant (legacy configs get a pull prepended,
// LARAENV_FORCE set); false renders the pipeline as embedded in Apply and the
// autopull cron. Synchronous, no SSH.
func (a *App) RenderDeployScriptPreview(applyCfg cloud.ApplyConfig, forDeploy bool) (cloud.RenderedScript, error) {
	applyCfg.GitURL = cloud.NormalizeGitURL(applyCfg.GitURL)
	if forDeploy {
		return cloud.DeployNowScript(applyCfg, a.presetScriptLookup)
	}
	return cloud.RenderDeployPipeline(applyCfg, a.presetScriptLookup)
}

// decodeApplyConfig turns the loose map[string]any (JSON-roundtripped
// deployment.config.apply) back into a typed ApplyConfig. ok=false when the
// blob is absent or doesn't look like a saved config.
func decodeApplyConfig(m map[string]any) (cloud.ApplyConfig, bool) {
	var out cloud.ApplyConfig
	if len(m) == 0 {
		return out, false
	}
	b, err := json.Marshal(m)
	if err != nil {
		return out, false
	}
	if json.Unmarshal(b, &out) != nil || out.Name == "" {
		return out, false
	}
	return out, true
}

// CloudDeploymentDeploy runs the deploy pipeline (pull + conditional builds)
// against an already-provisioned deployment — the "second step" separate from
// Apply. No packages, vhosts or SSL are touched; just the rendered preset (or
// legacy build script with a pull prepended) inside the deploy root.
//
// Async — returns immediately. UI subscribes to:
//
//	deployment:deploy:plan:<id>   payload: []deployStepInfo
//	deployment:deploy:step:<id>   payload: { idx, total, title, status }
//	deployment:deploy:log:<id>    payload: string (one log line)
//	deployment:deploy:done:<id>   payload: effective domain
//	deployment:deploy:error:<id>  payload: string (error message)
func (a *App) CloudDeploymentDeploy(id int) error {
	release, err := a.acquireDeploymentOperation()
	if err != nil {
		return err
	}
	opCtx, cancel, session, err := a.cloud.BeginSession(a.ctx)
	if err != nil {
		release()
		return err
	}
	launched := false
	defer func() {
		if !launched {
			release()
			cancel()
		}
	}()
	dep, err := a.cloud.GetDeployment(opCtx, id)
	if err != nil {
		return err
	}

	var applyCfg cloud.ApplyConfig
	ok := false
	if dep.Config != nil {
		if m, isMap := dep.Config["apply"].(map[string]any); isMap {
			applyCfg, ok = decodeApplyConfig(m)
		}
	}
	if !ok {
		return i18n.Errorf("errors.app.deploymentNoSavedConfig", nil)
	}

	rendered, err := cloud.DeployNowScript(applyCfg, a.presetScriptLookup)
	if err != nil {
		return err
	}
	if rendered.Script == "" {
		return i18n.Errorf("errors.app.noDeployScriptConfigured", nil)
	}

	var host *config.SSHHost
	for _, h := range a.cfg.ListSSHHosts() {
		if h.Name == dep.SSHHostExternalID {
			h := h
			host = &h
			break
		}
	}
	if host == nil {
		return i18n.Errorf("errors.app.sshHostNotFoundLocally", map[string]any{"host": dep.SSHHostExternalID})
	}

	sshManager, _, err := a.ssh.Snapshot(*host)
	if err != nil {
		return err
	}
	startedAt := time.Now().UTC().Format(time.RFC3339)
	logEvt := fmt.Sprintf("deployment:deploy:log:%d", id)
	stepEvt := fmt.Sprintf("deployment:deploy:step:%d", id)
	planEvt := fmt.Sprintf("deployment:deploy:plan:%d", id)
	doneEvt := fmt.Sprintf("deployment:deploy:done:%d", id)
	errEvt := fmt.Sprintf("deployment:deploy:error:%d", id)

	launched = true
	go func() {
		defer release()
		defer cancel()
		emit := func(line string) { a.emitHostedDeployment(opCtx, logEvt, line) }
		emitStep := func(idx, total int, title, status string) {
			a.emitHostedDeployment(opCtx, stepEvt, map[string]any{
				"idx": idx, "total": total, "title": title, "status": status,
			})
		}

		storeRun := func(status, logB64, errMsg string) {
			finishedAt := time.Now().UTC().Format(time.RFC3339)
			// The server's run-kind enum may predate "deploy" — fall back to
			// "apply" once so the run history isn't silently lost.
			if err := a.cloud.StoreDeploymentRun(opCtx, id, "deploy", status, logB64, errMsg, startedAt, finishedAt); err != nil {
				_ = a.cloud.StoreDeploymentRun(opCtx, id, "apply", status, logB64, errMsg, startedAt, finishedAt)
			}
		}

		emit(i18n.T("backend.app.deployConnecting", map[string]any{"user": host.User, "address": host.Address, "port": host.Port}))
		client, err := sshManager.Dial(*host)
		if err != nil {
			emit("ERR " + err.Error())
			a.emitHostedDeployment(opCtx, errEvt, err.Error())
			storeRun("error", "", err.Error())
			return
		}
		defer client.Close()

		plan := []cloud.Step{{
			Title:     "Deploy " + applyCfg.Name,
			TitleKey:  "deployments.steps.deployProject",
			TitleVars: map[string]any{"name": applyCfg.Name},
			Cmd:       rendered.Script,
		}}
		a.emitHostedDeployment(opCtx, planEvt, deployStepInfos(plan))

		log, runErr := cloud.RunApply(client, plan, emit, emitStep)

		// Encrypt log with the sync key (fall back to plain if no key set).
		var logB64 string
		if cl := session; cl.SyncKey != "" {
			if key, derr := cloud.DecodeKey(cl.SyncKey); derr == nil {
				if blob, eerr := cloud.Encrypt(key, []byte(log)); eerr == nil {
					logB64 = base64.StdEncoding.EncodeToString(blob)
				}
			}
		}

		if runErr != nil {
			storeRun("error", logB64, runErr.Error())
			a.emitHostedDeployment(opCtx, errEvt, runErr.Error())
			return
		}
		storeRun("ok", logB64, "")
		a.emitHostedDeployment(opCtx, doneEvt, applyCfg.EffectiveDomain())
	}()

	return nil
}

// CloudPortal opens the Stripe Customer Portal so the user can cancel,
// update their card, or change plan.
func (a *App) CloudPortal() error { return a.cloud.StartPortal(a.ctx) }

// CloudSetSyncPassphrase derives the AES-256 sync key from the given
// passphrase + the user's kdf_salt. Used by GitHub-OAuth accounts that
// don't have a password — they pick a passphrase here, separate from
// their GitHub credential. WARNING: forgetting it means the synced data
// is unrecoverable. Frontend must surface that clearly.
func (a *App) CloudSetSyncPassphrase(passphrase string) error {
	return a.cloud.PrimeKey(passphrase)
}

// CloudRekeySync re-encrypts ALL synced resources on the server under a key
// derived from the given account password (Argon2id with the account's
// kdf_salt), then adopts that key locally. Run this on the machine that can
// still decrypt the data (it holds the working key); afterwards any machine
// whose key already matches the account password decrypts everything.
//
// This is the recovery path for accounts that originally encrypted with a
// GitHub-era sync passphrase and later added an email/password login — the
// password-derived key differs from the passphrase-derived one. Returns a
// human-readable summary for the UI toast.
func (a *App) CloudRekeySync(accountPassword string) (string, error) {
	opCtx, cancel, cl, err := a.cloud.BeginSession(a.ctx)
	if err != nil {
		return "", err
	}
	defer cancel()
	defer a.reportCloudSyncStatus()
	if cl.KdfSalt == "" {
		return "", i18n.Errorf("errors.app.notLoggedInSignInFirst", nil)
	}
	newKey, err := cloud.DeriveKey(accountPassword, cl.KdfSalt)
	if err != nil {
		return "", err
	}
	rekeyed, skipped, err := a.cloud.RekeyResources(opCtx, newKey)
	if err != nil {
		return "", err
	}
	// Local data may now be stale relative to the freshly re-keyed server copy;
	// nudge listeners to refresh.
	wruntime.EventsEmit(a.ctx, "ssh:hosts-changed", "")
	msg := i18n.T("backend.app.rekeyedItems", map[string]any{"count": rekeyed})
	if skipped > 0 {
		msg += i18n.T("backend.app.rekeySkipped", map[string]any{"count": skipped})
	}
	return msg, nil
}

// CloudSyncStatus is the snapshot the Account UI uses to render the Sync
// section. Cheap, no network call.
func (a *App) CloudSyncStatus() cloud.SyncStatus {
	st := a.cloud.SyncStatusSnapshot()
	st.ResourceCount = len(a.cfg.ListSSHHosts()) // for now — just SSH host count
	return st
}

// CloudSetSyncEnabled flips the local sync toggle. Doesn't trigger a sync
// pass on its own — call CloudSyncNow afterwards.
func (a *App) CloudSetSyncEnabled(enabled bool) error {
	return a.cloud.SetSyncEnabled(enabled)
}

// CloudSyncNow performs a full push (every local SSH host) + pull (every
// remote SSH host applied locally). Returns a summary string for the UI toast.
func (a *App) CloudSyncNow() (string, error) {
	if !a.syncShouldRun() {
		// More specific reason for the UI.
		cl := a.cfg.Get().Cloud
		switch {
		case !cl.Enabled:
			return "", cloud.ErrDisabled
		case cl.Token == "":
			return "", i18n.Errorf("errors.app.notLoggedIn", nil)
		case cl.Plan != "pro":
			return "", i18n.Errorf("errors.app.syncRequiresProPlan", nil)
		case cl.SyncKey == "":
			return "", i18n.Errorf("errors.app.noSyncKeySignInAgain", nil)
		case !cl.SyncEnabled:
			return "", i18n.Errorf("errors.app.syncDisabled", nil)
		}
	}

	opCtx, cancel, err := a.beginCloudSyncSession()
	if err != nil {
		return "", err
	}
	defer cancel()
	defer a.reportCloudSyncStatus()
	// PUSH every local SSH host.
	pushed := 0
	skippedPasswords := 0
	for _, h := range a.cfg.ListSSHHosts() {
		if h.PasswordUnavailable {
			skippedPasswords++
			continue
		}
		b, err := json.Marshal(h)
		if err != nil {
			continue
		}
		if err := a.cloud.PutResourceWithRetry(opCtx, cloud.KindSSHHost, h.Name, b); err != nil {
			fmt.Printf("[cloud] push ssh_host %s: %v\n", h.Name, err)
			continue
		}
		pushed++
	}

	// Accumulates an actionable message when the server holds items this
	// machine can't decrypt (passphrase mismatch). Non-fatal: we still apply
	// whatever did decrypt.
	var syncWarning string
	if skippedPasswords > 0 {
		syncWarning = i18n.T("backend.app.syncSSHPasswordsUnavailable", map[string]any{"count": skippedPasswords})
	}

	// PULL all remote SSH hosts and apply to local config.
	remotes, err := a.cloud.PullResources(opCtx, cloud.KindSSHHost)
	if err != nil {
		if cloud.IsUndecryptable(err) {
			if syncWarning != "" {
				syncWarning += "\n"
			}
			syncWarning += err.Error()
		} else {
			return "", err
		}
	}
	pulled := 0
	for _, r := range remotes {
		if err := opCtx.Err(); err != nil {
			return "", err
		}
		var h config.SSHHost
		if err := json.Unmarshal(r.Payload, &h); err != nil {
			fmt.Printf("[cloud] unmarshal %s: %v\n", r.ExternalID, err)
			continue
		}
		if err := a.cfg.SetSSHHost(h); err != nil {
			fmt.Printf("[cloud] apply ssh_host %s: %v\n", r.ExternalID, err)
			continue
		}
		pulled++
	}
	if pulled > 0 {
		wruntime.EventsEmit(a.ctx, "ssh:hosts-changed", "")
	}

	// PUSH every local cron job (config only — strip runtime state).
	pushedCrons := 0
	for _, j := range a.crons.List() {
		clean := j
		clean.LastRun = time.Time{}
		clean.LastStatus = ""
		clean.LastOutput = ""
		clean.LastError = ""
		clean.NextRun = time.Time{}
		b, err := json.Marshal(clean)
		if err != nil {
			continue
		}
		if err := a.cloud.PutResourceWithRetry(opCtx, cloud.KindCron, j.ID, b); err != nil {
			fmt.Printf("[cloud] push cron %s: %v\n", j.ID, err)
			continue
		}
		pushedCrons++
	}

	// PULL remote crons.
	remoteCrons, err := a.cloud.PullResources(opCtx, cloud.KindCron)
	if err != nil {
		if cloud.IsUndecryptable(err) {
			if syncWarning == "" {
				syncWarning = err.Error()
			}
		} else {
			return "", err
		}
	}
	pulledCrons := 0
	for _, r := range remoteCrons {
		if err := opCtx.Err(); err != nil {
			return "", err
		}
		var j pkgcron.Job
		if err := json.Unmarshal(r.Payload, &j); err != nil {
			fmt.Printf("[cloud] unmarshal cron %s: %v\n", r.ExternalID, err)
			continue
		}
		if _, err := a.crons.Save(j); err != nil {
			fmt.Printf("[cloud] apply cron %s: %v\n", r.ExternalID, err)
			continue
		}
		pulledCrons++
	}

	// PUSH every local custom deploy preset. Best-effort: a server that
	// doesn't know the deploy_preset kind yet rejects these — presets then
	// simply stay local-only.
	pushedPresets := 0
	for _, p := range a.cfg.ListDeployPresets() {
		p.BuiltIn = false
		b, err := json.Marshal(p)
		if err != nil {
			continue
		}
		if err := a.cloud.PutResourceWithRetry(opCtx, cloud.KindDeployPreset, p.ID, b); err != nil {
			fmt.Printf("[cloud] push deploy_preset %s: %v\n", p.ID, err)
			continue
		}
		pushedPresets++
	}

	// PULL remote deploy presets.
	pulledPresets := 0
	remotePresets, err := a.cloud.PullResources(opCtx, cloud.KindDeployPreset)
	if err != nil {
		if cloud.IsUndecryptable(err) {
			if syncWarning == "" {
				syncWarning = err.Error()
			}
		} else {
			fmt.Printf("[cloud] pull deploy_presets: %v\n", err)
		}
	}
	{
		for _, r := range remotePresets {
			if err := opCtx.Err(); err != nil {
				return "", err
			}
			var p config.DeployPreset
			if err := json.Unmarshal(r.Payload, &p); err != nil {
				fmt.Printf("[cloud] unmarshal deploy_preset %s: %v\n", r.ExternalID, err)
				continue
			}
			if err := a.cfg.SetDeployPreset(p); err != nil {
				fmt.Printf("[cloud] apply deploy_preset %s: %v\n", r.ExternalID, err)
				continue
			}
			pulledPresets++
		}
		if pulledPresets > 0 {
			wruntime.EventsEmit(a.ctx, "deploypresets:changed", "")
		}
	}

	summary := i18n.T("backend.app.cloudSyncSummary", map[string]any{
		"pushed": pushed, "pushedCrons": pushedCrons, "pushedPresets": pushedPresets,
		"pulled": pulled, "pulledCrons": pulledCrons, "pulledPresets": pulledPresets,
	})
	if syncWarning != "" {
		// syncWarning itself is already translated: it comes from
		// cloud.UndecryptableError.Error(), which renders via i18n.T. The "⚠ "
		// glyph prefix is not prose, so it stays a plain Go literal.
		summary += "\n⚠ " + syncWarning
	}
	return summary, nil
}

// ===== Cron =====

// ListCrons returns all scheduled jobs sorted by project then name.
func (a *App) ListCrons() []pkgcron.Job { return a.crons.List() }

// SaveCron upserts a cron job. Empty ID = new (gets a UUID); non-empty =
// update. Returns the stored job (with assigned ID) on success.
// If sync is enabled, also pushes the new state to the cloud.
func (a *App) SaveCron(j pkgcron.Job) (pkgcron.Job, error) {
	saved, err := a.crons.Save(j)
	if err != nil {
		return saved, err
	}
	a.maybeSyncPushCron(saved)
	return saved, nil
}

// DeleteCron removes a job and unregisters it from the engine.
func (a *App) DeleteCron(id string) error {
	if err := a.crons.Remove(id); err != nil {
		return err
	}
	a.maybeSyncDeleteResource(cloud.KindCron, id)
	return nil
}

// ToggleCron enables/disables a job without recreating it.
func (a *App) ToggleCron(id string, enabled bool) error {
	if err := a.crons.Toggle(id, enabled); err != nil {
		return err
	}
	// Push the updated job state. Find it by id in the current list.
	for _, j := range a.crons.List() {
		if j.ID == id {
			a.maybeSyncPushCron(j)
			break
		}
	}
	return nil
}

// maybeSyncPushCron pushes one cron job to the cloud as an encrypted resource
// when sync is on. Strips runtime state (LastRun/Status/Output/NextRun) so
// the second machine doesn't see "last run from your other laptop" — only
// the user-edited config (schedule, command, project, etc.).
func (a *App) maybeSyncPushCron(j pkgcron.Job) {
	if !a.syncShouldRun() {
		return
	}
	opCtx, cancel, err := a.beginCloudSyncSession()
	if err != nil {
		return
	}
	go func() {
		defer cancel()
		defer a.reportCloudSyncStatus()
		clean := j
		clean.LastRun = time.Time{}
		clean.LastStatus = ""
		clean.LastOutput = ""
		clean.LastError = ""
		clean.NextRun = time.Time{}
		b, err := json.Marshal(clean)
		if err != nil {
			fmt.Printf("[cloud] marshal cron: %v\n", err)
			return
		}
		if err := a.cloud.PutResourceWithRetry(opCtx, cloud.KindCron, j.ID, b); err != nil {
			fmt.Printf("[cloud] push cron %s: %v\n", j.ID, err)
		}
	}()
}

// RunCronNow executes a job out-of-band (does not affect the schedule).
// Useful for the UI's "Run now" button.
func (a *App) RunCronNow(id string) error { return a.crons.RunNow(id) }

func sortHostsByName(hs []config.SSHHost) {
	for i := 1; i < len(hs); i++ {
		for j := i; j > 0 && hs[j-1].Name > hs[j].Name; j-- {
			hs[j-1], hs[j] = hs[j], hs[j-1]
		}
	}
}

// languageOverrides is what paths.Root()/langs/*.json contributed at
// startup (see LoadCatalogues): the folder's path, the accepted fragment per
// locale for GetLanguageOverrides to hand the frontend, and what got applied
// or rejected per locale for the Settings status line. Zero value means the
// folder doesn't exist or contributed nothing, which is the case for almost
// every user. Written once by LoadCatalogues before NewApp runs, and never
// mutated afterwards (rule 2: load at startup only, no watcher) — safe to
// read from any goroutine without further synchronization.
var languageOverrides LanguageOverridesPayload

// LanguageOverridesPayload is the bridge shape GetLanguageOverrides returns.
type LanguageOverridesPayload struct {
	// Dir is the on-disk overrides folder's absolute path, or "" if it
	// contributed nothing. The Settings screen shows "Using local
	// translations from {{path}}" only when this is non-empty.
	Dir string `json:"dir"`
	// Catalogues holds, per locale that contributed at least one key, a
	// fragment document containing only the accepted overridden keys (plus
	// $meta). I18nProvider overlays these onto what it bundled at build
	// time, since its Vite glob only sees files that existed at build time.
	Catalogues map[string]map[string]any `json:"catalogues"`
	// Results names what each contributing locale applied or rejected, the
	// latter naming the key and why — a translator whose edit to a
	// do-not-translate key was silently ignored needs to see that.
	Results []i18n.OverrideResult `json:"results"`
}

// LoadCatalogues parses the embedded langs/*.json, merges in any override at
// paths.Root()/langs/*.json, and installs the result as the package-level
// i18n.Global() set. A catalogue that will not parse is fatal at startup
// rather than a screen full of key paths later — but a disk override that
// fails to parse is not: see MergeOverrides's doc comment for why a
// translator's momentarily-invalid file must not be able to take the app
// down.
//
// This is a package-level function, not an *App method, and main.go calls it
// BEFORE NewApp() — deliberately. NewApp constructs several packages
// (config, agents, notifications, projectroutes) that can hit an
// i18n.Errorf/i18n.T call while still inside NewApp's own synchronous body
// (a corrupt-file recovery path, a rename failure) via panic(err) or a
// stored notice message. i18n.T renders eagerly, so if Global() were still
// nil at that point, a bare key path would either get baked permanently into
// a stored string (e.g. a StoreNotice a user reads long after catalogues
// loaded) or reach a panic message nobody can read. Loading first closes
// that window entirely, including for code written after this comment. The
// override merge happens inside this same window, before SetGlobal — never
// after, since a published *Set is treated as immutable.
func LoadCatalogues(fsys fs.FS) {
	set, err := i18n.Load(fsys, "langs")
	if err != nil {
		panic(fmt.Sprintf("load translation catalogues: %v", err))
	}
	manifest := i18n.LoadManifest(fsys, "langs")
	merged, fragments, results := i18n.MergeOverrides(set, os.DirFS(paths.Root()), "langs", manifest)
	// fragments and results are nil, not empty, when nothing on disk
	// contributed — the common case for almost every user. Normalise both
	// to non-nil here so GetLanguageOverrides always marshals honest JSON
	// (`{}`/`[]`) instead of `null`, matching the same reasoning applied to
	// OverrideResult.Rejected in MergeOverrides: the frontend must never
	// have to guess whether a bridge collection is present before indexing
	// or iterating it.
	if fragments == nil {
		fragments = map[string]map[string]any{}
	}
	if results == nil {
		results = []i18n.OverrideResult{}
	}
	languageOverrides = LanguageOverridesPayload{
		Catalogues: fragments,
		Results:    results,
	}
	if len(results) > 0 {
		languageOverrides.Dir = filepath.Join(paths.Root(), "langs")
	}
	i18n.SetGlobal(merged)
}

// GetLanguageOverrides is the bridge accessor behind the frontend's disk
// override layer and the Settings status line: see LanguageOverridesPayload
// and LoadCatalogues, which computes this once at startup.
func (a *App) GetLanguageOverrides() LanguageOverridesPayload {
	return languageOverrides
}

// T renders a catalogue key in the active language. Backend-side counterpart
// of the frontend's t().
func (a *App) T(key string, vars map[string]any) string {
	return i18n.Global().T(a.GetLanguage(), key, vars)
}

// resolveLanguage picks the UI language from persisted config, falling back
// to matching the OS locale against what shipped. Shared by GetLanguage and
// NewApp (which needs it before an *App exists to call GetLanguage on).
func resolveLanguage(cfg *config.Store) string {
	if code := cfg.Language(); code != "" {
		return code
	}
	return i18n.Match(i18n.OSLocale(), i18n.Global().Locales())
}

// GetLanguage is the active UI language. On first run nothing is stored, so we
// ask Windows and match against what shipped.
func (a *App) GetLanguage() string { return resolveLanguage(a.cfg) }

// ListLocales is every catalogue that shipped, for the Settings selector.
func (a *App) ListLocales() []i18n.Meta { return i18n.Global().Locales() }

// SetLanguage persists the choice, relabels the tray, and tells this window.
// Other LaraEnv processes are separate executables and pick the change up from
// config.json through watchLanguage.
func (a *App) SetLanguage(code string) error {
	if !a.hasLocale(code) {
		return i18n.Errorf("errors.app.unknownLanguage", map[string]any{"code": code})
	}
	if err := a.cfg.SetLanguage(code); err != nil {
		return err
	}
	i18n.SetActive(code)
	a.relabelTray()
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "language:changed", code)
	}
	return nil
}

func (a *App) hasLocale(code string) bool {
	for _, m := range i18n.Global().Locales() {
		if m.Code == code {
			return true
		}
	}
	return false
}

// watchLanguage mirrors a language change made by another LaraEnv process into
// this one's window. Started once per process from startup.
func (a *App) watchLanguage() {
	a.cfg.Watch(a.ctx, func(c config.Config) {
		code := c.Language
		if code == "" {
			code = i18n.Match(i18n.OSLocale(), i18n.Global().Locales())
		}
		i18n.SetActive(code)
		a.relabelTray()
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "language:changed", code)
		}
	})
}

// relabelTray re-applies the tray's titles from the catalogue. The tray
// already has its labels accessor from construction (Callbacks.Labels); only
// Relabel needs calling on a language change.
func (a *App) relabelTray() {
	tray.Relabel()
}

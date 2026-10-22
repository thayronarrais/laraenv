package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"lara4ever/internal/config"
	"lara4ever/internal/i18n"
	"lara4ever/internal/paths"
	"lara4ever/internal/vcruntime"
)

// Definition describes how to launch a service binary.
type Definition struct {
	Name    string
	Label   string
	Path    string
	Args    []string
	WorkDir string // override default (= dir of Path)
	// Deps are auxiliary services to start/stop alongside this one
	// (e.g. "nginx" depends on "php-fpm").
	Deps []string
	// Internal services don't appear in the user-facing list; they are only
	// started indirectly as deps of another service.
	Internal bool
	// FirstRunCheck returns true when first-run initialization is needed
	// (e.g. MySQL data dir missing). If true, FirstRunInit is invoked once
	// before the normal Start.
	FirstRunCheck func() bool
	FirstRunInit  func() error
	// Env carries extra environment variables to inject when spawning. The
	// process inherits the parent env on top of these. Used to set
	// PHP_FCGI_MAX_REQUESTS=0 on php-cgi so it doesn't self-exit after 500
	// requests (which would otherwise look like a random crash to the user).
	Env map[string]string
	// DropAdminToken spawns the process under a restricted token with the
	// Administrators group disabled. Required by postgres.exe, which refuses
	// to run elevated — and the app itself always runs elevated
	// (requireAdministrator manifest). No-op on non-Windows.
	DropAdminToken bool
	// Replicas, when >1, spawns that many processes under the same logical
	// service name. Each replica is tracked internally as "<Name>#<i>" but the
	// logical Name stays the dep target and status identity. Used to run a pool
	// of php-cgi workers so a single long request (large upload) doesn't block
	// the whole site — php-cgi in FastCGI mode handles one request at a time and
	// doesn't fork children on Windows.
	Replicas int
	// ReplicaArgs returns the args for replica i; when set it overrides Args per
	// replica (e.g. to bind each php-cgi worker to its own port). Only consulted
	// when Replicas > 1.
	ReplicaArgs func(i int) []string
	// Exclusive groups services that cannot run at the same time. Web
	// servers all bind :80, so nginx, apache and caddy share the group
	// "web": starting one while another runs fails with a message naming
	// the conflict, instead of failing at the socket and showing as stopped
	// for no visible reason.
	Exclusive string
	// RedistURL names the Visual C++ Redistributable this binary needs, so
	// vcruntime.Check can point at the right one instead of always assuming
	// the app-wide default (vcruntime.RedistURL). Empty means the default
	// applies. The registrar sets this per-Definition because it's the one
	// that knows which binary/toolchain Path actually is (e.g. a php-fpm
	// def for a vs18 PHP series sets this to vcruntime.RedistURLVS18).
	RedistURL string
}

type ErrorEvent struct {
	Service string `json:"service"`
	Message string `json:"message"`
	// RedistURL carries the resolved Visual C++ redistributable URL when
	// Message came from a vcruntime.Check failure (vcruntime.URLFromError
	// on the underlying error) — "" otherwise. Lets the frontend show the
	// exact redistributable this binary needs instead of parsing Message.
	RedistURL string `json:"redistUrl,omitempty"`
}

type Manager struct {
	lifecycle sync.Mutex // serialize initialization/start/stop across GUI and MCP
	mu        sync.Mutex
	procs     map[string]*exec.Cmd
	procArgs  map[string][]string // args each tracked proc/replica was started with, for respawn
	defs      map[string]Definition
	startedAt map[string]time.Time
	last      map[string]string
	logs      map[string]*logRing // captured stdout/stderr tail per service name
	// stopping marks services the user (or shutdown) explicitly asked to stop,
	// so the wait goroutine skips auto-restart for that exit.
	stopping map[string]bool
	// failStreak counts consecutive short-lived (<30s) auto-restart attempts,
	// reset whenever a respawned process survives long enough. Used to bail
	// out of restart loops on a fundamentally broken binary.
	failStreak map[string]int
	onError    func(ErrorEvent)
	cfg        *config.Store
}

func NewManager(cfg *config.Store) *Manager {
	defs := map[string]Definition{
		"nginx": {
			Name:      "nginx",
			Label:     "Nginx",
			Path:      paths.NginxExe(),
			Exclusive: "web",
		},
		"apache": {
			Name:      "apache",
			Label:     "Apache",
			Path:      paths.ApacheExe(),
			Exclusive: "web",
		},
		"caddy": {
			Name:      "caddy",
			Label:     "Caddy",
			Path:      paths.CaddyExe(),
			Args:      []string{"run", "--config", paths.CaddyFile(), "--adapter", "caddyfile"},
			Exclusive: "web",
		},
		"mysql": {
			Name: "mysql", Label: "MySQL",
			Path: paths.MySQLD(),
			Args: []string{
				"--basedir=" + paths.MySQLRoot(),
				"--datadir=" + paths.MySQLData(),
				"--console",
			},
			FirstRunCheck: func() bool {
				_, err := os.Stat(filepath.Join(paths.MySQLData(), "mysql"))
				return err != nil
			},
			FirstRunInit: func() error {
				return runOnce(
					paths.MySQLD(),
					[]string{"--initialize-insecure", "--datadir=" + paths.MySQLData()},
					paths.MySQLRoot(),
					60*time.Second,
				)
			},
		},
		"postgres": {
			Name: "postgres", Label: "PostgreSQL",
			Path:           paths.PostgresExe(),
			Args:           []string{"-D", paths.PostgresData()},
			DropAdminToken: true,
			FirstRunCheck: func() bool {
				_, err := os.Stat(filepath.Join(paths.PostgresData(), "PG_VERSION"))
				return err != nil
			},
			FirstRunInit: func() error {
				return runOnce(
					paths.InitDB(),
					[]string{"-D", paths.PostgresData(), "-U", "postgres", "--auth=trust", "--encoding=UTF8"},
					paths.PostgresRoot(),
					60*time.Second,
				)
			},
		},
		"mailpit": {
			Name: "mailpit", Label: "Mailpit",
			Path: paths.MailpitExe(),
		},
		"redis": {
			Name: "redis", Label: "Redis",
			Path: paths.RedisExe(),
			Args: []string{paths.RedisConf()},
			FirstRunCheck: func() bool {
				_, err := os.Stat(paths.RedisConf())
				return err != nil
			},
			FirstRunInit: func() error {
				if err := os.MkdirAll(paths.RedisData(), 0755); err != nil {
					return err
				}
				escaped := strings.ReplaceAll(paths.RedisData(), `\`, `\\`)
				conf := "port 6379\nbind 127.0.0.1\ndir " + escaped + "\nappendonly no\n"
				return os.WriteFile(paths.RedisConf(), []byte(conf), 0644)
			},
		},
		// php-fpm definitions são adicionadas dinamicamente em app.go
		// (uma por versão de PHP instalada).
	}
	return &Manager{
		procs:      make(map[string]*exec.Cmd),
		procArgs:   make(map[string][]string),
		defs:       defs,
		startedAt:  make(map[string]time.Time),
		last:       make(map[string]string),
		logs:       make(map[string]*logRing),
		stopping:   make(map[string]bool),
		failStreak: make(map[string]int),
		cfg:        cfg,
	}
}

func (m *Manager) SetErrorHandler(f func(ErrorEvent)) { m.onError = f }

// RegisterDef adds or overwrites a service definition. Used by app.go to
// register one PHP-FPM definition per installed PHP version.
func (m *Manager) RegisterDef(d Definition) {
	m.mu.Lock()
	m.defs[d.Name] = d
	m.mu.Unlock()
}

func (m *Manager) definition(name string) (Definition, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.defs[name]
	d.Deps = append([]string(nil), d.Deps...)
	return d, ok
}

// SetDeps replaces the dependency list of an existing service.
// Used to wire web servers (nginx/apache) to all installed php-fpm versions.
func (m *Manager) SetDeps(name string, deps []string) {
	m.mu.Lock()
	if d, ok := m.defs[name]; ok {
		d.Deps = deps
		m.defs[name] = d
	}
	m.mu.Unlock()
}

// Active returns services the user has enabled on the Dashboard (excluding
// internal-only services like php-fpm).
func (m *Manager) Active() []Definition {
	enabled := m.cfg.Get().Stack.Enabled
	m.mu.Lock()
	var out []Definition
	for name, d := range m.defs {
		if d.Internal {
			continue
		}
		if enabled[name] {
			out = append(out, d)
		}
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *Manager) ActiveNames() []string {
	a := m.Active()
	out := make([]string, len(a))
	for i, d := range a {
		out[i] = d.Name
	}
	return out
}

func (m *Manager) Start(name string) error {
	return m.StartContext(context.Background(), name)
}

// StartContext limits the startup operation, not the lifetime of services that
// successfully started. Cancelled partial starts roll back only their new PIDs.
func (m *Manager) StartContext(ctx context.Context, name string) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	started := map[string]*exec.Cmd{}
	defer func() {
		if ctx.Err() != nil {
			m.rollbackStartedLocked(started)
			err = ctx.Err()
		}
	}()
	def, ok := m.definition(name)
	if !ok {
		return i18n.Errorf("errors.services.unknownService", map[string]any{"name": name})
	}
	if err := m.conflictFor(name); err != nil {
		return err
	}
	for _, dep := range def.Deps {
		if err := m.startOneLockedContext(ctx, dep, started); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return i18n.Wrapf(err, "errors.services.depFailed", map[string]any{"dep": dep})
		}
	}
	return m.startOneLockedContext(ctx, name, started)
}

// conflictFor reports the running service that blocks name from starting.
func (m *Manager) conflictFor(name string) error {
	def, ok := m.definition(name)
	if !ok || def.Exclusive == "" {
		return nil
	}
	for _, running := range m.RunningNames() {
		if running == name {
			continue
		}
		other, ok := m.definition(running)
		if ok && other.Exclusive == def.Exclusive {
			return i18n.Errorf("errors.services.exclusiveConflict", map[string]any{"label": other.Label})
		}
	}
	return nil
}

func (m *Manager) Stop(name string) error {
	def, ok := m.definition(name)
	if !ok {
		return i18n.Errorf("errors.services.unknownService", map[string]any{"name": name})
	}
	if err := m.stopOne(name); err != nil {
		return err
	}
	for _, dep := range def.Deps {
		_ = m.stopOne(dep)
	}
	return nil
}

func (m *Manager) Reload(name string) error {
	if err := m.Stop(name); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	return m.Start(name)
}

// Restart bounces ONLY the named service, leaving its Deps untouched. Vhost
// changes need the web server to re-read config but must not stop/start every
// php-fpm pool along the way — Reload's dep cascade did exactly that, so a
// startup sweep over N projects bounced nginx and all FPM workers N times
// (the "services flapping RUNNING/STOPPED for two minutes" symptom).
func (m *Manager) Restart(name string) error {
	return m.RestartContext(context.Background(), name)
}

// RestartContext bounces only the named service, retaining its dependencies.
// Cancellation after stopping restores a previously running service before
// returning, so a cancelled config repair does not turn off shared sites.
func (m *Manager) RestartContext(ctx context.Context, name string) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	wasRunning := m.Status(name) == "running"
	stopIssued := false
	started := map[string]*exec.Cmd{}
	defer func() {
		if ctxErr := ctx.Err(); ctxErr != nil {
			m.rollbackStartedLocked(started)
			err = ctxErr
			if wasRunning && stopIssued {
				if restoreErr := m.restoreStoppedLocked(name); restoreErr != nil {
					err = fmt.Errorf("%w: restoring %s failed: %v", ctxErr, name, restoreErr)
				}
			}
		}
	}()
	if err := m.stopOneLocked(name); err != nil {
		return err
	}
	stopIssued = true
	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	if err := m.waitStoppedContext(ctx, name); err != nil {
		return err
	}
	return m.startOneLockedContext(ctx, name, started)
}

// Caller holds lifecycle, preventing a supervisor from replacing the stopped
// process while its wait goroutine finishes reaping it.
func (m *Manager) waitStoppedContext(ctx context.Context, name string) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if m.Status(name) == "stopped" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) restoreStoppedLocked(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.waitStoppedContext(ctx, name); err != nil {
		return err
	}
	// Keep restoration ownership separate from the cancelled operation's map:
	// its deferred rollback must not kill the service we just restored.
	restored := map[string]*exec.Cmd{}
	err := m.startOneLockedContext(ctx, name, restored)
	if ctx.Err() != nil {
		m.rollbackStartedLocked(restored)
		return ctx.Err()
	}
	return err
}

func (m *Manager) Status(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isRunningLocked(name) {
		return "running"
	}
	return "stopped"
}

// isRunningLocked reports whether the logical service `name` has at least one
// live process: either a direct match (single-process services) or any replica
// keyed "<name>#<i>" (pooled services like php-fpm). Caller must hold m.mu.
func (m *Manager) isRunningLocked(name string) bool {
	if _, ok := m.procs[name]; ok {
		return true
	}
	prefix := name + "#"
	for key := range m.procs {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func (m *Manager) RunningNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Collapse replica keys ("php-fpm:8.1#2") back to their logical name so
	// callers like ReloadAll don't try to Reload a non-existent service.
	set := make(map[string]bool, len(m.procs))
	for key := range m.procs {
		set[logicalName(key)] = true
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TrackedProc is one live service process, used by the Ports panel to attribute
// listening ports back to the service that owns them.
type TrackedProc struct {
	Name  string // logical service name (e.g. "php-fpm:8.1")
	Label string // human label, falls back to Name
	PID   int
}

// TrackedPIDs returns every live service process (including pooled replicas)
// with its root PID, so callers can map listeners to the owning service.
func (m *Manager) TrackedPIDs() []TrackedProc {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TrackedProc, 0, len(m.procs))
	for key, cmd := range m.procs {
		if cmd == nil || cmd.Process == nil {
			continue
		}
		name := logicalName(key)
		label := name
		if def, ok := m.defs[name]; ok && def.Label != "" {
			label = def.Label
		}
		out = append(out, TrackedProc{Name: name, Label: label, PID: cmd.Process.Pid})
	}
	return out
}

// replicaKey is the internal process key for replica i of a logical service.
func replicaKey(name string, i int) string { return name + "#" + strconv.Itoa(i) }

// logicalName strips a "#<i>" replica suffix, yielding the logical service name.
func logicalName(key string) string {
	if idx := strings.IndexByte(key, '#'); idx >= 0 {
		return key[:idx]
	}
	return key
}

// replicaCount is the number of processes to run for a def (>=1).
func replicaCount(def Definition) int {
	if def.Replicas > 1 {
		return def.Replicas
	}
	return 1
}

func (m *Manager) startOne(name string) error {
	return m.startOneContext(context.Background(), name)
}

func (m *Manager) startOneContext(ctx context.Context, name string) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	started := map[string]*exec.Cmd{}
	defer func() {
		if ctx.Err() != nil {
			m.rollbackStartedLocked(started)
			err = ctx.Err()
		}
	}()
	return m.startOneLockedContext(ctx, name, started)
}

// The lifecycle mutex remains held across dependency and replica startup so
// rollback cannot stop a service started by another caller in the meantime.
func (m *Manager) startOneLockedContext(ctx context.Context, name string, started map[string]*exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	def, ok := m.definition(name)
	// Without this guard a phantom dep (e.g. nginx wired to a php-fpm version
	// nobody registered) flows into exec.Command("", ...) and surfaces as the
	// cryptic "exec: no command". Fail loudly with the offending name instead.
	if !ok {
		return i18n.Errorf("errors.services.notRegistered", map[string]any{"name": name})
	}
	if err := m.conflictFor(name); err != nil {
		return err
	}

	// Make sure the binary can actually load before we try to start it.
	// Without VC++ runtime DLLs, mysqld/httpd/postgres/redis-server fail
	// with exit code 0xc0000135 (STATUS_DLL_NOT_FOUND) before they even
	// parse args, which surfaces as a cryptic "process exited in Xms"
	// from the goroutine below. vcruntime.Check returns nil when the
	// binary either loads cleanly or doesn't exist yet — so this also
	// no-ops on services whose binary hasn't been downloaded. def.RedistURL
	// names the specific redistributable this binary needs (empty = the
	// vcruntime default), so the message points at the right download.
	if err := vcruntime.CheckContext(ctx, def.Path, def.RedistURL); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		m.emitCheckError(name, err)
		return err
	}

	// First-run initialization (e.g. mysqld --initialize-insecure, initdb).
	// Runs once per logical service, before any replica is spawned.
	if def.FirstRunCheck != nil && def.FirstRunInit != nil && def.FirstRunCheck() {
		if err := ctx.Err(); err != nil {
			return err
		}
		m.emitError(name, i18n.T("backend.services.firstRunInitializing", nil))
		if err := def.FirstRunInit(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			m.emitError(name, i18n.T("backend.services.initializationFailed", map[string]any{"error": err}))
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Single-process services keep the logical name as their process key
	// (back-compat). Pooled services (Replicas > 1) spawn "<name>#<i>" workers.
	n := replicaCount(def)
	if n <= 1 {
		cmd, err := m.startProcContext(ctx, name, def, def.Args)
		if cmd != nil {
			started[name] = cmd
		}
		return err
	}
	// Clear any lingering stop intent on the logical name so a later exit can
	// auto-restart again. Per-replica intents are cleared inside startProc.
	m.mu.Lock()
	delete(m.stopping, name)
	m.mu.Unlock()
	var firstErr error
	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		args := def.Args
		if def.ReplicaArgs != nil {
			args = def.ReplicaArgs(i)
		}
		key := replicaKey(name, i)
		cmd, err := m.startProcContext(ctx, key, def, args)
		if cmd != nil {
			started[key] = cmd
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// startProc spawns one process tracked under `key` (== def.Name for single
// services, "<def.Name>#<i>" for replicas) and arms the wait/restart goroutine.
func (m *Manager) startProc(key string, def Definition, args []string) error {
	_, err := m.startProcContext(context.Background(), key, def, args)
	return err
}

func (m *Manager) startProcContext(ctx context.Context, key string, def Definition, args []string) (*exec.Cmd, error) {
	m.mu.Lock()
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if _, running := m.procs[key]; running {
		m.mu.Unlock()
		return nil, nil
	}
	cmd := exec.Command(def.Path, args...)
	if def.WorkDir != "" {
		cmd.Dir = def.WorkDir
	} else {
		cmd.Dir = filepath.Dir(def.Path)
	}
	if len(def.Env) > 0 {
		env := os.Environ()
		for k, v := range def.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}
	hideWindow(cmd)
	if def.DropAdminToken {
		if cleanup, err := applyRestrictedToken(cmd); err == nil {
			defer cleanup()
		} else {
			// Fail open: without the restricted token postgres will refuse on
			// elevated sessions with its own (clearer) error, and still start
			// fine on non-elevated ones (e.g. wails dev).
			m.logRingForLocked(def.Name).push("warning: could not build restricted token: " + err.Error())
		}
	}
	var stderr, stdout bytes.Buffer
	// Tee output into a bounded ring (for the logs viewer) while keeping the
	// short buffers used for exit-error reporting.
	ring := m.logRingForLocked(def.Name)
	ring.push("--- " + time.Now().Format("15:04:05") + " starting " + def.Name + " ---")
	cmd.Stderr = io.MultiWriter(&stderr, &lineWriter{ring: ring})
	cmd.Stdout = io.MultiWriter(&stdout, &lineWriter{ring: ring})
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		m.emitError(def.Name, i18n.T("backend.services.startFailed", map[string]any{"error": err}))
		return nil, err
	}
	m.procs[key] = cmd
	m.procArgs[key] = args
	m.startedAt[key] = time.Now()
	// A fresh Start clears any prior "stopping" intent so a later exit can
	// auto-restart again.
	delete(m.stopping, key)
	m.mu.Unlock()

	go m.waitAndMaybeRestart(def.Name, key, cmd, &stdout, &stderr)
	return cmd, ctx.Err()
}

func (m *Manager) rollbackStartedLocked(started map[string]*exec.Cmd) {
	m.mu.Lock()
	var toKill []*exec.Cmd
	for key := range started {
		// Include a replacement spawned by this new service's supervisor, and
		// suppress a restart already waiting in backoff. Preexisting keys were
		// never recorded in started and retain their current process and intent.
		m.stopping[key] = true
		if cmd := m.procs[key]; cmd != nil && cmd.Process != nil {
			toKill = append(toKill, cmd)
		}
	}
	m.mu.Unlock()
	for _, cmd := range toKill {
		killTracked(cmd)
	}
}

func (m *Manager) respawnProc(key string, def Definition, args []string) error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.mu.Lock()
	if m.stopping[key] || m.stopping[def.Name] {
		delete(m.stopping, key)
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	return m.startProc(key, def, args)
}

// waitAndMaybeRestart blocks on cmd.Wait, then either reports the exit (for
// user-facing services, or when the user asked to stop) or transparently
// respawns internal services like php-fpm so a crashed PHP-CGI doesn't leave
// nginx serving 502s until the user clicks Start again.
func (m *Manager) waitAndMaybeRestart(name, key string, cmd *exec.Cmd, stdout, stderr *bytes.Buffer) {
	err := cmd.Wait()

	m.mu.Lock()
	uptime := time.Since(m.startedAt[key])
	delete(m.procs, key)
	delete(m.startedAt, key)
	def, hasDef := m.defs[name]
	stopping := m.stopping[key]
	if stopping {
		delete(m.stopping, key)
		delete(m.failStreak, key)
	}
	// A run that lasted long enough is treated as healthy: reset the streak
	// so a later death starts a fresh backoff sequence at 500ms.
	if uptime >= 30*time.Second {
		delete(m.failStreak, key)
	}
	streak := m.failStreak[key]
	args := m.procArgs[key]
	m.mu.Unlock()

	exitMsg := func() string {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" && err != nil {
			msg = err.Error()
		}
		if msg == "" {
			msg = i18n.T("backend.services.exitedNoMessage", map[string]any{"uptime": uptime.Round(time.Millisecond)})
		}
		return msg
	}

	// User-initiated stop: nothing to do. Don't restart, don't spam errors.
	if stopping {
		return
	}

	// Non-internal services (nginx, apache, mysql, …) keep the original
	// behavior: report only when the process died very quickly, otherwise
	// stay quiet and let the user drive Start/Stop.
	if !hasDef || !def.Internal {
		if uptime < 2*time.Second {
			m.emitError(name, exitMsg())
		}
		return
	}

	// Internal service: auto-restart with exponential backoff. Cap consecutive
	// short-lived failures so a fundamentally broken binary doesn't loop.
	// Streak is tracked per replica key so one flaky worker doesn't trip the
	// limit for its siblings.
	const maxStreak = 10
	if streak >= maxStreak {
		m.mu.Lock()
		delete(m.failStreak, key)
		m.mu.Unlock()
		m.emitError(name, i18n.T("backend.services.restartLoopGivingUp", map[string]any{"name": name, "streak": streak, "detail": exitMsg()}))
		return
	}

	delay := backoffFor(streak)
	m.mu.Lock()
	m.failStreak[key] = streak + 1
	m.mu.Unlock()

	// Surface the restart so the user can see it in the dashboard log instead
	// of wondering why PHP "just keeps working" (or, on failure, why it keeps
	// dying). The existing onError channel is reused as a generic event sink.
	m.emitError(name, i18n.T("backend.services.restartAfterCrash", map[string]any{"uptime": uptime.Round(time.Millisecond), "delay": delay, "detail": exitMsg()}))

	time.Sleep(delay)

	// Re-check stopping flag after sleeping: the user may have hit Stop on
	// the parent service while we were waiting to respawn.
	m.mu.Lock()
	if m.stopping[key] {
		delete(m.stopping, key)
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	// Respawn just this process (the same replica, on the same args/port),
	// not the whole pool — its siblings are still serving.
	if err := m.respawnProc(key, def, args); err != nil {
		m.emitError(name, i18n.T("backend.services.restartFailed", map[string]any{"name": name, "error": err}))
	}
}

func backoffFor(streak int) time.Duration {
	steps := []time.Duration{
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
	}
	if streak < 0 {
		streak = 0
	}
	if streak >= len(steps) {
		return steps[len(steps)-1]
	}
	return steps[streak]
}

func (m *Manager) stopOne(name string) error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	return m.stopOneLocked(name)
}

func (m *Manager) stopOneLocked(name string) error {
	m.mu.Lock()
	def, exists := m.defs[name]
	// Mark intent to stop BEFORE killing so the wait goroutine sees it and
	// skips the auto-restart path for internal services like php-fpm. Mark the
	// logical name and every replica key — including replicas currently sleeping
	// in backoff (not in m.procs), which would otherwise respawn after waking.
	m.stopping[name] = true
	if exists {
		for i := 0; i < replicaCount(def); i++ {
			m.stopping[replicaKey(name, i)] = true
		}
	}
	// Collect every live process belonging to this logical service: the direct
	// match (single services) plus any "<name>#<i>" replicas (pooled services).
	var toKill []*exec.Cmd
	prefix := name + "#"
	for key, cmd := range m.procs {
		if key == name || strings.HasPrefix(key, prefix) {
			if cmd.Process != nil {
				toKill = append(toKill, cmd)
			}
		}
	}
	m.mu.Unlock()

	if len(toKill) == 0 {
		// Mesmo sem rastrear o PID localmente, garante que nenhum processo
		// órfão com o mesmo nome de binário fique vivo.
		if exists {
			killByImage(filepath.Base(def.Path))
		}
		return nil
	}
	for _, cmd := range toKill {
		killTracked(cmd)
	}
	return nil
}

// StopAll para todos os serviços conhecidos (rastreados ou não), incluindo
// internos como php-fpm. Chamado no shutdown da app.
func (m *Manager) StopAll() {
	m.mu.Lock()
	pids := make([]int, 0, len(m.procs))
	for name, cmd := range m.procs {
		if cmd.Process != nil {
			pids = append(pids, cmd.Process.Pid)
		}
		// Suppress auto-restart for every tracked service before killing.
		m.stopping[name] = true
	}
	defs := make([]Definition, 0, len(m.defs))
	for _, d := range m.defs {
		defs = append(defs, d)
		// Also mark by-definition entries so any goroutine that races a
		// kill-by-image still treats the exit as user-initiated.
		m.stopping[d.Name] = true
	}
	m.procs = make(map[string]*exec.Cmd)
	m.procArgs = make(map[string][]string)
	m.startedAt = make(map[string]time.Time)
	m.mu.Unlock()

	for _, pid := range pids {
		killTree(pid)
	}
	// Garante que workers órfãos do nginx/mysqld também morram.
	for _, d := range defs {
		killByImage(filepath.Base(d.Path))
	}
}

// KillOrphans é executado no startup: mata qualquer binário de serviço
// conhecido que tenha sobrado de uma execução anterior do app.
func (m *Manager) KillOrphans() {
	m.mu.Lock()
	defs := make([]Definition, 0, len(m.defs))
	for _, d := range m.defs {
		defs = append(defs, d)
	}
	m.mu.Unlock()
	for _, d := range defs {
		killByImage(filepath.Base(d.Path))
	}
}

// killTracked retains the process handle as a fallback if taskkill cannot
// access the process, and bounds the helper so cancellation cleanup can finish.
func killTracked(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	kill := exec.CommandContext(ctx, "taskkill.exe", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	hideWindow(kill)
	_ = kill.Run()
	_ = cmd.Process.Kill()
}

// killTree mata o processo e toda a árvore de filhos (nginx workers, etc.).
func killTree(pid int) {
	if pid <= 0 {
		return
	}
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
	hideWindow(cmd)
	_ = cmd.Run()
}

// killByImage mata todos os processos com o nome de imagem dado.
// Sem efeito se não houver match.
func killByImage(image string) {
	if image == "" {
		return
	}
	cmd := exec.Command("taskkill", "/F", "/T", "/IM", image)
	hideWindow(cmd)
	_ = cmd.Run()
}

func (m *Manager) emitError(name, msg string) {
	if m.onError != nil {
		m.onError(ErrorEvent{Service: name, Message: msg})
	}
}

// emitCheckError reports a vcruntime.Check failure, threading the resolved
// redistributable URL through as structured data (ErrorEvent.RedistURL)
// via vcruntime.URLFromError — instead of leaving the frontend to parse it
// back out of err.Error().
func (m *Manager) emitCheckError(name string, err error) {
	if m.onError == nil {
		return
	}
	m.onError(ErrorEvent{Service: name, Message: err.Error(), RedistURL: vcruntime.URLFromError(err)})
}

// runOnce executes a binary and waits for it to exit, capturing stderr/stdout
// so any failure is surfaced. Used for first-run init (mysqld --initialize, initdb).
func runOnce(bin string, args []string, workDir string, timeout time.Duration) error {
	if _, err := os.Stat(bin); err != nil {
		return i18n.Errorf("errors.services.binaryNotFound", map[string]any{"path": bin})
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = workDir
	hideWindow(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = err.Error()
		}
		return i18n.Errorf("errors.services.processFailed", map[string]any{"bin": filepath.Base(bin), "message": msg})
	}
	return nil
}

func (m *Manager) WatchStatus(ctx context.Context, emit func(name, status string)) {
	t := time.NewTicker(1500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.mu.Lock()
			// Watch all known non-internal services so the UI reflects state
			// even after the user changes the active stack.
			for name, def := range m.defs {
				if def.Internal {
					continue
				}
				cur := "stopped"
				if m.isRunningLocked(name) {
					cur = "running"
				}
				if m.last[name] != cur {
					m.last[name] = cur
					emit(name, cur)
				}
			}
			m.mu.Unlock()
		}
	}
}

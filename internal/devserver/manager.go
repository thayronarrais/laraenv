// Package devserver runs a project's `dev` (or `start`) script in the
// background, captures stdout/stderr in a ring buffer, and exposes
// start/stop/status/logs to the rest of the app.
//
// State is purely in-memory. When the app exits, processes are killed via
// taskkill /T /F /PID so children (esbuild workers, vite spawned procs)
// don't survive.
package devserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"lara4ever/internal/i18n"
	"lara4ever/internal/netports"
)

const ringSize = 500

type RuntimePathResolver func(projectName, projectPath string) ([]string, error)

type ProjectPackageManagerResolver func(projectName, projectPath, prefer string, pathDirs []string) (bin, pm string, err error)

// StackExes are the executables our dev servers launch that may end up holding
// a TCP port. KillPort uses this allowlist so freeing a port can never kill an
// unrelated application that happens to be bound to it.
var StackExes = []string{"php.exe", "php-cgi.exe", "node.exe", "bun.exe"}

// Status represents what the UI needs to know about a dev server.
type Status struct {
	Running     bool   `json:"running"`
	PID         int    `json:"pid"`
	Port        int    `json:"port"`
	Script      string `json:"script"`
	InstanceID  string `json:"instanceId,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
	ProjectPath string `json:"projectPath,omitempty"`
}

// LogLine is emitted as a Wails event when a new line arrives.
type LogLine struct {
	Project  string `json:"project"`
	Instance string `json:"instance,omitempty"`
	Line     string `json:"line"`
}

type process struct {
	cmd         *exec.Cmd
	logs        []string
	mu          sync.Mutex
	port        int
	portFromLog bool
	// portPrio ranks the source of the current port so a lower-confidence
	// signal can't override a higher one. -1 = the reserved guess (no log yet);
	// 0 = a bare localhost:<port>; 1 = a generic "ready/listening" line (e.g.
	// Vite's "Local:"); 2 = an application server line ("Server running on",
	// e.g. `php artisan serve`). For a Laravel `composer run dev` that starts
	// both artisan serve (:8000) and Vite (:5174), this keeps the proxy pointed
	// at the app server, not the asset server.
	portPrio int
	script   string
	instance string
	project  string
	path     string
}

func (p *process) push(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logs = append(p.logs, line)
	if len(p.logs) > ringSize {
		p.logs = p.logs[len(p.logs)-ringSize:]
	}
}

func (p *process) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.logs))
	copy(out, p.logs)
	return out
}

func (p *process) updatePort(port, priority int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if port <= 0 || priority < p.portPrio || port == p.port {
		return false
	}
	p.port = port
	p.portPrio = priority
	p.portFromLog = true
	return true
}

func (p *process) currentPort() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.port
}

func (p *process) status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Status{Running: true, PID: p.cmd.Process.Pid, Port: p.port, Script: p.script, InstanceID: p.instance, ProjectName: p.project, ProjectPath: p.path}
}

// Manager owns the running processes per project.
type Manager struct {
	startMu        sync.Mutex // serializes launches, port reservation, and stops
	mu             sync.Mutex
	procs          map[string]*process
	emit           func(event string, data ...any) // wruntime.EventsEmit
	pmFinder       func(projectPath, prefer string) (bin, pm string, err error)
	pathDirs       func(projectName string) []string
	runtimePathFor RuntimePathResolver
	projectPMFor   ProjectPackageManagerResolver
	// allowedHosts returns the hostnames the reverse proxy will send in the
	// Host header for a project (main vhost + worktree routes). Vite 6+ rejects
	// requests from hosts outside server.allowedHosts with a 403; exporting them
	// via __VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS avoids editing vite.config.
	allowedHosts   func(projectName string) []string
	environmentFor func(projectName, projectPath string, env []string) ([]string, error)
}

// SetEnvironmentResolver augments each child environment after runtime PATH
// selection. An error prevents launch rather than weakening trust settings.
func (m *Manager) SetEnvironmentResolver(resolver func(string, string, []string) ([]string, error)) {
	m.mu.Lock()
	m.environmentFor = resolver
	m.mu.Unlock()
}

// SetRuntimePathResolver selects runtimes for each checkout before launch.
// It has no effect on processes that are already running.
func (m *Manager) SetRuntimePathResolver(resolver RuntimePathResolver) {
	m.mu.Lock()
	m.runtimePathFor = resolver
	m.mu.Unlock()
}

// SetProjectPackageManagerResolver selects the executable from the same runtime
// PATH snapshot used to launch the process. Existing processes are unaffected.
func (m *Manager) SetProjectPackageManagerResolver(resolver ProjectPackageManagerResolver) {
	m.mu.Lock()
	m.projectPMFor = resolver
	m.mu.Unlock()
}

func (m *Manager) resolvePackageManager(projectName, projectPath, prefer string, pathDirs []string) (string, string, error) {
	m.mu.Lock()
	resolver := m.projectPMFor
	m.mu.Unlock()
	if resolver != nil {
		return resolver(projectName, projectPath, prefer, append([]string(nil), pathDirs...))
	}
	return m.pmFinder(projectPath, prefer)
}

func (m *Manager) resolveRuntimePaths(projectName, projectPath string) ([]string, error) {
	m.mu.Lock()
	resolver := m.runtimePathFor
	m.mu.Unlock()
	if resolver != nil {
		return resolver(projectName, projectPath)
	}
	if m.pathDirs != nil {
		return m.pathDirs(projectName), nil
	}
	return nil, nil
}

// NewManager constructs a Manager. emit forwards events to the frontend.
// pmFinder resolves which package manager binary to use for a project
// (returning the absolute path + the canonical name e.g. "pnpm").
// pathDirs returns the bin dirs to prepend to the dev server's PATH, in
// priority order (e.g. [php, node, composer]); the first dir wins. This is
// what lets custom commands resolve bare `php` / `composer` invocations.
// allowedHosts (optional) returns the proxy hostnames for a project so
// Vite-based dev servers accept requests forwarded by nginx/Apache.
func NewManager(
	emit func(event string, data ...any),
	pmFinder func(projectPath, prefer string) (string, string, error),
	pathDirs func(projectName string) []string,
	allowedHosts func(projectName string) []string,
) *Manager {
	return &Manager{
		procs:        map[string]*process{},
		emit:         emit,
		pmFinder:     pmFinder,
		pathDirs:     pathDirs,
		allowedHosts: allowedHosts,
	}
}

// allowedHostsEnv builds the Vite env assignment that whitelists the proxy
// hostnames (comma-separated, trimmed, deduped). Empty when there is nothing
// to add so callers can skip appending it.
func allowedHostsEnv(hosts []string) string {
	seen := map[string]bool{}
	var out []string
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	if len(out) == 0 {
		return ""
	}
	return "__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS=" + strings.Join(out, ",")
}

// Start launches `<pm> run <script>` in projectPath. Returns the PID.
// `script` is the package.json script name; "" auto-picks "dev" then "start".
// `prefer` is the package manager hint from project detection ("npm","pnpm","yarn","bun");
// "" lets pmFinder decide.
// `preferredPort` is the port to try first (e.g. the project's configured
// ProxyPort); if 0 or already in use, the manager scans upward for the next
// free TCP port.
func (m *Manager) Start(projectName, projectPath, script, prefer string, preferredPort int, customCmd string) (int, error) {
	return m.StartInstance(projectName, projectName, projectPath, script, prefer, preferredPort, customCmd)
}

// StartInstance launches a dev server under an explicit process identity.
// Main checkouts use the project name; worktrees use a session-scoped key so
// both can run simultaneously without sharing lifecycle or port state.
func (m *Manager) StartInstance(instanceID, projectName, projectPath, script, prefer string, preferredPort int, customCmd string) (int, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return 0, i18n.Errorf("errors.devserver.instanceIdRequired", nil)
	}
	m.startMu.Lock()
	unlock := sync.OnceFunc(m.startMu.Unlock)
	defer unlock()
	m.mu.Lock()
	if existing, ok := m.procs[instanceID]; ok && existing.cmd != nil && existing.cmd.Process != nil {
		m.mu.Unlock()
		return existing.cmd.Process.Pid, i18n.Errorf("errors.devserver.alreadyRunning", map[string]any{"instanceId": instanceID})
	}
	m.mu.Unlock()

	pathDirs, err := m.resolveRuntimePaths(projectName, projectPath)
	if err != nil {
		return 0, err
	}

	base := preferredPort
	if base <= 0 {
		base = 3000
	}
	port := m.pickFreePort(base)

	var cmd *exec.Cmd
	var scriptLabel string
	if strings.TrimSpace(customCmd) != "" {
		// Custom command: run the user's exact command line via cmd /c,
		// substituting {port} with the reserved free port. No `-- --port`
		// is appended (arbitrary tools have their own port flags).
		line := substitutePort(customCmd, port)
		// Passed unescaped to `cmd /c` on purpose so the user's command can use
		// shell operators (&&, |, etc.); do not "harden"/quote this.
		cmd = exec.Command("cmd", "/c", line)
		scriptLabel = customCmd
	} else {
		// Default behavior: `<pm> run <script>`. Only direct, compatible dev
		// tools receive a port flag; wrappers and arbitrary scripts may reject it.
		pkgScripts, err := readScripts(projectPath)
		if err != nil {
			return 0, err
		}
		if script == "" {
			switch {
			case pkgScripts["dev"] != "":
				script = "dev"
			case pkgScripts["start"] != "":
				script = "start"
			default:
				return 0, i18n.Errorf("errors.devserver.noDevOrStartScript", nil)
			}
		}
		bin, _, err := m.resolvePackageManager(projectName, projectPath, prefer, pathDirs)
		if err != nil {
			return 0, err
		}
		args := []string{"run", script}
		if port > 0 && scriptAcceptsPortFlag(pkgScripts[script]) {
			args = append(args, "--", "--port", strconv.Itoa(port))
		}
		cmd = exec.Command(bin, args...)
		scriptLabel = script
	}
	cmd.Dir = projectPath
	hideWindow(cmd)
	// Prepend the project's tool bin dirs to PATH so the dev server uses the
	// per-project Node version (matters for native modules like esbuild) and
	// so custom commands can resolve bare `php` / `composer`. Dirs come in
	// priority order; prepend in reverse so the first one ends up first.
	env := os.Environ()
	for i := len(pathDirs) - 1; i >= 0; i-- {
		if pathDirs[i] != "" {
			env = prependPath(env, pathDirs[i])
		}
	}
	if port > 0 {
		env = append(env, fmt.Sprintf("PORT=%d", port), "BROWSER=none")
	}
	if m.allowedHosts != nil {
		if kv := allowedHostsEnv(m.allowedHosts(projectName)); kv != "" {
			env = append(env, kv)
		}
	}
	m.mu.Lock()
	resolveEnvironment := m.environmentFor
	m.mu.Unlock()
	if resolveEnvironment != nil {
		env, err = resolveEnvironment(projectName, projectPath, env)
		if err != nil {
			return 0, err
		}
	}
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}

	p := &process{cmd: cmd, script: scriptLabel, port: port, portPrio: -1, instance: instanceID, project: projectName, path: projectPath}
	m.mu.Lock()
	m.procs[instanceID] = p
	m.mu.Unlock()
	unlock()
	if m.emit != nil {
		m.emit("devserver:status", instanceID)
	}

	pump := func(r io.Reader) {
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 64*1024), 1024*1024)
		for s.Scan() {
			line := stripANSI(s.Text())
			p.push(line)
			// Update the running port from log output. We deliberately do
			// NOT latch on the first match: dev tools often print a "Port X
			// is in use, using Y instead" line before the real "ready on Y"
			// line, and we want the final binding to win. Lines that look
			// like a port-conflict warning are ignored outright. A new port
			// only wins if its source is at least as authoritative as the
			// current one (prio), so an app server (artisan serve) outranks a
			// co-running asset server (Vite) regardless of print order.
			if !isPortConflictLine(line) {
				if port, prio := extractBindingPort(line); p.updatePort(port, prio) {
					if m.emit != nil {
						m.emit("devserver:status", instanceID)
					}
				}
			}
			if m.emit != nil {
				m.emit("devserver:log", LogLine{Project: projectName, Instance: instanceID, Line: line})
			}
		}
	}
	go pump(stdout)
	go pump(stderr)
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		if m.procs[instanceID] == p {
			delete(m.procs, instanceID)
		}
		m.mu.Unlock()
		if m.emit != nil {
			m.emit("devserver:status", instanceID)
		}
	}()

	return cmd.Process.Pid, nil
}

// Stop kills the process tree for projectName. No-op if not running.
func (m *Manager) Stop(projectName string) error {
	m.startMu.Lock()
	unlock := sync.OnceFunc(m.startMu.Unlock)
	defer unlock()
	m.mu.Lock()
	p, ok := m.procs[projectName]
	m.mu.Unlock()
	if !ok || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	pid := p.cmd.Process.Pid
	// Collect every port the live process tree is listening on BEFORE killing,
	// so we can free them even if a child (e.g. `php artisan serve`, vite) is
	// detached and survives the PID-tree kill below.
	ports := netports.PortsForTree(pid)
	if port := p.currentPort(); port > 0 {
		ports = append(ports, port)
	}
	// taskkill kills the whole tree; vite/next spawn esbuild workers, etc.
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		// A "not found" here means the tree already exited — not fatal; we
		// still try to free any port a detached child left bound below.
		s := strings.ToLower(string(out))
		if !strings.Contains(s, "not found") && !strings.Contains(s, "no running") {
			return i18n.Wrapf(err, "errors.devserver.taskkillFailed", map[string]any{"output": strings.TrimSpace(string(out))})
		}
	}
	// Belt-and-suspenders: kill anything from our own stack still holding those
	// ports. The exe allowlist guards against nuking an unrelated app.
	for _, port := range ports {
		if _, err := netports.KillPort(port, StackExes...); err != nil {
			fmt.Printf("[devserver] free port %d for %s: %v\n", port, projectName, err)
		}
	}
	// Remove from the live map immediately so callers reading Status right
	// after Stop don't still see Running=true with the old port (the
	// cmd.Wait goroutine also deletes, but that runs asynchronously).
	m.mu.Lock()
	if m.procs[projectName] == p {
		delete(m.procs, projectName)
	}
	m.mu.Unlock()
	unlock()
	if m.emit != nil {
		m.emit("devserver:status", projectName)
	}
	return nil
}

// Status returns the current state for projectName.
func (m *Manager) Status(projectName string) Status {
	m.mu.Lock()
	p, ok := m.procs[projectName]
	m.mu.Unlock()
	if !ok || p.cmd == nil || p.cmd.Process == nil {
		return Status{}
	}
	return p.status()
}

// Running is a snapshot of one live dev server, used by the Ports panel.
type Running struct {
	Instance string
	Project  string
	Path     string
	PID      int
	Port     int
}

// ListRunning returns every dev server currently tracked as running, with the
// root PID (the launcher) and the last-known port.
func (m *Manager) ListRunning() []Running {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Running, 0, len(m.procs))
	for name, p := range m.procs {
		if p.cmd == nil || p.cmd.Process == nil {
			continue
		}
		status := p.status()
		out = append(out, Running{Instance: name, Project: p.project, Path: p.path, PID: status.PID, Port: status.Port})
	}
	return out
}

// Logs returns the ring buffer snapshot.
func (m *Manager) Logs(projectName string) []string {
	m.mu.Lock()
	p, ok := m.procs[projectName]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return p.snapshot()
}

// StopAll kills every running dev server. Called on app shutdown.
func (m *Manager) StopAll() {
	m.mu.Lock()
	names := make([]string, 0, len(m.procs))
	for n := range m.procs {
		names = append(names, n)
	}
	m.mu.Unlock()
	for _, n := range names {
		_ = m.Stop(n)
	}
}

func readScripts(projectPath string) (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(projectPath, "package.json"))
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.devserver.readPackageJsonFailed", nil)
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return nil, i18n.Wrapf(err, "errors.devserver.parsePackageJsonFailed", nil)
	}
	if pkg.Scripts == nil {
		pkg.Scripts = map[string]string{}
	}
	return pkg.Scripts, nil
}

// ListScripts reads only package.json, without starting a package manager or
// invoking project code. Missing manifests produce an empty, non-nil list.
func ListScripts(projectPath string) ([]string, error) {
	scripts, err := readScripts(projectPath)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(scripts))
	for name := range scripts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func scriptAcceptsPortFlag(script string) bool {
	// Appending arguments to a shell chain would target its final command.
	if strings.ContainsAny(script, "&|;<>\r\n") {
		return false
	}
	fields := strings.Fields(script)
	if len(fields) == 0 {
		return false
	}
	command := strings.Trim(fields[0], `"'`)
	mode := ""
	if len(fields) > 1 && !strings.HasPrefix(fields[1], "-") {
		mode = fields[1]
	}
	switch command {
	case "vite":
		return mode == "" || mode == "dev" || mode == "serve" || mode == "preview"
	case "next":
		return mode == "" || mode == "dev" || mode == "start"
	case "nuxt", "nuxi":
		return mode == "" || mode == "dev" || mode == "start" || mode == "preview"
	case "astro":
		return mode == "dev" || mode == "preview"
	default:
		return false
	}
}

// pickFreePort scans sequentially from base for a TCP port not already
// assigned to another live dev server and not in use on 127.0.0.1.
// Returns 0 if nothing is free in the scan window (frameworks then fall
// back to their own defaults).
func (m *Manager) pickFreePort(base int) int {
	m.mu.Lock()
	used := make(map[int]bool, len(m.procs))
	for _, p := range m.procs {
		if port := p.currentPort(); port > 0 {
			used[port] = true
		}
	}
	m.mu.Unlock()
	for port := base; port < base+200; port++ {
		if used[port] {
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		_ = ln.Close()
		return port
	}
	return 0
}

var portRE = regexp.MustCompile(`(?i)(?:localhost|127\.0\.0\.1)(?::|%3A)(\d{2,5})`)

func extractPort(line string) int {
	m := portRE.FindStringSubmatch(line)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// portConflictRE matches the warning frameworks print when their requested
// port is already in use ("Port 3000 is in use", "address already in use",
// "EADDRINUSE", "trying 3001 instead"). Numbers on these lines are NOT the
// real binding port and should be ignored.
var portConflictRE = regexp.MustCompile(`(?i)(in use|already in use|EADDRINUSE|trying \d+ instead|using available port)`)

func isPortConflictLine(line string) bool {
	return portConflictRE.MatchString(line)
}

// bindingPortRE matches lines that announce the actual listening port
// (Vite "Local:", Next.js "ready on" / "started server on", generic
// "listening on"). These take precedence over any earlier guess.
// appServerPortRE matches an application server announcing its address, e.g.
// `php artisan serve`'s "INFO  Server running on [http://127.0.0.1:8000]" or a
// Node app's "started server on …". Higher confidence than a generic binding
// line because this is the actual app, not an asset/HMR sidecar.
var appServerPortRE = regexp.MustCompile(`(?i)(?:server running|started server)[^0-9]*(?:https?://)?(?:localhost|127\.0\.0\.1|0\.0\.0\.0)(?::|%3A)(\d{2,5})`)

// bindingPortRE matches generic "this is where we're listening" lines, e.g.
// Vite's "➜  Local:   http://localhost:5173/" or "ready on …".
var bindingPortRE = regexp.MustCompile(`(?i)(local|ready|listening|available)[^0-9]*(?:https?://)?(?:localhost|127\.0\.0\.1|0\.0\.0\.0)(?::|%3A)(\d{2,5})`)

// extractBindingPort returns a port number and a confidence priority from a log
// line: 2 = application server, 1 = generic binding marker, 0 = a bare
// localhost:<port> fallback. Port 0 means no match.
func extractBindingPort(line string) (port, prio int) {
	if m := appServerPortRE.FindStringSubmatch(line); len(m) >= 2 {
		n, _ := strconv.Atoi(m[1])
		return n, 2
	}
	if m := bindingPortRE.FindStringSubmatch(line); len(m) >= 3 {
		n, _ := strconv.Atoi(m[2])
		return n, 1
	}
	if n := extractPort(line); n > 0 {
		return n, 0
	}
	return 0, 0
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// substitutePort replaces the literal token "{port}" in a custom command with
// the chosen free port. A port of 0 removes the token (the tool then picks its
// own port; log detection still drives the proxy).
func substitutePort(cmd string, port int) string {
	if port <= 0 {
		return strings.ReplaceAll(cmd, "{port}", "")
	}
	return strings.ReplaceAll(cmd, "{port}", strconv.Itoa(port))
}

func prependPath(env []string, dir string) []string {
	for i, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			env[i] = "PATH=" + dir + string(os.PathListSeparator) + e[len("PATH="):]
			return env
		}
	}
	return append(env, "PATH="+dir)
}

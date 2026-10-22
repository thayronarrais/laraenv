// Package queueworker runs `php artisan queue:work` for a Laravel project in
// the background, captures stdout/stderr in a ring buffer, and exposes
// start/stop/restart/status/logs to the rest of the app. Mirrors the devserver
// package; state is purely in-memory and processes are killed via taskkill /T
// /F /PID on shutdown so children don't survive.
package queueworker

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"lara4ever/internal/i18n"
)

const ringSize = 500

// Status is what the UI needs to know about a queue worker.
type Status struct {
	Running bool     `json:"running"`
	PID     int      `json:"pid"`
	Args    []string `json:"args"`
}

// LogLine is emitted as a Wails event when a new line arrives.
type LogLine struct {
	Project string `json:"project"`
	Line    string `json:"line"`
}

type process struct {
	cmd  *exec.Cmd
	logs []string
	mu   sync.Mutex
	args []string
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

// Manager owns the running workers per project.
type Manager struct {
	startMu        sync.Mutex // serializes lifecycle changes through process registration
	mu             sync.Mutex
	procs          map[string]*process
	emit           func(event string, data ...any) // wruntime.EventsEmit
	phpExe         func(projectName string) string
	environmentFor func(projectName, projectPath string, env []string) ([]string, error)
}

func (m *Manager) SetEnvironmentResolver(resolver func(string, string, []string) ([]string, error)) {
	m.mu.Lock()
	m.environmentFor = resolver
	m.mu.Unlock()
}

// NewManager constructs a Manager. emit forwards events to the frontend.
// phpExe resolves the absolute path to the project's php.exe.
func NewManager(
	emit func(event string, data ...any),
	phpExe func(projectName string) string,
) *Manager {
	return &Manager{
		procs:  map[string]*process{},
		emit:   emit,
		phpExe: phpExe,
	}
}

// Start launches `php artisan queue:work <extraArgs...>` in projectPath.
// Returns the PID. Errors if no PHP is resolvable, the project has no
// `artisan` file, or a worker is already running for the project.
func (m *Manager) Start(projectName, projectPath string, extraArgs []string) (int, error) {
	m.startMu.Lock()
	unlock := sync.OnceFunc(m.startMu.Unlock)
	defer unlock()
	m.mu.Lock()
	if existing, ok := m.procs[projectName]; ok && existing.cmd != nil && existing.cmd.Process != nil {
		m.mu.Unlock()
		return existing.cmd.Process.Pid, i18n.Errorf("errors.queueworker.alreadyRunning", map[string]any{"project": projectName})
	}
	m.mu.Unlock()

	php := ""
	if m.phpExe != nil {
		php = m.phpExe(projectName)
	}
	if php == "" {
		return 0, i18n.Errorf("errors.queueworker.phpUnavailable", map[string]any{"project": projectName})
	}
	if _, err := os.Stat(filepath.Join(projectPath, "artisan")); err != nil {
		return 0, i18n.Errorf("errors.queueworker.artisanNotFound", map[string]any{"path": projectPath})
	}

	args := append([]string{"artisan", "queue:work"}, extraArgs...)
	cmd := exec.Command(php, args...)
	cmd.Dir = projectPath
	hideWindow(cmd)
	cmd.Env = os.Environ()
	m.mu.Lock()
	resolveEnvironment := m.environmentFor
	m.mu.Unlock()
	if resolveEnvironment != nil {
		var err error
		cmd.Env, err = resolveEnvironment(projectName, projectPath, cmd.Env)
		if err != nil {
			return 0, err
		}
	}

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

	p := &process{cmd: cmd, args: append([]string(nil), extraArgs...)}
	m.mu.Lock()
	m.procs[projectName] = p
	m.mu.Unlock()
	unlock()
	if m.emit != nil {
		m.emit("queueworker:status", projectName)
	}

	pump := func(r io.Reader) {
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 64*1024), 1024*1024)
		for s.Scan() {
			line := stripANSI(s.Text())
			p.push(line)
			if m.emit != nil {
				m.emit("queueworker:log", LogLine{Project: projectName, Line: line})
			}
		}
	}
	go pump(stdout)
	go pump(stderr)
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		if m.procs[projectName] == p {
			delete(m.procs, projectName)
		}
		m.mu.Unlock()
		if m.emit != nil {
			m.emit("queueworker:status", projectName)
		}
	}()

	return cmd.Process.Pid, nil
}

// Stop kills the process tree for projectName. No-op if not running.
func (m *Manager) Stop(projectName string) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.mu.Lock()
	p, ok := m.procs[projectName]
	m.mu.Unlock()
	if !ok || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	pid := p.cmd.Process.Pid
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return i18n.Wrapf(err, "errors.queueworker.taskkillFailed", map[string]any{"output": strings.TrimSpace(string(out))})
	}
	return nil
}

// Restart stops the running worker (if any), waits up to 2s for the entry
// to clear, then starts again with the supplied args.
func (m *Manager) Restart(projectName, projectPath string, extraArgs []string) (int, error) {
	if err := m.Stop(projectName); err != nil {
		return 0, err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		_, still := m.procs[projectName]
		m.mu.Unlock()
		if !still {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return m.Start(projectName, projectPath, extraArgs)
}

// Status returns the current state for projectName.
func (m *Manager) Status(projectName string) Status {
	m.mu.Lock()
	p, ok := m.procs[projectName]
	m.mu.Unlock()
	if !ok || p.cmd == nil || p.cmd.Process == nil {
		return Status{}
	}
	return Status{Running: true, PID: p.cmd.Process.Pid, Args: append([]string(nil), p.args...)}
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

// StopAll kills every running worker. Called on app shutdown.
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

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

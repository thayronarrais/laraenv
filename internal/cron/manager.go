// Package cron is an in-process scheduler for user-defined commands.
// Jobs run while the app is open (window or tray); they don't survive a
// full quit. Persisted in config.json so they re-register on startup.
package cron

import (
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	rcron "github.com/robfig/cron/v3"

	"lara4ever/internal/i18n"
)

// createNoWindow keeps cron tick executions silent: every fired job spawns
// `cmd.exe /C …` and without this flag Windows would briefly pop a console
// window on each tick.
const createNoWindow = 0x08000000

// PathResolver lets the manager inject the project's PHP/Node binaries into
// PATH at run time. Returns the directories to prepend (in priority order).
type PathResolver func(projectName, phpOverride string) []string

// WorkDirResolver returns the absolute working directory for a job. The
// override (j.WorkingDir) wins; otherwise the project's root is used; an
// empty string is returned for free-standing global jobs.
type WorkDirResolver func(projectName, override string) string

// RuntimeValidator rejects a job before spawning when the required runtimes
// are unavailable. phpOverride is the job's explicit pin, which takes priority
// over a detected project requirement.
type RuntimeValidator func(projectName, workDir, phpOverride string) error

// RuntimePathResolver selects PATH for the actual checkout used by this run.
// Errors block execution rather than silently falling back to system tools.
type RuntimePathResolver func(projectName, workDir, phpOverride string) ([]string, error)

// Manager owns the cron engine and the persistence callbacks.
type Manager struct {
	mu              sync.RWMutex
	jobs            map[string]*Job
	entries         map[string]rcron.EntryID // job ID → cron entry
	cron            *rcron.Cron
	parser          rcron.Parser
	pathFor         PathResolver
	workDirFor      WorkDirResolver
	validateRuntime RuntimeValidator
	runtimePathFor  RuntimePathResolver
	environmentFor  func(projectName, projectPath string, env []string) ([]string, error)
	persist         func([]Job) error
	emit            func(j Job)
	// exePath is the absolute path to laraenv.exe — needed to register
	// Task Scheduler tasks that call back with --cron-run. Optional:
	// when empty, BackgroundMode jobs fall back to in-process scheduling.
	exePath string
}

func (m *Manager) SetEnvironmentResolver(resolver func(string, string, []string) ([]string, error)) {
	m.mu.Lock()
	m.environmentFor = resolver
	m.mu.Unlock()
}

// SetExePath wires the path used when registering Task Scheduler entries
// for jobs with BackgroundMode=true. Called from app.startup once we
// know our own binary path.
func (m *Manager) SetExePath(p string) {
	m.mu.Lock()
	m.exePath = p
	m.mu.Unlock()
}

// NewManager creates the engine but does not start it. Call Start() once
// the rest of the app is ready (after config + project list are known).
func NewManager(persist func([]Job) error, pathFor PathResolver, workDirFor WorkDirResolver) *Manager {
	parser := rcron.NewParser(
		rcron.SecondOptional | rcron.Minute | rcron.Hour | rcron.Dom |
			rcron.Month | rcron.Dow | rcron.Descriptor,
	)
	return &Manager{
		jobs:       map[string]*Job{},
		entries:    map[string]rcron.EntryID{},
		cron:       rcron.New(rcron.WithParser(parser)),
		parser:     parser,
		pathFor:    pathFor,
		workDirFor: workDirFor,
		persist:    persist,
	}
}

// SetEmit wires the per-run status broadcaster. Called from app.startup so
// the wails ctx is available for EventsEmit.
func (m *Manager) SetEmit(fn func(j Job)) {
	m.mu.Lock()
	m.emit = fn
	m.mu.Unlock()
}

// SetRuntimeValidator wires the same preflight used by interactive commands.
func (m *Manager) SetRuntimeValidator(fn RuntimeValidator) {
	m.mu.Lock()
	m.validateRuntime = fn
	m.mu.Unlock()
}

func (m *Manager) SetRuntimePathResolver(fn RuntimePathResolver) {
	m.mu.Lock()
	m.runtimePathFor = fn
	m.mu.Unlock()
}

// Start hydrates from the persisted job list and begins the engine.
func (m *Manager) Start(initial []Job) {
	m.mu.Lock()
	for i := range initial {
		j := initial[i]
		m.jobs[j.ID] = &j
		if j.Enabled {
			m.scheduleLocked(&j)
		}
	}
	m.mu.Unlock()
	m.cron.Start()
}

// Stop tears the engine down. Call from app.shutdown.
func (m *Manager) Stop() {
	if m.cron == nil {
		return
	}
	ctx := m.cron.Stop()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
	}
}

// List returns all jobs sorted by name. Computes NextRun on the fly.
func (m *Manager) List() []Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Job, 0, len(m.jobs))
	for id, j := range m.jobs {
		copy := *j
		if eid, ok := m.entries[id]; ok {
			if e := m.cron.Entry(eid); e.ID != 0 {
				copy.NextRun = e.Next
			}
		}
		out = append(out, copy)
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].ProjectName != out[k].ProjectName {
			return out[i].ProjectName < out[k].ProjectName
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[k].Name)
	})
	return out
}

// Save upserts a job. Validates the schedule before accepting. New jobs get
// a UUID. Re-registers with the engine so changes apply immediately.
func (m *Manager) Save(j Job) (Job, error) {
	if strings.TrimSpace(j.Name) == "" {
		return j, i18n.Errorf("errors.cron.nameRequired", nil)
	}
	if strings.TrimSpace(j.Schedule) == "" {
		return j, i18n.Errorf("errors.cron.scheduleRequired", nil)
	}
	if strings.TrimSpace(j.Command) == "" {
		return j, i18n.Errorf("errors.cron.commandRequired", nil)
	}
	if _, err := m.parser.Parse(j.Schedule); err != nil {
		return j, i18n.Wrapf(err, "errors.cron.invalidSchedule", nil)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if j.ID == "" {
		j.ID = uuid.NewString()
	}
	// Drop the previous registration before replacing. Also clear any
	// stale Task Scheduler entry — the user may have toggled BackgroundMode
	// off, in which case we must remove the old task to stop double-firing.
	if eid, ok := m.entries[j.ID]; ok {
		m.cron.Remove(eid)
		delete(m.entries, j.ID)
	}
	if !j.BackgroundMode && HasBackgroundTask(j.ID) {
		RemoveBackgroundTask(j.ID)
	}

	stored := j
	m.jobs[j.ID] = &stored
	if stored.Enabled {
		if err := m.scheduleLocked(&stored); err != nil {
			return j, err
		}
	}
	if err := m.persistLocked(); err != nil {
		return j, err
	}
	return stored, nil
}

// Remove deletes a job and unregisters it from the engine.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if eid, ok := m.entries[id]; ok {
		m.cron.Remove(eid)
		delete(m.entries, id)
	}
	// If the job had a Task Scheduler entry, drop it too. Best-effort —
	// HasBackgroundTask call avoids spawning schtasks when there's nothing
	// to remove.
	if HasBackgroundTask(id) {
		RemoveBackgroundTask(id)
	}
	delete(m.jobs, id)
	return m.persistLocked()
}

// Toggle flips Enabled on a job and (un)registers it accordingly.
func (m *Manager) Toggle(id string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return i18n.Errorf("errors.cron.jobNotFound", map[string]any{"id": id})
	}
	j.Enabled = enabled
	if eid, ok := m.entries[id]; ok {
		m.cron.Remove(eid)
		delete(m.entries, id)
	}
	// Disabling a background job means removing its Task Scheduler entry
	// too — otherwise the runner will keep firing every minute and just
	// no-op out, which wastes CPU and pollutes logs.
	if !enabled && HasBackgroundTask(id) {
		RemoveBackgroundTask(id)
	}
	if enabled {
		if err := m.scheduleLocked(j); err != nil {
			return err
		}
	}
	return m.persistLocked()
}

// RunNow executes a job out-of-band without affecting its schedule. Useful
// for testing from the UI ("run this once").
func (m *Manager) RunNow(id string) error {
	m.mu.RLock()
	j, ok := m.jobs[id]
	m.mu.RUnlock()
	if !ok {
		return i18n.Errorf("errors.cron.jobNotFound", map[string]any{"id": id})
	}
	go m.execute(j)
	return nil
}

func (m *Manager) scheduleLocked(j *Job) error {
	// BackgroundMode jobs are owned by Task Scheduler — don't double-fire
	// from the in-process engine. We still validate the schedule via the
	// parser at Save() time, so the user gets the same error UX.
	if j.BackgroundMode {
		if m.exePath != "" {
			if err := EnsureBackgroundTask(j.ID, m.exePath); err != nil {
				return i18n.Wrapf(err, "errors.cron.registerTaskFailed", nil)
			}
		}
		return nil
	}
	eid, err := m.cron.AddFunc(j.Schedule, func() { m.execute(j) })
	if err != nil {
		return i18n.Wrapf(err, "errors.cron.scheduleFailed", nil)
	}
	m.entries[j.ID] = eid
	return nil
}

func (m *Manager) persistLocked() error {
	if m.persist == nil {
		return nil
	}
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, *j)
	}
	return m.persist(out)
}

// execute runs one job synchronously. Captures combined stdout/stderr,
// truncates to maxOutputBytes, persists the result, and emits an event so
// the UI can refresh without polling.
func (m *Manager) execute(j *Job) {
	m.markStatus(j.ID, "running", "", "")

	workDir := j.WorkingDir
	if m.workDirFor != nil {
		workDir = m.workDirFor(j.ProjectName, j.WorkingDir)
	}
	m.mu.RLock()
	validator := m.validateRuntime
	runtimePathFor := m.runtimePathFor
	resolveEnvironment := m.environmentFor
	m.mu.RUnlock()
	if validator != nil {
		if err := validator(j.ProjectName, workDir, j.PhpVersion); err != nil {
			m.markStatus(j.ID, "error", "", err.Error())
			return
		}
	}

	var extra []string
	if runtimePathFor != nil {
		var err error
		extra, err = runtimePathFor(j.ProjectName, workDir, j.PhpVersion)
		if err != nil {
			m.markStatus(j.ID, "error", "", err.Error())
			return
		}
	} else if m.pathFor != nil {
		extra = m.pathFor(j.ProjectName, j.PhpVersion)
	}

	cmdLine := j.Command
	cmd := exec.Command("cmd.exe", "/C", cmdLine)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}

	cmd.Dir = workDir
	cmd.Env = os.Environ()
	if len(extra) > 0 {
		cmd.Env = augmentPATH(extra)
	}
	if resolveEnvironment != nil {
		var err error
		cmd.Env, err = resolveEnvironment(j.ProjectName, workDir, cmd.Env)
		if err != nil {
			m.markStatus(j.ID, "error", "", err.Error())
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	doneCh := make(chan error, 1)
	var output []byte
	var runErr error

	go func() {
		out, err := cmd.CombinedOutput()
		output = out
		doneCh <- err
	}()
	select {
	case runErr = <-doneCh:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		runErr = i18n.Errorf("errors.cron.executionTimedOut", nil)
		<-doneCh
	}

	out := output
	if len(out) > maxOutputBytes {
		out = out[len(out)-maxOutputBytes:]
	}
	status := "ok"
	errMsg := ""
	if runErr != nil {
		status = "error"
		errMsg = runErr.Error()
	}
	m.markStatus(j.ID, status, string(out), errMsg)
}

func (m *Manager) markStatus(id, status, out, errMsg string) {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	if status != "running" {
		j.LastRun = time.Now()
	}
	j.LastStatus = status
	if status != "running" {
		j.LastOutput = out
		j.LastError = errMsg
	}
	snap := *j
	emit := m.emit
	_ = m.persistLocked()
	m.mu.Unlock()
	if emit != nil {
		emit(snap)
	}
}

package pty

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// TerminalIDEnv is set in every PTY LaraEnv spawns. Provider lifecycle hooks
// installed by LaraEnv use it to recognise terminals the app owns; a process
// started anywhere else never carries it.
const TerminalIDEnv = "LARAENV_TERMINAL_ID"

const AgentOwnerEnv = "LARAENV_AGENT_OWNER"

// How long watchExit lets the read loop drain after the hosted process exits,
// before closing the session out from under it.
const exitDrainGrace = 100 * time.Millisecond

// Registry manages multiple PTY sessions identified by string IDs.
type Registry struct {
	mu               sync.Mutex
	sessions         map[string]Session
	counter          atomic.Int64
	ownerID          string
	activityObserver func(terminalID string, live bool)
	environmentFor   func(cwd string, env []string) ([]string, error)
}

func (r *Registry) SetEnvironmentResolver(resolver func(string, []string) ([]string, error)) {
	r.mu.Lock()
	r.environmentFor = resolver
	r.mu.Unlock()
}

func (r *Registry) checkoutEnvironment(id string, dirs []string, overrides map[string]string, cwd string) ([]string, error) {
	env := r.sessionEnvironment(id, dirs, overrides)
	r.mu.Lock()
	resolver := r.environmentFor
	r.mu.Unlock()
	if resolver == nil {
		return env, nil
	}
	return resolver(cwd, env)
}

func NewRegistry() *Registry {
	return &Registry{sessions: make(map[string]Session)}
}

func (r *Registry) SetOwnerID(ownerID string) {
	r.mu.Lock()
	r.ownerID = strings.TrimSpace(ownerID)
	r.mu.Unlock()
}

// SetActivityTerminalObserver publishes actual local PTY membership. Callbacks
// run outside the registry lock and may query the registry themselves.
func (r *Registry) SetActivityTerminalObserver(observer func(terminalID string, live bool)) {
	r.mu.Lock()
	r.activityObserver = observer
	r.mu.Unlock()
}

func (r *Registry) sessionEnvironment(id string, dirs []string, overrides map[string]string) []string {
	r.mu.Lock()
	ownerID := r.ownerID
	r.mu.Unlock()
	return applyEnvOverrides(sessionEnv(id, dirs, overrides), map[string]string{AgentOwnerEnv: ownerID})
}

func (r *Registry) nextID() string { return fmt.Sprintf("term-%d", r.counter.Add(1)) }

// sessionEnv builds the environment for a new PTY: the live PATH plus
// extraPathDirs, caller overrides, and finally the terminal id (applied last
// so an override can never spoof it).
func sessionEnv(id string, extraPathDirs []string, overrides map[string]string) []string {
	env := applyEnvOverrides(buildEnv(extraPathDirs), overrides)
	return applyEnvOverrides(env, map[string]string{TerminalIDEnv: id})
}

// New creates a fresh PTY session and stores it under a generated ID.
// extraPathDirs are prepended to the spawned shell's PATH (e.g. PHP bin).
// cwd is the initial working directory; "" lets the OS pick.
func (r *Registry) New(shell string, cols, rows int, extraPathDirs []string, cwd string) (string, Session, error) {
	id := r.nextID()
	env, err := r.checkoutEnvironment(id, extraPathDirs, nil, cwd)
	if err != nil {
		return "", nil, err
	}
	s, err := Start(shell, cols, rows, env, cwd)
	if err != nil {
		return "", nil, err
	}
	r.mu.Lock()
	r.sessions[id] = s
	observer := r.activityObserver
	r.mu.Unlock()
	if observer != nil {
		observer(id, true)
	}
	r.watchExit(id, s)
	return id, s, nil
}

// NewCommand starts an executable with an argument vector without involving a
// shell. This is the safe path for agent CLIs because project paths and
// provider session IDs never need shell escaping in frontend code.
func (r *Registry) NewCommand(executable string, args []string, cols, rows int, extraPathDirs []string, cwd string, envOverrides map[string]string) (string, Session, error) {
	id := r.nextID()
	env, err := r.checkoutEnvironment(id, extraPathDirs, envOverrides, cwd)
	if err != nil {
		return "", nil, err
	}
	s, err := StartCommand(executable, args, cols, rows, env, cwd)
	if err != nil {
		return "", nil, err
	}
	r.mu.Lock()
	r.sessions[id] = s
	observer := r.activityObserver
	r.mu.Unlock()
	if observer != nil {
		observer(id, true)
	}
	r.watchExit(id, s)
	return id, s, nil
}

// applyEnvOverrides updates environment keys case-insensitively. An empty
// override removes the key, which is needed for opt-out variables such as
// NO_COLOR that affect a command merely by being present.
func applyEnvOverrides(env []string, overrides map[string]string) []string {
	type override struct {
		key   string
		value string
	}
	normalized := make(map[string]override, len(overrides))
	for key, value := range overrides {
		key = strings.TrimSpace(key)
		if key != "" && !strings.Contains(key, "=") {
			normalized[strings.ToLower(key)] = override{key: key, value: value}
		}
	}
	if len(normalized) == 0 {
		return env
	}

	out := make([]string, 0, len(env)+len(normalized))
	applied := make(map[string]bool, len(normalized))
	for _, entry := range env {
		eq := strings.IndexByte(entry, '=')
		if eq <= 0 {
			out = append(out, entry)
			continue
		}
		key := strings.ToLower(entry[:eq])
		value, ok := normalized[key]
		if !ok {
			out = append(out, entry)
			continue
		}
		if !applied[key] && value.value != "" {
			out = append(out, value.key+"="+value.value)
		}
		applied[key] = true
	}

	keys := make([]string, 0, len(normalized))
	for key := range normalized {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := normalized[key]
		if !applied[key] && value.value != "" {
			out = append(out, value.key+"="+value.value)
		}
	}
	return out
}

// Register inserts a pre-built Session (e.g. an SSH session that wasn't
// created via Start) and returns its generated id.
func (r *Registry) Register(s Session) string {
	id := fmt.Sprintf("term-%d", r.counter.Add(1))
	r.mu.Lock()
	r.sessions[id] = s
	observer := r.activityObserver
	r.mu.Unlock()
	if observer != nil {
		observer(id, true)
	}
	return id
}

// watchExit closes and unregisters a session once the process behind it exits,
// for sessions that cannot signal it through their reads (see Waiter). Closing
// is what unblocks the read loop, which is what emits term:exit:<id> and lets
// the pane close itself.
func (r *Registry) watchExit(id string, s Session) {
	w, ok := s.(Waiter)
	if !ok {
		return
	}
	go func() {
		_ = w.Wait()
		// Wait returns the moment the process does, which can be before the
		// read loop has drained what it wrote on the way out — closing
		// immediately would throw away the last chunk of output, so a shell's
		// farewell line or a command's final rows could vanish. The loop reads
		// continuously while the process runs, so only that last chunk is ever
		// in flight and this is far longer than draining it takes.
		time.Sleep(exitDrainGrace)
		// Through the registry, not s.Close(), so the id is dropped too: a
		// later StopPty finds nothing and CloseAll cannot close it twice.
		_ = r.Close(id)
	}()
}

func (r *Registry) Get(id string) (Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	return s, ok
}

func (r *Registry) Close(id string) error {
	r.mu.Lock()
	s, ok := r.sessions[id]
	delete(r.sessions, id)
	observer := r.activityObserver
	r.mu.Unlock()
	if !ok {
		return nil
	}
	if observer != nil {
		observer(id, false)
	}
	return s.Close()
}

func (r *Registry) CloseAll() {
	r.mu.Lock()
	all := r.sessions
	r.sessions = make(map[string]Session)
	observer := r.activityObserver
	r.mu.Unlock()
	for id, s := range all {
		if observer != nil {
			observer(id, false)
		}
		_ = s.Close()
	}
}

// buildEnv copies the current process env and rebuilds PATH so the spawned
// shell sees (a) tools installed after LaraEnv launched and (b) our managed
// runtime dirs. The process env PATH is only a snapshot from launch time, so on
// Windows we re-read the live machine+user PATH from the registry and merge it
// with the snapshot; extraPathDirs (php/node/composer) are then prepended.
func buildEnv(extraPathDirs []string) []string {
	env := os.Environ()
	pathKey := "PATH"
	pathSep := string(os.PathListSeparator)

	// Current process PATH value (case-insensitive key match for Windows).
	curPath := ""
	for _, e := range env {
		if eq := strings.IndexByte(e, '='); eq > 0 && strings.EqualFold(e[:eq], pathKey) {
			curPath = e[eq+1:]
			break
		}
	}

	base := mergePath(livePath(), curPath, pathSep)
	newPath := base
	if len(extraPathDirs) > 0 {
		newPath = strings.Join(extraPathDirs, pathSep) + pathSep + base
	}

	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, e := range env {
		if eq := strings.IndexByte(e, '='); eq > 0 && strings.EqualFold(e[:eq], pathKey) {
			out = append(out, pathKey+"="+newPath)
			replaced = true
		} else {
			out = append(out, e)
		}
	}
	if !replaced {
		out = append(out, pathKey+"="+newPath)
	}
	return out
}

// mergePath concatenates primary then secondary PATH lists, dropping empty and
// case-insensitively duplicated entries (preserving first-seen order). primary
// (the live registry PATH) wins; secondary (the process snapshot) backfills
// anything not already present so nothing the app added is lost.
func mergePath(primary, secondary, sep string) string {
	if primary == "" {
		return secondary
	}
	seen := make(map[string]bool)
	var out []string
	add := func(list string) {
		for _, p := range strings.Split(list, sep) {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			key := strings.ToLower(p)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, p)
		}
	}
	add(primary)
	add(secondary)
	return strings.Join(out, sep)
}

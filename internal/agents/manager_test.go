package agents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"lara4ever/internal/i18n"
	"lara4ever/internal/pty"
)

// TestMain loads the real catalogues before running this package's tests, so
// an *i18n.Error's message renders the actual English prose (with vars
// interpolated) instead of degrading to a bare key path — the same nil-Global
// fallback a package would hit in production before LoadCatalogues runs, but
// not what a test asserting on error text wants to see. See
// internal/services/manager_test.go and internal/vhost/nginx_conf_test.go for
// the same pattern.
func TestMain(m *testing.M) {
	set, err := i18n.Load(os.DirFS(filepath.Join("..", "..", "langs")), ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "load langs:", err)
		os.Exit(1)
	}
	i18n.SetGlobal(set)
	os.Exit(m.Run())
}

type fakeAgentSession struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	input  bytes.Buffer
	mu     sync.Mutex
}

func newFakeAgentSession() *fakeAgentSession {
	reader, writer := io.Pipe()
	return &fakeAgentSession{reader: reader, writer: writer}
}

func (s *fakeAgentSession) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s *fakeAgentSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.input.Write(p)
}
func (s *fakeAgentSession) Close() error {
	_ = s.writer.Close()
	return s.reader.Close()
}
func (s *fakeAgentSession) Resize(int, int) error { return nil }

type fakeAgentRegistry struct {
	mu         sync.Mutex
	session    *fakeAgentSession
	executable string
	args       []string
	cwd        string
	env        map[string]string
}

func (r *fakeAgentRegistry) NewCommand(executable string, args []string, _, _ int, _ []string, cwd string, env map[string]string) (string, pty.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executable = executable
	r.args = append([]string(nil), args...)
	r.cwd = cwd
	r.env = make(map[string]string, len(env))
	for key, value := range env {
		r.env[key] = value
	}
	return "term-test", r.session, nil
}
func (r *fakeAgentRegistry) Get(string) (pty.Session, bool) { return r.session, true }
func (r *fakeAgentRegistry) Close(string) error             { return r.session.Close() }

func TestManagerLaunchRecordsMetadataAndOutput(t *testing.T) {
	store, err := NewSessionStore(t.TempDir()+"/sessions.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog := newCatalog(
		func(name string) (string, error) {
			if name == "codex.exe" {
				return `C:\tools\codex.exe`, nil
			}
			return "", errors.New("not found")
		},
		func(context.Context, string) (string, error) { return "0.149.0", nil },
		catalogDirs{},
	)
	terminalSession := newFakeAgentSession()
	registry := &fakeAgentRegistry{session: terminalSession}
	events := make(chan string, 20)
	manager := NewManager(
		catalog, store, registry,
		func(options AgentLaunchOptions) (string, string, string, error) {
			return `C:\projects\demo`, "main", "", nil
		},
		func(string) []string { return []string{`C:\php`} },
		func(event string, _ ...any) { events <- event },
	)

	session, err := manager.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	if session.ProjectPath != `C:\projects\demo` || registry.cwd != session.ProjectPath {
		t.Fatalf("project path changed during launch: session=%q cwd=%q", session.ProjectPath, registry.cwd)
	}
	if registry.executable != `C:\tools\codex.exe` || len(registry.args) != 0 {
		t.Fatalf("unexpected invocation: %q %#v", registry.executable, registry.args)
	}
	if registry.env["TERM"] != "xterm-256color" || registry.env["COLORTERM"] != "truecolor" || registry.env["NO_COLOR"] != "" {
		t.Fatalf("unexpected agent terminal environment: %#v", registry.env)
	}
	if registry.env[AgentSessionEnv] != session.ID {
		t.Fatalf("launched terminal must carry its LaraEnv session id, got %#v", registry.env)
	}
	if _, err := terminalSession.writer.Write([]byte("\x1b[31mworking\x1b[0m")); err != nil {
		t.Fatal(err)
	}
	waitForAgentState(t, store, session.ID, SessionWorking)
	got, _ := store.Get(session.ID)
	if got.LastPreview != "working" {
		t.Fatalf("preview = %q, want working", got.LastPreview)
	}
	if err := manager.Stop(session.ID); err != nil {
		t.Fatal(err)
	}
	waitForAgentState(t, store, session.ID, SessionStopped)
}

func TestManagerCapturesExactlyOneNewProviderSessionID(t *testing.T) {
	manager, store, terminalSession := newDiscoveryTestManager(t)
	var calls int
	manager.discover = func(context.Context, AgentProfile, string) (map[string]struct{}, error) {
		calls++
		if calls == 1 {
			return sessionIDSet("existing"), nil
		}
		return sessionIDSet("existing", "new-provider-session"), nil
	}
	manager.captureDelay = time.Millisecond
	manager.captureAttempts = 3

	session, err := manager.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	waitForProviderSessionID(t, store, session.ID, "new-provider-session")
	if err := manager.Stop(session.ID); err != nil {
		t.Fatal(err)
	}
	waitForAgentState(t, store, session.ID, SessionStopped)
	_ = terminalSession
}

func TestManagerNeverGuessesAmbiguousProviderSessionID(t *testing.T) {
	manager, store, _ := newDiscoveryTestManager(t)
	var calls int
	manager.discover = func(context.Context, AgentProfile, string) (map[string]struct{}, error) {
		calls++
		if calls == 1 {
			return sessionIDSet("existing"), nil
		}
		return sessionIDSet("existing", "candidate-a", "candidate-b"), nil
	}
	manager.captureDelay = time.Millisecond
	manager.captureAttempts = 2

	session, err := manager.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	got, _ := store.Get(session.ID)
	if got.ProviderSessionID != "" {
		t.Fatalf("ambiguous provider session was guessed as %q", got.ProviderSessionID)
	}
	if err := manager.Stop(session.ID); err != nil {
		t.Fatal(err)
	}
	waitForAgentState(t, store, session.ID, SessionStopped)
}

func TestManagerBlocksMetadataResetWhileSessionIsLive(t *testing.T) {
	manager, store, _ := newDiscoveryTestManager(t)
	manager.discover = nil
	session, err := manager.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ResetMetadata(); err == nil {
		t.Fatal("metadata reset should be blocked while a session is live")
	}
	if _, ok := store.Get(session.ID); !ok {
		t.Fatal("blocked reset removed the live session")
	}
	if err := manager.Stop(session.ID); err != nil {
		t.Fatal(err)
	}
	waitForAgentState(t, store, session.ID, SessionStopped)
}

func TestManagerAppliesVerifiedLifecycleSignals(t *testing.T) {
	manager, store, _ := newDiscoveryTestManager(t)
	manager.discover = nil
	session, err := manager.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}

	manager.ObserveSignal(SessionSignal{SessionID: session.ID, Kind: SignalNeedsInput, CreatedAt: time.Now().UTC()})
	got, _ := store.Get(session.ID)
	if got.State != SessionNeedsInput || !got.Unread {
		t.Fatalf("needs-input signal produced %#v", got)
	}
	if _, err := manager.MarkRead(session.ID); err != nil {
		t.Fatal(err)
	}
	manager.SetFocused(session.ID)
	manager.ObserveSignal(SessionSignal{SessionID: session.ID, Kind: SignalDone, CreatedAt: time.Now().UTC()})
	got, _ = store.Get(session.ID)
	if got.State != SessionDone || got.Unread {
		t.Fatalf("focused done signal produced %#v", got)
	}

	if err := manager.Stop(session.ID); err != nil {
		t.Fatal(err)
	}
	waitForAgentState(t, store, session.ID, SessionStopped)
}

func TestManagerReconcilesPersistedTerminalAsStopped(t *testing.T) {
	store, err := NewSessionStore(t.TempDir()+"/sessions.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(AgentSession{
		ID: "stale", ProjectName: "demo", ProjectPath: `C:\projects\demo`, AgentID: AgentCodex,
		TerminalID: "term-from-previous-process", State: SessionDone, Title: "stale terminal",
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(nil, store, nil, nil, nil, nil)
	got, ok := manager.GetSession("stale")
	if !ok || got.State != SessionStopped || got.TerminalID != "" {
		t.Fatalf("persisted runtime was not reconciled: %#v", got)
	}
}

func TestManagerRepairsBenignInvalidHandleExit(t *testing.T) {
	store, err := NewSessionStore(t.TempDir()+"/sessions.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(AgentSession{
		ID: "closed", ProjectName: "demo", ProjectPath: `C:\projects\demo`, AgentID: AgentClaude,
		State: SessionError, ExitError: "The handle is invalid.", Title: "closed terminal",
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(nil, store, nil, nil, nil, nil)
	got, ok := manager.GetSession("closed")
	if !ok || got.State != SessionStopped || got.ExitError != "" {
		t.Fatalf("benign ConPTY close was not repaired: %#v", got)
	}
}

func TestManagerRejectsMissingProviderConversationBeforeSpawningTerminal(t *testing.T) {
	store, err := NewSessionStore(t.TempDir()+"/sessions.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(AgentSession{
		ID: "lara-session", ProjectName: "demo", ProjectPath: `C:\projects\demo`, AgentID: AgentClaude,
		ProviderSessionID: "missing-provider-session", State: SessionStopped, Title: "Claude Code",
	}); err != nil {
		t.Fatal(err)
	}
	catalog := newCatalog(
		func(name string) (string, error) {
			if name == "claude.exe" {
				return `C:\tools\claude.exe`, nil
			}
			return "", errors.New("not found")
		},
		func(context.Context, string) (string, error) { return "2.1.261", nil },
		catalogDirs{},
	)
	registry := &fakeAgentRegistry{session: newFakeAgentSession()}
	workspaceCalled := false
	manager := NewManager(
		catalog, store, registry,
		func(AgentLaunchOptions) (string, string, string, error) {
			workspaceCalled = true
			return `C:\projects\demo`, "main", "", nil
		},
		func(string) []string { return nil },
		func(string, ...any) {},
	)
	manager.discover = func(context.Context, AgentProfile, string) (map[string]struct{}, error) {
		return sessionIDSet("another-session"), nil
	}

	if _, err := manager.Resume(context.Background(), "lara-session"); err == nil || !strings.Contains(err.Error(), "provider conversation is unavailable") {
		t.Fatalf("resume error = %v, want unavailable provider conversation", err)
	}
	if registry.executable != "" {
		t.Fatalf("provider terminal spawned unexpectedly: %s %#v", registry.executable, registry.args)
	}
	if workspaceCalled {
		t.Fatal("workspace resolver ran before provider history validation")
	}
	got, _ := store.Get("lara-session")
	if got.State != SessionError || got.TerminalID != "" || !strings.Contains(got.ExitError, "provider conversation is unavailable") {
		t.Fatalf("missing provider conversation was not persisted as an error: %#v", got)
	}
}

func newDiscoveryTestManager(t *testing.T) (*Manager, *SessionStore, *fakeAgentSession) {
	t.Helper()
	store, err := NewSessionStore(t.TempDir()+"/sessions.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog := newCatalog(
		func(name string) (string, error) {
			if name == "codex.exe" {
				return `C:\tools\codex.exe`, nil
			}
			return "", errors.New("not found")
		},
		func(context.Context, string) (string, error) { return "0.149.0", nil },
		catalogDirs{},
	)
	terminalSession := newFakeAgentSession()
	manager := NewManager(
		catalog,
		store,
		&fakeAgentRegistry{session: terminalSession},
		func(AgentLaunchOptions) (string, string, string, error) {
			return `C:\projects\demo`, "main", "", nil
		},
		func(string) []string { return nil },
		func(string, ...any) {},
	)
	return manager, store, terminalSession
}

func TestManagerRestoresAdditionalDirectoriesAfterRestart(t *testing.T) {
	for _, provider := range []AgentID{AgentClaude, AgentCodex, AgentKimi} {
		t.Run(string(provider), func(t *testing.T) {
			manager, store, _ := newDiscoveryTestManager(t)
			manager.discover = nil
			manager.catalog = newCatalog(
				func(name string) (string, error) { return filepath.Join(t.TempDir(), name), nil },
				func(context.Context, string) (string, error) { return "1.0", nil }, catalogDirs{},
			)
			primary, first, second := t.TempDir(), t.TempDir(), t.TempDir()
			manager.workspace = func(AgentLaunchOptions) (string, string, string, error) {
				return primary, "main", "", nil
			}
			options := AgentLaunchOptions{ProjectName: "demo", AgentID: provider, AdditionalDirectories: []string{primary, first, first, second}}
			session, err := manager.Launch(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{first, second}
			if !reflect.DeepEqual(session.AdditionalDirectories, want) {
				t.Fatalf("saved context = %#v, want %#v", session.AdditionalDirectories, want)
			}
			registry := manager.terminals.(*fakeAgentRegistry)
			wantFlags := []string{"--add-dir", first, "--add-dir", second}
			if provider == AgentClaude {
				wantFlags = []string{"--add-dir", first, second}
			}
			if !reflect.DeepEqual(registry.args[len(registry.args)-len(wantFlags):], wantFlags) {
				t.Fatalf("launch arguments = %#v, want context flags %#v", registry.args, wantFlags)
			}
			if err := manager.Stop(session.ID); err != nil {
				t.Fatal(err)
			}
			saved, _ := store.Get(session.ID)
			saved.ProviderSessionID = "provider-session"
			if err := store.Upsert(saved); err != nil {
				t.Fatal(err)
			}
			reloaded, err := NewSessionStore(store.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resumedRegistry := &fakeAgentRegistry{session: newFakeAgentSession()}
			restarted := NewManager(manager.catalog, reloaded, resumedRegistry, manager.workspace, manager.pathPrefixes, nil)
			restarted.discover = nil
			resumed, err := restarted.Resume(context.Background(), session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(resumed.AdditionalDirectories, want) || resumedRegistry.cwd != primary {
				t.Fatalf("resume changed session directories: %#v cwd=%q", resumed, resumedRegistry.cwd)
			}
			if !reflect.DeepEqual(resumedRegistry.args[len(resumedRegistry.args)-len(wantFlags):], wantFlags) {
				t.Fatalf("resume arguments = %#v, want context flags %#v", resumedRegistry.args, wantFlags)
			}
			if err := restarted.Stop(session.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestManagerValidatesAdditionalDirectoriesBeforeWorkspaceChanges(t *testing.T) {
	manager, _, _ := newDiscoveryTestManager(t)
	manager.discover = nil
	workspaceCalls := 0
	manager.workspace = func(AgentLaunchOptions) (string, string, string, error) {
		workspaceCalls++
		return t.TempDir(), "main", "", nil
	}
	_, err := manager.Launch(context.Background(), AgentLaunchOptions{
		ProjectName: "demo", AgentID: AgentCodex, Mode: LaunchWorktree,
		AdditionalDirectories: []string{filepath.Join(t.TempDir(), "missing")},
	})
	if err == nil || !strings.Contains(err.Error(), "additional directory") {
		t.Fatalf("got %v, want missing directory error", err)
	}
	if workspaceCalls != 0 || manager.terminals.(*fakeAgentRegistry).executable != "" {
		t.Fatal("invalid context changed the workspace or spawned a terminal")
	}
}

func TestManagerResumeKeepsMissingContextAndDoesNotSpawn(t *testing.T) {
	manager, store, _ := newDiscoveryTestManager(t)
	manager.discover = nil
	extra := t.TempDir()
	if err := store.Upsert(AgentSession{
		ID: "saved", ProjectName: "demo", ProjectPath: t.TempDir(), AgentID: AgentCodex,
		ProviderSessionID: "provider-session", State: SessionStopped, AdditionalDirectories: []string{extra},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	workspaceCalls := 0
	manager.workspace = func(AgentLaunchOptions) (string, string, string, error) {
		workspaceCalls++
		return t.TempDir(), "main", "", nil
	}
	if _, err := manager.Resume(context.Background(), "saved"); err == nil || !strings.Contains(err.Error(), "additional directory") {
		t.Fatalf("got %v, want missing context error", err)
	}
	if workspaceCalls != 0 || manager.terminals.(*fakeAgentRegistry).executable != "" {
		t.Fatal("resume with missing context changed the workspace or spawned a terminal")
	}
	saved, _ := store.Get("saved")
	if !reflect.DeepEqual(saved.AdditionalDirectories, []string{extra}) {
		t.Fatal("failed resume discarded context metadata")
	}
}

func sessionIDSet(ids ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		result[id] = struct{}{}
	}
	return result
}

func waitForProviderSessionID(t *testing.T, store *SessionStore, id, providerID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if session, ok := store.Get(id); ok && session.ProviderSessionID == providerID {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	session, _ := store.Get(id)
	t.Fatalf("provider session ID = %q, want %q", session.ProviderSessionID, providerID)
}

func waitForAgentState(t *testing.T, store *SessionStore, id string, state SessionState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if session, ok := store.Get(id); ok && session.State == state {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	session, _ := store.Get(id)
	t.Fatalf("session state = %q, want %q", session.State, state)
}

func newAdoptionTestManager(t *testing.T) (*Manager, *SessionStore, chan AgentSession) {
	t.Helper()
	manager, store, _ := newDiscoveryTestManager(t)
	manager.discover = nil
	changes := make(chan AgentSession, 50)
	manager.emit = func(event string, data ...any) {
		if event == "agent:session-changed" && len(data) > 0 {
			if session, ok := data[0].(AgentSession); ok {
				changes <- session
			}
		}
	}
	manager.SetPathResolver(func(cwd string) (ProjectLocation, bool) {
		switch {
		case PathWithin(`C:\laraenv\www\demo`, cwd):
			return ProjectLocation{ProjectName: "demo", ProjectPath: `C:\laraenv\www\demo`, Branch: "main", Mode: LaunchCurrent}, true
		case PathWithin(`C:\laraenv\worktrees\demo-feature`, cwd):
			return ProjectLocation{ProjectName: "demo", ProjectPath: `C:\laraenv\worktrees\demo-feature`, WorktreePath: `C:\laraenv\worktrees\demo-feature`, Branch: "feature", Mode: LaunchWorktree}, true
		}
		return ProjectLocation{}, false
	})
	return manager, store, changes
}

func startedSignal(terminalID, providerSession, cwd string) SessionSignal {
	return SessionSignal{
		Kind: SignalStarted, TerminalID: terminalID, Provider: AgentClaude,
		ProviderSessionID: providerSession, Cwd: cwd, Source: "startup", CreatedAt: time.Now().UTC(),
	}
}

func adoptedSignal(terminalID string, kind SignalKind) SessionSignal {
	return SessionSignal{Kind: kind, TerminalID: terminalID, Provider: AgentClaude, CreatedAt: time.Now().UTC()}
}

func adoptedSignalWithProvider(terminalID string, kind SignalKind, providerSessionID string) SessionSignal {
	signal := adoptedSignal(terminalID, kind)
	signal.ProviderSessionID = providerSessionID
	return signal
}

func TestManagerAdoptsSessionFromLaraEnvTerminal(t *testing.T) {
	manager, store, changes := newAdoptionTestManager(t)
	manager.ObserveSignal(startedSignal("term-9", "provider-1", `C:\laraenv\www\demo\app`))

	session, ok := manager.AdoptedSessionForTerminal("term-9")
	if !ok {
		t.Fatal("adopted session was not created")
	}
	if !session.Adopted || session.AgentID != AgentClaude || session.ProjectName != "demo" || session.ProjectPath != `C:\laraenv\www\demo` ||
		session.GitBranch != "main" || session.LaunchMode != LaunchCurrent || session.TerminalID != "term-9" ||
		session.ProviderSessionID != "provider-1" || session.State != SessionIdle || session.Title != "Claude Code" || session.Unread {
		t.Fatalf("unexpected adopted session: %#v", session)
	}
	if !manager.HasLiveSessions() {
		t.Fatal("adopted session must count as live")
	}
	if len(changes) == 0 {
		t.Fatal("adoption did not emit agent:session-changed")
	}

	manager.ObserveSignal(adoptedSignal("term-9", SignalWorking))
	got, _ := store.Get(session.ID)
	if got.State != SessionWorking {
		t.Fatalf("working signal produced %q", got.State)
	}
	manager.ObserveSignal(adoptedSignal("term-9", SignalNeedsInput))
	got, _ = store.Get(session.ID)
	if got.State != SessionNeedsInput || !got.Unread {
		t.Fatalf("needs-input signal produced %#v", got)
	}
	manager.SetFocused(session.ID)
	manager.ObserveSignal(adoptedSignal("term-9", SignalDone))
	got, _ = store.Get(session.ID)
	if got.State != SessionDone {
		t.Fatalf("done signal produced %#v", got)
	}

	manager.ObserveSignal(adoptedSignal("term-9", SignalEnded))
	got, _ = store.Get(session.ID)
	if got.State != SessionStopped || got.TerminalID != "" || manager.HasLiveSessions() {
		t.Fatalf("ended signal produced %#v (live=%v)", got, manager.HasLiveSessions())
	}
	if _, stillBound := manager.AdoptedSessionForTerminal("term-9"); stillBound {
		t.Fatal("terminal binding survived the end of the session")
	}
}

func TestManagerIgnoresAdoptionOutsideKnownProjects(t *testing.T) {
	manager, store, _ := newAdoptionTestManager(t)
	manager.ObserveSignal(startedSignal("term-10", "provider-2", `C:\Users\developer\Downloads`))
	if sessions := store.List("", true); len(sessions) != 0 {
		t.Fatalf("session was adopted outside a project: %#v", sessions)
	}
	manager.ObserveSignal(adoptedSignal("term-10", SignalDone))
	if manager.HasLiveSessions() {
		t.Fatal("stray signals must not create runtime entries")
	}
	// No resolver at all: nothing is adopted.
	manager.SetPathResolver(nil)
	manager.ObserveSignal(startedSignal("term-11", "provider-3", `C:\laraenv\www\demo`))
	if sessions := store.List("", true); len(sessions) != 0 {
		t.Fatalf("session was adopted without a resolver: %#v", sessions)
	}
}

func TestManagerAdoptsWorktreeLocation(t *testing.T) {
	manager, _, _ := newAdoptionTestManager(t)
	manager.ObserveSignal(startedSignal("term-12", "provider-4", `C:\laraenv\worktrees\demo-feature`))
	session, ok := manager.AdoptedSessionForTerminal("term-12")
	if !ok || session.LaunchMode != LaunchWorktree || session.WorktreePath != `C:\laraenv\worktrees\demo-feature` || session.GitBranch != "feature" || session.ProjectName != "demo" {
		t.Fatalf("worktree adoption produced %#v", session)
	}
}

func TestManagerReattachesResumedProviderSession(t *testing.T) {
	manager, store, _ := newAdoptionTestManager(t)
	manager.ObserveSignal(startedSignal("term-13", "provider-5", `C:\laraenv\www\demo`))
	first, _ := manager.AdoptedSessionForTerminal("term-13")
	manager.ObserveSignal(adoptedSignal("term-13", SignalEnded))

	resumed := startedSignal("term-14", "provider-5", `C:\laraenv\www\demo`)
	resumed.Source = "resume"
	manager.ObserveSignal(resumed)
	second, ok := manager.AdoptedSessionForTerminal("term-14")
	if !ok || second.ID != first.ID || second.State != SessionIdle || second.TerminalID != "term-14" {
		t.Fatalf("resume did not reuse the session: first=%#v second=%#v", first, second)
	}
	if sessions := store.List("", true); len(sessions) != 1 {
		t.Fatalf("resume created a duplicate: %#v", sessions)
	}
}

func TestManagerReplacesSessionWhenTerminalStartsAnotherOne(t *testing.T) {
	manager, store, _ := newAdoptionTestManager(t)
	manager.ObserveSignal(startedSignal("term-15", "provider-6", `C:\laraenv\www\demo`))
	first, _ := manager.AdoptedSessionForTerminal("term-15")
	// e.g. /clear in Claude starts a new provider session in the same terminal.
	manager.ObserveSignal(startedSignal("term-15", "provider-7", `C:\laraenv\www\demo`))
	second, _ := manager.AdoptedSessionForTerminal("term-15")
	if second.ID == first.ID || second.ProviderSessionID != "provider-7" {
		t.Fatalf("new provider session was not adopted: %#v", second)
	}
	old, _ := store.Get(first.ID)
	if old.State != SessionStopped || old.TerminalID != "" {
		t.Fatalf("previous session in the terminal was not stopped: %#v", old)
	}
}

func TestManagerDoesNotReuseSessionLiveInAnotherTerminal(t *testing.T) {
	manager, store, _ := newAdoptionTestManager(t)
	manager.ObserveSignal(startedSignal("term-20", "provider-shared", `C:\laraenv\www\demo`))
	first, _ := manager.AdoptedSessionForTerminal("term-20")

	manager.ObserveSignal(startedSignal("term-21", "provider-shared", `C:\laraenv\www\demo`))
	second, ok := manager.AdoptedSessionForTerminal("term-21")
	if !ok || second.ID == first.ID {
		t.Fatalf("live session was reused across terminals: first=%#v second=%#v", first, second)
	}
	still, ok := manager.AdoptedSessionForTerminal("term-20")
	if !ok || still.ID != first.ID || still.TerminalID != "term-20" {
		t.Fatalf("first terminal lost its session: %#v", still)
	}
	manager.TerminalClosed("term-20")
	got, _ := store.Get(first.ID)
	if got.State != SessionStopped {
		t.Fatalf("closing the first terminal did not stop its session: %#v", got)
	}
	if _, live := manager.AdoptedSessionForTerminal("term-21"); !live {
		t.Fatal("second terminal's session was affected by closing the first")
	}
}

func TestManagerTerminalClosedStopsAdoptedSession(t *testing.T) {
	manager, store, _ := newAdoptionTestManager(t)
	manager.ObserveSignal(startedSignal("term-16", "provider-8", `C:\laraenv\www\demo`))
	session, _ := manager.AdoptedSessionForTerminal("term-16")
	manager.TerminalClosed("term-16")
	got, _ := store.Get(session.ID)
	if got.State != SessionStopped || got.TerminalID != "" || manager.HasLiveSessions() {
		t.Fatalf("closing the terminal did not stop the session: %#v", got)
	}
	manager.TerminalClosed("term-does-not-exist") // must be a no-op
}

// TestManagerIgnoresEndedForReplacedProviderSession covers a /clear race:
// Claude fires SessionEnd(old) and SessionStart(new) for the same terminal;
// if the hook processes race so started(new) lands first, the late
// ended(old) must not stop (and orphan) the session that already replaced
// it in that terminal.
func TestManagerIgnoresEndedForReplacedProviderSession(t *testing.T) {
	manager, store, _ := newAdoptionTestManager(t)
	manager.ObserveSignal(startedSignal("term-30", "p1", `C:\laraenv\www\demo`))
	first, _ := manager.AdoptedSessionForTerminal("term-30")
	manager.ObserveSignal(startedSignal("term-30", "p2", `C:\laraenv\www\demo`))
	second, ok := manager.AdoptedSessionForTerminal("term-30")
	if !ok || second.ID == first.ID || second.ProviderSessionID != "p2" {
		t.Fatalf("term-30 did not adopt the new provider session: %#v", second)
	}

	// A stale SessionEnd for p1 arrives after p2 already replaced it.
	manager.ObserveSignal(adoptedSignalWithProvider("term-30", SignalEnded, "p1"))

	stillSecond, ok := manager.AdoptedSessionForTerminal("term-30")
	if !ok || stillSecond.ID != second.ID || stillSecond.State == SessionStopped {
		t.Fatalf("stale ended(p1) affected the replacement session: %#v", stillSecond)
	}
	if !manager.HasLiveSessions() {
		t.Fatal("stale ended(p1) must not orphan the live replacement session")
	}

	// A genuine ended(p2) still stops it.
	manager.ObserveSignal(adoptedSignalWithProvider("term-30", SignalEnded, "p2"))
	got, _ := store.Get(second.ID)
	if got.State != SessionStopped || got.TerminalID != "" {
		t.Fatalf("ended(p2) did not stop the current session: %#v", got)
	}
}

// TestManagerIgnoresStateSignalForReplacedProviderSession covers the same
// /clear race for the working/done/needs-input/error signals, not just ended.
func TestManagerIgnoresStateSignalForReplacedProviderSession(t *testing.T) {
	manager, store, _ := newAdoptionTestManager(t)
	manager.ObserveSignal(startedSignal("term-31", "p1", `C:\laraenv\www\demo`))
	manager.ObserveSignal(startedSignal("term-31", "p2", `C:\laraenv\www\demo`))
	second, ok := manager.AdoptedSessionForTerminal("term-31")
	if !ok || second.ProviderSessionID != "p2" {
		t.Fatalf("term-31 did not adopt the new provider session: %#v", second)
	}

	// A stale "done" for p1 arrives after p2 already replaced it in this terminal.
	manager.ObserveSignal(adoptedSignalWithProvider("term-31", SignalDone, "p1"))
	got, _ := store.Get(second.ID)
	if got.State != SessionIdle {
		t.Fatalf("stale done(p1) changed the replacement session's state: %#v", got)
	}

	// A genuine "done" for p2 still applies.
	manager.ObserveSignal(adoptedSignalWithProvider("term-31", SignalDone, "p2"))
	got, _ = store.Get(second.ID)
	if got.State != SessionDone {
		t.Fatalf("done(p2) did not update the current session: %#v", got)
	}
}

// TestManagerTerminalClosedIgnoresLaunchedSessions confirms TerminalClosed
// (the plain-PTY-exit path) only ever stops an Adopted session, never a
// session Manager.Launch owns.
func TestManagerTerminalClosedIgnoresLaunchedSessions(t *testing.T) {
	manager, store, _ := newDiscoveryTestManager(t)
	manager.discover = nil
	session, err := manager.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}

	manager.TerminalClosed("term-test")

	got, _ := store.Get(session.ID)
	if got.State == SessionStopped || got.TerminalID == "" {
		t.Fatalf("TerminalClosed stopped a launched session: %#v", got)
	}
	if !manager.HasLiveSessions() {
		t.Fatal("launched session must still be live after TerminalClosed")
	}

	if err := manager.Stop(session.ID); err != nil {
		t.Fatal(err)
	}
	waitForAgentState(t, store, session.ID, SessionStopped)
}

func TestManagerResumesAdoptedSessionAsManagedLaunch(t *testing.T) {
	manager, store, _ := newAdoptionTestManager(t)
	manager.catalog = newCatalog(
		func(name string) (string, error) { return filepath.Join(t.TempDir(), name), nil },
		func(context.Context, string) (string, error) { return "1.0", nil }, catalogDirs{},
	)
	manager.workspace = func(options AgentLaunchOptions) (string, string, string, error) {
		return `C:\laraenv\www\demo`, "main", "", nil
	}
	manager.ObserveSignal(startedSignal("term-17", "provider-9", `C:\laraenv\www\demo`))
	session, _ := manager.AdoptedSessionForTerminal("term-17")
	manager.ObserveSignal(adoptedSignal("term-17", SignalEnded))

	resumed, err := manager.Resume(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.TerminalID != "term-test" || resumed.ProviderSessionID != "provider-9" || !resumed.Adopted {
		t.Fatalf("unexpected resumed session: %#v", resumed)
	}
	if err := manager.Stop(session.ID); err != nil {
		t.Fatal(err)
	}
	waitForAgentState(t, store, session.ID, SessionStopped)
}

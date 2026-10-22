package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lara4ever/internal/pty"
)

type runtimeObservation struct {
	kind     string
	run      ActivityRun
	session  AgentSession
	provider string
}

const runtimeTestOwner = "a052e0b7-8599-4224-b3db-5c4b751f1964"

type runtimeRecorder struct {
	mu       sync.Mutex
	events   []runtimeObservation
	callback func()
	changed  chan runtimeObservation
}

func (r *runtimeRecorder) record(e runtimeObservation) {
	if r.callback != nil {
		r.callback()
	}
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	if r.changed != nil {
		r.changed <- e
	}
}
func (r *runtimeRecorder) ReserveRun(run ActivityRun) {
	r.record(runtimeObservation{kind: "reserve", run: run})
}
func (r *runtimeRecorder) BeginRun(run ActivityRun) {
	r.record(runtimeObservation{kind: "begin", run: run})
}
func (r *runtimeRecorder) BindProvider(id, run, provider string) {
	r.record(runtimeObservation{kind: "bind", run: ActivityRun{RunID: run, Session: AgentSession{ID: id}}, provider: provider})
}
func (r *runtimeRecorder) RootChanged(s AgentSession, run string) {
	r.record(runtimeObservation{kind: "root", run: ActivityRun{RunID: run}, session: s})
}
func (r *runtimeRecorder) EndRun(id, run string, _ time.Time) {
	r.record(runtimeObservation{kind: "end", run: ActivityRun{RunID: run, Session: AgentSession{ID: id}}})
}
func (r *runtimeRecorder) Reset() { r.record(runtimeObservation{kind: "reset"}) }
func (r *runtimeRecorder) all(kind string) []runtimeObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []runtimeObservation
	for _, e := range r.events {
		if e.kind == kind {
			out = append(out, e)
		}
	}
	return out
}

type runtimeRegistry struct {
	mu       sync.Mutex
	sessions map[string]*fakeAgentSession
	envs     []map[string]string
	fail     bool
	onSpawn  func(map[string]string)
}

func (r *runtimeRegistry) NewCommand(_ string, _ []string, _, _ int, _ []string, _ string, env map[string]string) (string, pty.Session, error) {
	if r.onSpawn != nil {
		r.onSpawn(env)
	}
	if r.fail {
		return "", nil, errors.New("synthetic spawn failure")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions == nil {
		r.sessions = make(map[string]*fakeAgentSession)
	}
	id := fmt.Sprintf("term-%d", len(r.sessions)+1)
	s := newFakeAgentSession()
	r.sessions[id] = s
	r.envs = append(r.envs, env)
	return id, s, nil
}
func (r *runtimeRegistry) Get(id string) (pty.Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	return s, ok
}
func (r *runtimeRegistry) Close(id string) error {
	s, ok := r.Get(id)
	if ok {
		return s.Close()
	}
	return nil
}

func activityRuntimeManager(t *testing.T) (*Manager, *SessionStore, *runtimeRegistry, *runtimeRecorder) {
	t.Helper()
	m, store, _ := newDiscoveryTestManager(t)
	r := &runtimeRegistry{}
	m.terminals = r
	m.discover = nil
	o := &runtimeRecorder{changed: make(chan runtimeObservation, 100)}
	m.SetActivityObserver(o, runtimeTestOwner)
	t.Cleanup(func() {
		// Stop synchronously before TempDir cleanup; merely closing the fake PTY
		// leaves its pump racing a final SessionStore write with RemoveAll.
		for _, session := range store.List("", true) {
			_ = m.Stop(session.ID)
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, s := range r.sessions {
			_ = s.Close()
		}
	})
	return m, store, r, o
}

func TestActivityRuntimeLaunchResumeGenerations(t *testing.T) {
	m, store, r, o := activityRuntimeManager(t)
	r.onSpawn = func(env map[string]string) {
		reserved := o.all("reserve")
		if len(reserved) == 0 || reserved[len(reserved)-1].run.RunID != env["LARAENV_AGENT_RUN"] {
			t.Fatal("spawn was not reserved")
		}
		if reserved[len(reserved)-1].run.Session.TerminalID != "" {
			t.Error("reservation claims live terminal")
		}
	}
	s, err := m.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	previous := o.all("begin")[0].run
	if previous.RunID == "" || previous.OwnerID != runtimeTestOwner || previous.Session.TerminalID != s.TerminalID {
		t.Fatal(previous)
	}
	if err = m.Stop(s.ID); err != nil {
		t.Fatal(err)
	}
	s, _ = store.Get(s.ID)
	s.ProviderSessionID = "provider-new"
	if err = store.Upsert(s); err != nil {
		t.Fatal(err)
	}
	s, err = m.Resume(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := o.all("begin")[1].run
	if next.RunID == previous.RunID {
		t.Fatal("generation reused")
	}
	if next.Session.TerminalID == previous.Session.TerminalID {
		t.Fatal("terminal reused")
	}
	if len(o.all("end")) != 1 {
		t.Fatal("run was not ended exactly once")
	}
	if _, err = m.Rename(s.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.MarkRead(s.ID); err != nil {
		t.Fatal(err)
	}
	if len(o.all("begin")) != 2 {
		t.Fatal("metadata update started a generation")
	}
	roots := o.all("root")
	if len(roots) < 2 || roots[len(roots)-1].run.RunID != next.RunID {
		t.Fatal("root metadata not observed")
	}
}

func TestActivityRuntimeOldDiscoveryCannotBindNewRun(t *testing.T) {
	m, store, _, o := activityRuntimeManager(t)
	started, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s, err := m.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	previous := o.all("begin")[0].run
	m.discover = func(context.Context, AgentProfile, string) (map[string]struct{}, error) {
		close(started)
		<-release
		return sessionIDSet("existing", "provider-old"), nil
	}
	m.captureDelay = time.Millisecond
	m.captureAttempts = 1
	go func() {
		m.captureProviderSessionID(context.Background(), s.ID, previous.RunID, s.TerminalID, AgentProfile{ID: AgentCodex}, s.ProjectPath, sessionIDSet("existing"))
		close(returned)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("discovery did not start")
	}
	if err = m.Stop(s.ID); err != nil {
		t.Fatal(err)
	}
	s, _ = store.Get(s.ID)
	s.ProviderSessionID = "provider-new"
	if err = store.Upsert(s); err != nil {
		t.Fatal(err)
	}
	m.discover = nil
	next, err := m.Resume(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	<-returned
	got, _ := store.Get(s.ID)
	if got.ProviderSessionID != "provider-new" {
		t.Fatal(got)
	}
	if got.TerminalID != next.TerminalID || len(o.all("begin")) != 2 || len(o.all("bind")) != 0 {
		t.Fatal("old discovery changed new runtime")
	}
}

// Clearing provider metadata during a new generation must not let an old
// discovery pass the original "provider is empty" check and bind that run.
func TestActivityRuntimeOldDiscoveryChecksGenerationAndTerminal(t *testing.T) {
	m, store, _, o := activityRuntimeManager(t)
	s, err := m.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	old := o.all("begin")[0].run
	if err = m.Stop(s.ID); err != nil {
		t.Fatal(err)
	}
	s, _ = store.Get(s.ID)
	s.ProviderSessionID = "provider-new"
	if err = store.Upsert(s); err != nil {
		t.Fatal(err)
	}
	next, err := m.Resume(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	next.ProviderSessionID = ""
	if err = store.Upsert(next); err != nil {
		t.Fatal(err)
	}
	m.captureDelay = time.Nanosecond
	m.captureAttempts = 1
	m.discover = func(context.Context, AgentProfile, string) (map[string]struct{}, error) {
		return sessionIDSet("provider-old"), nil
	}
	m.captureProviderSessionID(context.Background(), s.ID, old.RunID, old.Session.TerminalID, AgentProfile{ID: AgentCodex}, s.ProjectPath, nil)
	got, _ := store.Get(s.ID)
	if got.ProviderSessionID != "" || got.TerminalID != next.TerminalID || len(o.all("bind")) != 0 {
		t.Fatal("stale run bound new generation", got)
	}
}

func TestActivityRuntimeDiscoveryBindsCurrentRun(t *testing.T) {
	m, store, _, o := activityRuntimeManager(t)
	s, err := m.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	run := o.all("begin")[0].run
	m.captureDelay = time.Nanosecond
	m.captureAttempts = 1
	m.discover = func(context.Context, AgentProfile, string) (map[string]struct{}, error) {
		return sessionIDSet("provider-current"), nil
	}
	m.captureProviderSessionID(context.Background(), s.ID, run.RunID, s.TerminalID, AgentProfile{ID: AgentCodex}, s.ProjectPath, nil)
	got, _ := store.Get(s.ID)
	binds := o.all("bind")
	if got.ProviderSessionID != "provider-current" || len(binds) != 1 || binds[0].run.RunID != run.RunID || binds[0].provider != "provider-current" {
		t.Fatal("current discovery not bound", got, binds)
	}
}

func TestActivityRuntimeRepeatedAdoptStartKeepsRun(t *testing.T) {
	m, _, _ := newAdoptionTestManager(t)
	o := &runtimeRecorder{}
	m.SetActivityObserver(o, runtimeTestOwner)
	signal := startedSignal("term-9", "provider-1", `C:\laraenv\www\demo`)
	m.ObserveSignal(signal)
	m.ObserveSignal(signal)
	if len(o.all("reserve")) != 1 || len(o.all("begin")) != 1 {
		t.Fatal("repeated start created generation")
	}
	m.ObserveSignal(startedSignal("term-9", "provider-2", signal.Cwd))
	if len(o.all("begin")) != 2 || len(o.all("end")) != 1 {
		t.Fatal("replacement did not end/start generation")
	}
	m.ObserveSignal(adoptedSignalWithProvider("term-9", SignalEnded, "provider-1"))
	if len(o.all("end")) != 1 {
		t.Fatal("stale end stopped replacement")
	}
}

func TestActivityRuntimeManagedResumeDoesNotRetainAdoptedRouting(t *testing.T) {
	m, store, _, observer := activityRuntimeManager(t)
	m.SetPathResolver(func(string) (ProjectLocation, bool) {
		return ProjectLocation{ProjectName: "demo", ProjectPath: `C:\projects\demo`}, true
	})
	m.ObserveSignal(SessionSignal{TerminalID: "shell", Provider: AgentCodex, ProviderSessionID: "provider", Cwd: `C:\projects\demo`, Kind: SignalStarted, CreatedAt: time.Now()})
	session, ok := m.AdoptedSessionForTerminal("shell")
	if !ok {
		t.Fatal("adoption missing")
	}
	m.ObserveSignal(SessionSignal{TerminalID: "shell", Provider: AgentCodex, ProviderSessionID: "provider", Kind: SignalEnded, CreatedAt: time.Now()})
	session, err := m.Resume(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !session.Adopted {
		t.Fatal("test must retain historical Adopted metadata")
	}
	run := observer.all("begin")[1].run
	if _, ok := m.AdoptedSessionForTerminal(session.TerminalID); ok {
		t.Error("managed resume exposed as current adopted runtime")
	}
	m.ObserveSignal(SessionSignal{SessionID: session.ID, RunID: run.RunID, OwnerID: run.OwnerID, TerminalID: session.TerminalID, Provider: AgentCodex, ProviderSessionID: "provider", Kind: SignalEnded, CreatedAt: time.Now()})
	m.TerminalClosed(session.TerminalID)
	got, _ := store.Get(session.ID)
	if got.State != session.State || !m.HasLiveSessions() {
		t.Fatal("adopted lifecycle ended managed resume")
	}
	m.ObserveSignal(SessionSignal{SessionID: session.ID, RunID: run.RunID, OwnerID: run.OwnerID, TerminalID: session.TerminalID, Provider: AgentCodex, ProviderSessionID: "provider", Kind: SignalDone, CreatedAt: time.Now()})
	got, _ = store.Get(session.ID)
	if got.State != SessionDone {
		t.Fatal("managed completion no longer routed")
	}
}

func TestActivityRuntimeRepeatedAnonymousAdoptionKeepsRun(t *testing.T) {
	m, store, _ := newAdoptionTestManager(t)
	o := &runtimeRecorder{}
	m.SetActivityObserver(o, runtimeTestOwner)
	signal := startedSignal("term-9", "", `C:\laraenv\www\demo`)
	m.ObserveSignal(signal)
	m.ObserveSignal(signal)
	if len(o.all("begin")) != 1 || len(store.List("", true)) != 1 {
		t.Fatal("repeated anonymous start created multiple principals/generations")
	}
}

func TestActivityRuntimeSpawnFailureReleasesBinding(t *testing.T) {
	for _, persistFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(persistFailure), func(t *testing.T) {
			m, store, r, o := activityRuntimeManager(t)
			if persistFailure {
				block := filepath.Join(t.TempDir(), "block")
				if err := os.WriteFile(block, []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
				store.path = filepath.Join(block, "sessions.json")
			} else {
				r.fail = true
			}
			if _, err := m.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex}); err == nil {
				t.Fatal("expected failure")
			}
			if len(o.all("reserve")) != 1 || len(o.all("begin")) != 0 || len(o.all("end")) != 1 || m.HasLiveSessions() {
				t.Fatal("failed launch leaked reservation")
			}
		})
	}
}

func TestActivityRuntimeResumeFailureReleasesBinding(t *testing.T) {
	m, store, r, o := activityRuntimeManager(t)
	if err := store.Upsert(AgentSession{ID: "resume-1", AgentID: AgentCodex, ProjectName: "demo", ProjectPath: `C:\projects\demo`, ProviderSessionID: "provider-1", State: SessionStopped}); err != nil {
		t.Fatal(err)
	}
	r.fail = true
	if _, err := m.Resume(context.Background(), "resume-1"); err == nil {
		t.Fatal("expected spawn failure")
	}
	if len(o.all("reserve")) != 1 || len(o.all("begin")) != 0 || len(o.all("end")) != 1 || m.HasLiveSessions() {
		t.Fatal("failed resume leaked binding")
	}
}

func TestActivityRuntimeLifecycleFailureReleasesReservation(t *testing.T) {
	m, _, _, o := activityRuntimeManager(t)
	m.SetEventExecutable("invalid\"path")
	if _, err := m.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex}); err == nil {
		t.Fatal("expected lifecycle configuration error")
	}
	if len(o.all("reserve")) != 1 || len(o.all("begin")) != 0 || len(o.all("end")) != 1 {
		t.Fatal("configuration failure leaked reservation")
	}
}

func TestActivityRuntimeManagedReadExitEndsObservation(t *testing.T) {
	m, store, r, o := activityRuntimeManager(t)
	s, err := m.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Close(s.TerminalID); err != nil {
		t.Fatal(err)
	}
	waitForAgentState(t, store, s.ID, SessionStopped)
	deadline := time.After(time.Second)
	for {
		select {
		case e := <-o.changed:
			if e.kind == "end" {
				return
			}
		case <-deadline:
			t.Fatal("read exit did not end activity")
		}
	}
}

func TestActivityRuntimeCallbackOutsideLock(t *testing.T) {
	m, _, _, o := activityRuntimeManager(t)
	o.callback = func() { m.HasLiveSessions() }
	done := make(chan struct{})
	go func() {
		defer close(done)
		s, err := m.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
		if err == nil {
			_ = m.Stop(s.ID)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("observer called under manager lock")
	}
}

func TestActivityRuntimeExitResetAndAdoption(t *testing.T) {
	m, _, _ := newAdoptionTestManager(t)
	o := &runtimeRecorder{}
	m.SetActivityObserver(o, runtimeTestOwner)
	m.ObserveSignal(startedSignal("term-9", "provider-1", `C:\laraenv\www\demo`))
	m.ObserveSignal(adoptedSignal("term-9", SignalDone))
	if len(o.all("end")) != 0 || len(o.all("root")) == 0 {
		t.Fatal("turn completion ended identity or missed root")
	}
	m.TerminalClosed("term-9")
	m.TerminalClosed("term-9")
	if len(o.all("end")) != 1 {
		t.Fatal("terminal exit not idempotent")
	}
	if _, err := m.ResetMetadata(); err != nil {
		t.Fatal(err)
	}
	if len(o.all("reset")) != 1 {
		t.Fatal("successful reset not observed")
	}
}

func TestActivityRuntimeAdoptedExitPersistenceFailureEndsObservation(t *testing.T) {
	for _, exitKind := range []string{"terminal-close", "session-end"} {
		t.Run(exitKind, func(t *testing.T) {
			m, store, changes := newAdoptionTestManager(t)
			o := &runtimeRecorder{}
			m.SetActivityObserver(o, runtimeTestOwner)
			m.ObserveSignal(startedSignal("term-9", "provider-1", `C:\laraenv\www\demo`))
			session, ok := m.AdoptedSessionForTerminal("term-9")
			if !ok {
				t.Fatal("adoption missing")
			}
			run := o.all("begin")[0].run
			<-changes
			block := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(block, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			store.path = filepath.Join(block, "sessions.json")
			o.callback = func() { m.HasLiveSessions() }
			closed := make(chan struct{})
			go func() {
				defer close(closed)
				if exitKind == "terminal-close" {
					m.TerminalClosed("term-9")
				} else {
					m.ObserveSignal(adoptedSignalWithProvider("term-9", SignalEnded, "provider-1"))
				}
			}()
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("adopted exit callback held manager mutex")
			}
			if m.HasLiveSessions() || m.runtimeTerminal(session.ID) != "" {
				t.Fatal("closed adopted terminal retained runtime after failed metadata write")
			}
			ends := o.all("end")
			if len(ends) != 1 || ends[0].run.RunID != run.RunID {
				t.Fatal("exit did not end exact run once", ends)
			}
			if _, ok := m.AdoptedSessionForTerminal("term-9"); ok {
				t.Fatal("closed terminal remained authorized for adoption")
			}
			if len(changes) != 0 {
				t.Fatal("failed metadata write emitted session-changed")
			}
			persisted, _ := store.Get(session.ID)
			if persisted.TerminalID != "term-9" {
				t.Fatal("failure fixture unexpectedly persisted", persisted)
			}
			m.TerminalClosed("term-9")
			m.ObserveSignal(adoptedSignalWithProvider("term-9", SignalEnded, "provider-1"))
			if len(o.all("end")) != 1 {
				t.Fatal("duplicate exit ended run twice")
			}
		})
	}
}

func TestActivityRuntimeStaleAdoptedEndCannotClearReplacementOnPersistenceFailure(t *testing.T) {
	m, store, _ := newAdoptionTestManager(t)
	o := &runtimeRecorder{}
	m.SetActivityObserver(o, runtimeTestOwner)
	m.ObserveSignal(startedSignal("term-9", "provider-old", `C:\laraenv\www\demo`))
	m.ObserveSignal(startedSignal("term-9", "provider-new", `C:\laraenv\www\demo`))
	next, ok := m.AdoptedSessionForTerminal("term-9")
	if !ok {
		t.Fatal("replacement missing")
	}
	run := o.all("begin")[1].run
	block := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(block, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(block, "sessions.json")
	m.ObserveSignal(adoptedSignalWithProvider("term-9", SignalEnded, "provider-old"))
	got, ok := m.AdoptedSessionForTerminal("term-9")
	if !ok || got.ID != next.ID || got.ProviderSessionID != "provider-new" || m.runtimeTerminal(next.ID) != "term-9" {
		t.Fatal("stale end cleared replacement", got)
	}
	if len(o.all("end")) != 1 || o.all("begin")[1].run.RunID != run.RunID {
		t.Fatal("stale end changed replacement generation")
	}
}

func TestActivityRuntimeResetFailureDoesNotResetObserver(t *testing.T) {
	m, store, _, o := activityRuntimeManager(t)
	block := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(block, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(block, "sessions.json")
	if _, err := m.ResetMetadata(); err == nil {
		t.Fatal("expected failure")
	}
	if len(o.all("reset")) != 0 {
		t.Fatal("failed reset cleared activity")
	}
}

func TestActivityRuntimePreservesForeignLiveAndRepairsOrphans(t *testing.T) {
	m, store, _, _ := activityRuntimeManager(t)
	for _, id := range []string{"foreign", "orphan"} {
		if err := store.Upsert(AgentSession{ID: id, AgentID: AgentCodex, TerminalID: "term-" + id, State: SessionWorking}); err != nil {
			t.Fatal(err)
		}
	}
	NewManager(m.catalog, store, m.terminals, m.workspace, nil, nil, func(s AgentSession) bool { return s.ID == "foreign" })
	foreign, _ := store.Get("foreign")
	orphan, _ := store.Get("orphan")
	if foreign.TerminalID != "term-foreign" || foreign.State != SessionWorking {
		t.Fatal("foreign owner metadata overwritten", foreign)
	}
	if orphan.TerminalID != "" || orphan.State != SessionStopped {
		t.Fatal("orphan not repaired", orphan)
	}
}

func TestActivityRuntimeActivityHookOptOutKeepsLifecycleIdentity(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			m, _, _, o := activityRuntimeManager(t)
			m.SetEventExecutable(`C:\LaraEnv\laraenv.exe`)
			m.SetActivityHooksEnabled(enabled)
			s := AgentSession{ID: "session-1", AgentID: AgentClaude}
			run := m.reserveActivityRun(s)
			inv, err := m.configureLifecycleInvocation(Invocation{Executable: "claude"}, AgentClaude, run)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Join(inv.Args, " ")
			if !strings.Contains(args, run.RunID) || !strings.Contains(args, runtimeTestOwner) {
				t.Fatal("root lifecycle identity absent", args)
			}
			if strings.Contains(args, "SubagentStart") != enabled {
				t.Fatal("activity hooks ignored opt-out", args)
			}
			env := m.activityTerminalEnvironment(s.ID, run.RunID)
			want := "0"
			if enabled {
				want = "1"
			}
			if env["LARAENV_ACTIVITY_HOOKS"] != want {
				t.Fatal("managed activity opt-out not inherited", env)
			}
			if len(o.all("begin")) != 0 {
				t.Fatal("configuration began live observation")
			}
		})
	}
}

func TestActivityRuntimeObserverExcludesTerminalPreview(t *testing.T) {
	m, store, _, o := activityRuntimeManager(t)
	s, err := m.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	m.recordOutput(s.ID, s.TerminalID, "private terminal text")
	got, _ := store.Get(s.ID)
	if got.LastPreview != "private terminal text" {
		t.Fatal("existing preview behavior changed")
	}
	for _, root := range o.all("root") {
		if root.session.LastPreview != "" {
			t.Fatal("terminal preview reached activity observer")
		}
	}
}

func TestActivityRuntimeReservationBlocksReset(t *testing.T) {
	m, _, r, o := activityRuntimeManager(t)
	r.onSpawn = func(map[string]string) {
		if _, err := m.ResetMetadata(); err == nil {
			t.Error("reset allowed while reserved spawn in progress")
		}
	}
	if _, err := m.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex}); err != nil {
		t.Fatal(err)
	}
	if len(o.all("reset")) != 0 {
		t.Fatal("reservation discarded by reset")
	}
}

func TestActivityRuntimeManagedSignalsRequireCurrentGeneration(t *testing.T) {
	m, store, _, o := activityRuntimeManager(t)
	s, err := m.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	old := o.all("begin")[0].run
	if err = m.Stop(s.ID); err != nil {
		t.Fatal(err)
	}
	s, _ = store.Get(s.ID)
	s.ProviderSessionID = "provider-new"
	if err = store.Upsert(s); err != nil {
		t.Fatal(err)
	}
	next, err := m.Resume(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	current := o.all("begin")[1].run
	for _, signal := range []SessionSignal{
		{OwnerID: runtimeTestOwner, RunID: old.RunID, TerminalID: old.Session.TerminalID},
		{OwnerID: runtimeTestOwner, RunID: current.RunID, TerminalID: old.Session.TerminalID},
		{OwnerID: "foreign-owner", RunID: current.RunID, TerminalID: next.TerminalID},
		{},
	} {
		signal.SessionID = s.ID
		signal.Kind = SignalDone
		signal.CreatedAt = time.Now().UTC()
		m.ObserveSignal(signal)
		got, _ := store.Get(s.ID)
		if got.State != SessionStarting {
			t.Fatal("unfenced/stale signal altered new generation", signal, got)
		}
	}
	m.ObserveSignal(SessionSignal{SessionID: s.ID, OwnerID: runtimeTestOwner, RunID: current.RunID, TerminalID: next.TerminalID, Kind: SignalDone, CreatedAt: time.Now().UTC()})
	got, _ := store.Get(s.ID)
	if got.State != SessionDone {
		t.Fatal("current fenced signal rejected", got)
	}
}

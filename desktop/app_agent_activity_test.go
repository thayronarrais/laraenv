package main

import (
	"context"
	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/config"
	pkgpty "lara4ever/internal/pty"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type appActivityReader struct {
	calls   atomic.Int32
	entered chan struct{}
	release <-chan struct{}
}

func (r *appActivityReader) Read(ctx context.Context, _ pkgagents.ActivityMetadataQuery) (pkgagents.ActivityMetadataResult, error) {
	r.calls.Add(1)
	if r.entered != nil {
		r.entered <- struct{}{}
	}
	if r.release != nil {
		select {
		case <-r.release:
		case <-ctx.Done():
			return pkgagents.ActivityMetadataResult{}, ctx.Err()
		}
	}
	return pkgagents.ActivityMetadataResult{Status: pkgagents.ActivityPartial, MessageCode: pkgagents.ActivityMessageHooksUnobserved}, nil
}

// With a read already in flight, Refresh calls Done only after obtaining that
// read's completion channel. This barrier proves the second request joined
// before the test releases the reader, without relying on scheduler timing.
type appActivityJoinedContext struct {
	context.Context
	joined chan struct{}
	once   sync.Once
}

func (c *appActivityJoinedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.joined) })
	return c.Context.Done()
}
func appActivityFixture(t *testing.T, root string, reader *appActivityReader) (*App, *pkgagents.SessionStore) {
	t.Helper()
	cfg, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetAgentSessionsEnabled(true)
	cfg.SetAgentHooksDisabled(true)
	terminals := pkgpty.NewRegistry()
	store, err := pkgagents.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{cfg: cfg, terminals: terminals}
	a.initAgentActivity(root)
	a.agentActivity.reader = reader
	a.agentActivity.executable = func(pkgagents.AgentSession) string { return "synthetic" }
	a.agentActivity.emit = func(string, ...any) {}
	a.agents = pkgagents.NewManager(pkgagents.NewCatalog(), store, terminals, nil, nil, nil, a.foreignActivityRoot)
	t.Cleanup(func() { a.stopAgentActivity(); a.agentActivity.registry.Close() })
	return a, store
}
func appActivityRun(t *testing.T, a *App, store *pkgagents.SessionStore) pkgagents.ActivityRun {
	t.Helper()
	a.activityTerminalChanged("term-1", true)
	run := pkgagents.ActivityRun{OwnerID: a.agentActivity.identity.ID, RunID: "run", StartedAt: time.Now().UTC(), Session: pkgagents.AgentSession{ID: "session", AgentID: pkgagents.AgentCodex, TerminalID: "term-1", ProviderSessionID: "root", State: pkgagents.SessionWorking}}
	if err := store.Upsert(run.Session); err != nil {
		t.Fatal(err)
	}
	a.agentActivity.coordinator.BeginRun(run)
	return run
}
func TestAgentActivityMainDetachedRouting(t *testing.T) {
	root := t.TempDir()
	release := make(chan struct{})
	reader := &appActivityReader{entered: make(chan struct{}, 2), release: release}
	detachedReader := &appActivityReader{}
	main, store := appActivityFixture(t, root, reader)
	detached, otherStore := appActivityFixture(t, root, detachedReader)
	detached.detached = true
	main.startAgentActivity(context.Background())
	detached.startAgentActivity(context.Background())
	run := appActivityRun(t, main, store)
	otherStore.Upsert(run.Session)
	type result struct {
		snapshot pkgagents.ActivitySnapshot
		err      error
	}
	remoteDone := make(chan result, 1)
	go func() { snapshot, err := detached.RefreshAgentActivity("session"); remoteDone <- result{snapshot, err} }()
	select {
	case <-reader.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("detached refresh did not enter owner reader")
	}
	if err := main.SetAgentActivityInterest(pkgagents.ActivityInterest{WindowID: "main", Purpose: pkgagents.ActivityInterestDialog, SessionID: "session"}); err != nil {
		t.Fatal(err)
	}
	if err := detached.SetAgentActivityInterest(pkgagents.ActivityInterest{WindowID: "detached", Purpose: pkgagents.ActivityInterestCounter, SessionID: "session"}); err != nil {
		t.Fatal(err)
	}
	joined := &appActivityJoinedContext{Context: context.Background(), joined: make(chan struct{})}
	localDone := make(chan result, 1)
	go func() {
		snapshot, err := main.agentActivity.router.Refresh(joined, "session")
		localDone <- result{snapshot, err}
	}()
	select {
	case <-joined.joined:
	case <-time.After(5 * time.Second):
		t.Fatal("local refresh did not join owner read")
	}
	// Remove both scheduling leases before releasing the shared read. Completed
	// periodic work followed by a manual refresh is allowed to start a new read;
	// this test specifically measures overlapping requests to the same owner.
	if err := main.RemoveAgentActivityInterest("main", "dialog"); err != nil {
		t.Fatal(err)
	}
	if err := detached.RemoveAgentActivityInterest("detached", "counter"); err != nil {
		t.Fatal(err)
	}
	close(release)
	for _, done := range []<-chan result{remoteDone, localDone} {
		select {
		case response := <-done:
			if response.err != nil || response.snapshot.SourceID != run.OwnerID {
				t.Fatalf("routed refresh: %+v %v", response.snapshot, response.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("shared refresh did not complete")
		}
	}
	if reader.calls.Load() != 1 || detachedReader.calls.Load() != 0 {
		t.Fatalf("overlapping requests did not share owner reader: owner=%d detached=%d", reader.calls.Load(), detachedReader.calls.Load())
	}
	if _, err := detached.GetAgentActivity("session"); err != nil {
		t.Fatal(err)
	}
}
func TestAgentActivityDisableResetShutdown(t *testing.T) {
	reader := &appActivityReader{}
	a, store := appActivityFixture(t, t.TempDir(), reader)
	a.startAgentActivity(context.Background())
	run := appActivityRun(t, a, store)
	a.agentActivity.coordinator.EndRun("session", run.RunID, time.Now())
	before, _ := a.GetAgentActivity("session")
	owner := a.agentActivity.identity.ID
	if err := a.SetAgentSessionsEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := a.agentActivity.registry.ResolveOwner(owner); ok {
		t.Fatal("disabled activity server remains registered")
	}
	if _, err := a.GetAgentActivity("session"); err == nil {
		t.Fatal("disabled Wails API accepted")
	}
	if err := a.SetAgentSessionsEnabled(true); err != nil {
		t.Fatal(err)
	}
	if a.agentActivity.identity.ID != owner {
		t.Fatal("terminal owner changed across toggle")
	}
	if live, _ := a.agentActivity.registry.ResolveTerminal(owner, "term-1"); !live {
		t.Fatal("ordinary terminal membership not restored")
	}
	run.RunID = "next"
	a.agentActivity.coordinator.BeginRun(run)
	after, _ := a.GetAgentActivity("session")
	if after.Revision <= before.Revision {
		t.Fatal("same owner revision reset")
	}
	a.agentActivity.coordinator.EndRun("session", "next", time.Now())
	oldServer := a.agentActivity.server
	if _, err := a.ResetAgentMetadata(); err != nil {
		t.Fatal(err)
	}
	if a.agentActivity.server == nil || a.agentActivity.server == oldServer || a.agentActivity.server.Descriptor().Token == oldServer.Descriptor().Token {
		t.Fatal("reset did not replace the service with an empty collector")
	}
	if _, err := a.GetAgentActivity("session"); err == nil {
		t.Fatal("reset session still accessible")
	}
	a.startAgentActivity(context.Background())
	a.commandBar = true
	a.shutdown(context.Background())
	if a.agentActivity.server != nil {
		t.Fatal("early shutdown return retained service")
	}
}
func TestAgentActivityForeignUnknownRootBlocksResetAndDisable(t *testing.T) {
	root := t.TempDir()
	a, _ := appActivityFixture(t, root, &appActivityReader{})
	b, store := appActivityFixture(t, root, &appActivityReader{})
	a.startAgentActivity(context.Background())
	b.startAgentActivity(context.Background())
	appActivityRun(t, b, store)
	if len(a.agents.ListSessions("", true)) != 0 {
		t.Fatal("test must use an unknown foreign root")
	}
	if _, err := a.ResetAgentMetadata(); err == nil {
		t.Fatal("reset ignored foreign live root")
	}
	if err := a.SetAgentSessionsEnabled(false); err == nil {
		t.Fatal("disable ignored foreign live root")
	}
	if !a.GetAgentSessionsEnabled() {
		t.Fatal("failed disable changed config")
	}
}
func TestAgentActivityCommandBarHasNoCollector(t *testing.T) {
	a, _ := appActivityFixture(t, t.TempDir(), &appActivityReader{})
	a.commandBar = true
	a.startAgentActivity(context.Background())
	if a.agentActivity.server != nil {
		t.Fatal("command bar started collector")
	}
}

func TestAgentActivityResetAllowsNewOrdinaryAdoption(t *testing.T) {
	a, _ := appActivityFixture(t, t.TempDir(), &appActivityReader{})
	a.agents.SetPathResolver(func(string) (pkgagents.ProjectLocation, bool) {
		return pkgagents.ProjectLocation{ProjectName: "synthetic", ProjectPath: "C:/synthetic", Mode: pkgagents.LaunchCurrent}, true
	})
	a.startAgentActivity(context.Background())
	a.activityTerminalChanged("term-9", true)
	old := a.agentActivity.coordinator
	if _, err := a.ResetAgentMetadata(); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Get(context.Background(), "anything"); err == nil {
		t.Fatal("old coordinator survived reset")
	}
	a.agentActivity.coordinator.ObserveSessionSignal(pkgagents.SessionSignal{OwnerID: a.agentActivity.identity.ID, TerminalID: "term-9", Provider: pkgagents.AgentClaude, ProviderSessionID: "new-root", Cwd: "C:/synthetic", Kind: pkgagents.SignalStarted, CreatedAt: time.Now().UTC()})
	sessions := a.agents.ListSessions("", true)
	if len(sessions) != 1 {
		t.Fatal("ordinary terminal could not adopt after metadata reset")
	}
	snapshot, err := a.GetAgentActivity(sessions[0].ID)
	if err != nil || len(snapshot.Nodes) != 1 {
		t.Fatalf("fresh adoption did not start empty: %+v %v", snapshot, err)
	}
}
func TestAgentActivityConstructorGuardPreservesOnlyForeignLiveRoot(t *testing.T) {
	root := t.TempDir()
	owner, store := appActivityFixture(t, root, &appActivityReader{})
	owner.startAgentActivity(context.Background())
	run := appActivityRun(t, owner, store)
	other, _ := appActivityFixture(t, root, &appActivityReader{})
	if !other.foreignActivityRoot(run.Session) {
		t.Fatal("constructor would repair proven foreign runtime")
	}
	owner.agentActivity.coordinator.EndRun(run.Session.ID, run.RunID, time.Now())
	if other.foreignActivityRoot(run.Session) {
		t.Fatal("ended binding authorized runtime preservation")
	}
}

func TestAgentActivityKnownUnobservedRoot(t *testing.T) {
	a, store := appActivityFixture(t, t.TempDir(), &appActivityReader{})
	a.startAgentActivity(context.Background())
	for _, provider := range []pkgagents.AgentID{pkgagents.AgentClaude, pkgagents.AgentKimi, pkgagents.AgentOpenCode} {
		session := pkgagents.AgentSession{ID: string(provider), AgentID: provider, Title: "Known principal", ProjectName: "project", ProjectPath: "C:/synthetic", State: pkgagents.SessionStopped}
		store.Upsert(session)
		snapshot, err := a.GetAgentActivity(session.ID)
		if err != nil || len(snapshot.Nodes) != 1 || snapshot.Nodes[0].Label != "Known principal" {
			t.Fatalf("known stopped principal unavailable: %+v %v", snapshot, err)
		}
		if snapshot.RunID != "" || snapshot.SourceID != "" || snapshot.Nodes[0].StartedAt != nil || snapshot.Capabilities.Duration || snapshot.Capabilities.Tokens {
			t.Fatal("unobserved root invented execution evidence")
		}
		if provider != pkgagents.AgentClaude && snapshot.MessageCode != pkgagents.ActivityMessageUnsupportedProvider {
			t.Fatal("unsupported principal lacks limitation")
		}
		if err := a.SetAgentActivityInterest(pkgagents.ActivityInterest{WindowID: "w", Purpose: pkgagents.ActivityInterestDialog, SessionID: session.ID}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.RefreshAgentActivity(session.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.GetAgentActivity("unknown"); err == nil {
		t.Fatal("unknown session accepted")
	}
}

func TestAgentActivityInvalidInterestOnUnobservedRoot(t *testing.T) {
	a, store := appActivityFixture(t, t.TempDir(), &appActivityReader{})
	a.startAgentActivity(context.Background())
	store.Upsert(pkgagents.AgentSession{ID: "stopped", AgentID: pkgagents.AgentClaude, State: pkgagents.SessionStopped})
	if err := a.SetAgentActivityInterest(pkgagents.ActivityInterest{WindowID: "window", Purpose: "invalid", SessionID: "stopped"}); err == nil {
		t.Fatal("invalid interest accepted for unobserved root")
	}
}
func TestAgentActivityFailedResetKeepsService(t *testing.T) {
	a, store := appActivityFixture(t, t.TempDir(), &appActivityReader{})
	a.startAgentActivity(context.Background())
	store.Upsert(pkgagents.AgentSession{ID: "worktree", AgentID: pkgagents.AgentClaude, State: pkgagents.SessionStopped, WorktreePath: t.TempDir()})
	server := a.agentActivity.server
	if _, err := a.ResetAgentMetadata(); err == nil {
		t.Fatal("fixture reset should fail for existing worktree")
	}
	if a.agentActivity.server != server {
		t.Fatal("failed reset changed live service")
	}
}

func TestAgentActivityCollectorFailureStillShowsKnownPrincipal(t *testing.T) {
	a, store := appActivityFixture(t, t.TempDir(), &appActivityReader{})
	store.Upsert(pkgagents.AgentSession{ID: "known", AgentID: pkgagents.AgentClaude, Title: "Known", State: pkgagents.SessionStopped})
	snapshot, err := a.GetAgentActivity("known")
	if err != nil || len(snapshot.Nodes) != 1 {
		t.Fatal("collector failure hid known principal")
	}
}

// The real manager emits ReserveRun immediately before BeginRun for adoption.
// A pending child must survive that ordering without moving managed run fences.
func TestAgentActivityReservedAdoptionReplaysEarlyChild(t *testing.T) {
	a, _ := appActivityFixture(t, t.TempDir(), &appActivityReader{})
	snapshots := make(chan pkgagents.ActivitySnapshot, 8)
	a.agentActivity.emit = func(_ string, values ...any) {
		if len(values) != 0 {
			if snapshot, ok := values[0].(pkgagents.ActivitySnapshot); ok {
				select {
				case snapshots <- snapshot:
				default:
				}
			}
		}
	}
	if err := a.cfg.SetAgentHooksDisabled(false); err != nil {
		t.Fatal(err)
	}
	a.agents.SetPathResolver(func(string) (pkgagents.ProjectLocation, bool) {
		return pkgagents.ProjectLocation{ProjectName: "synthetic", ProjectPath: "C:/synthetic", Mode: pkgagents.LaunchCurrent}, true
	})
	a.startAgentActivity(context.Background())
	a.activityTerminalChanged("term-9", true)
	receipt := time.Now().UTC().Add(-time.Second)
	activityDir, _ := a.agentActivity.registry.QueueDir(a.agentActivity.identity.ID, "activity")
	sessionDir, _ := a.agentActivity.registry.QueueDir(a.agentActivity.identity.ID, "session")
	if err := pkgagents.WriteActivitySignal(activityDir, pkgagents.ActivityEvent{OwnerID: a.agentActivity.identity.ID, TerminalID: "term-9", Provider: pkgagents.AgentClaude, ProviderSessionID: "adopt-root", NodeID: "early-child", Kind: pkgagents.ActivitySubagentStart, ReceivedAt: receipt}); err != nil {
		t.Fatal(err)
	}
	if err := pkgagents.WriteSignal(sessionDir, pkgagents.SessionSignal{OwnerID: a.agentActivity.identity.ID, TerminalID: "term-9", Provider: pkgagents.AgentClaude, ProviderSessionID: "adopt-root", Kind: pkgagents.SignalWorking}); err != nil {
		t.Fatal(err)
	}
	if err := pkgagents.WriteSignal(sessionDir, pkgagents.SessionSignal{OwnerID: a.agentActivity.identity.ID, TerminalID: "term-9", Provider: pkgagents.AgentClaude, ProviderSessionID: "adopt-root", Cwd: "C:/synthetic", Kind: pkgagents.SignalStarted}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-snapshots:
	case <-time.After(5 * time.Second):
		t.Fatal("queued adoption was not observed")
	}
	sessions := a.agents.ListSessions("", true)
	if len(sessions) != 1 {
		t.Fatal("real manager failed to adopt principal")
	}
	if sessions[0].State != pkgagents.SessionWorking {
		t.Fatal("pending lifecycle signal was not replayed")
	}
	snapshot, err := a.GetAgentActivity(sessions[0].ID)
	if err != nil || len(snapshot.Nodes) != 2 {
		t.Fatalf("real ReserveRun/BeginRun ordering discarded early child: %+v %v", snapshot, err)
	}
	if !snapshot.ObservedSince.Equal(receipt) || !sessions[0].StartedAt.After(receipt) {
		t.Fatal("adoption did not preserve receipt separately from actual session start")
	}
	if !snapshot.Nodes[1].FirstObservedAt.Equal(receipt) {
		t.Fatal("replay rewrote event timestamp")
	}
	binding, ok, err := a.agentActivity.registry.ResolveProvider(pkgagents.AgentClaude, "early-child")
	if err != nil || !ok || binding.SessionID != sessions[0].ID {
		t.Fatal("replayed child lacks verified alias")
	}
}

func TestAgentActivityResetCannotBeRepopulatedByBlockedAdoption(t *testing.T) {
	a, _ := appActivityFixture(t, t.TempDir(), &appActivityReader{})
	entered := make(chan struct{})
	release := make(chan struct{})
	a.agents.SetPathResolver(func(string) (pkgagents.ProjectLocation, bool) {
		close(entered)
		<-release
		return pkgagents.ProjectLocation{ProjectName: "synthetic", ProjectPath: "C:/synthetic", Mode: pkgagents.LaunchCurrent}, true
	})
	a.startAgentActivity(context.Background())
	a.activityTerminalChanged("term-9", true)
	observed := make(chan struct{})
	go func() {
		defer close(observed)
		a.agentActivity.coordinator.ObserveSessionSignal(pkgagents.SessionSignal{OwnerID: a.agentActivity.identity.ID, TerminalID: "term-9", Provider: pkgagents.AgentClaude, ProviderSessionID: "root-before-reset", Cwd: "C:/synthetic", Kind: pkgagents.SignalStarted, CreatedAt: time.Now().UTC()})
	}()
	<-entered
	resetDone := make(chan error, 1)
	go func() { _, err := a.ResetAgentMetadata(); resetDone <- err }()
	var resetErr error
	resetCompleted := false
	select {
	case resetErr = <-resetDone:
		resetCompleted = true
	case <-time.After(250 * time.Millisecond):
	}
	close(release)
	<-observed
	if !resetCompleted {
		resetErr = <-resetDone
	}
	if resetErr == nil && len(a.agents.ListSessions("", true)) != 0 {
		t.Fatal("old blocked adoption repopulated successful metadata reset")
	}
}

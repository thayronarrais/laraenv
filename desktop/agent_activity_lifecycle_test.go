package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pkgagents "lara4ever/internal/agents"
	pkgpty "lara4ever/internal/pty"
)

type activityShellFixture struct{}

func (*activityShellFixture) Read([]byte) (int, error)    { return 0, io.EOF }
func (*activityShellFixture) Write(p []byte) (int, error) { return len(p), nil }
func (*activityShellFixture) Resize(int, int) error       { return nil }
func (*activityShellFixture) Close() error                { return nil }

func TestAgentActivityBoundAdoptionLifecycleThroughHelper(t *testing.T) {
	registry, old := activityHelperRegistry(t)
	defer registry.Close()
	if err := registry.Release(old.SessionID, old.RunID); err != nil {
		t.Fatal(err)
	}
	terminals := pkgpty.NewRegistry()
	terminal := terminals.Register(&activityShellFixture{})
	defer terminals.CloseAll()
	if terminal != old.TerminalID {
		t.Fatal("fixture terminal mismatch")
	}
	store, err := pkgagents.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := pkgagents.NewManager(pkgagents.NewCatalog(), store, terminals, nil, nil, nil)
	manager.SetPathResolver(func(string) (pkgagents.ProjectLocation, bool) {
		return pkgagents.ProjectLocation{ProjectName: "fixture", ProjectPath: "C:/fixture"}, true
	})
	reader := &appActivityReader{}
	coordinator := pkgagents.NewActivityCoordinator(pkgagents.ActivityCoordinatorOptions{OwnerID: old.OwnerID, Registry: registry, Reader: reader, ObserveSession: manager.ObserveSignal, Executable: func(pkgagents.AgentSession) string { return "synthetic" }})
	defer coordinator.Close()
	manager.SetActivityObserver(coordinator, old.OwnerID)
	queue, _ := registry.QueueDir(old.OwnerID, "session")
	helper := &agentHelperContext{registry: registry, getenv: envWith(map[string]string{"LARAENV_AGENT_OWNER": old.OwnerID, "LARAENV_TERMINAL_ID": terminal}), stdout: &bytes.Buffer{}}
	send := func(event string) pkgagents.SessionSignal {
		t.Helper()
		payload := fmt.Sprintf(`{"hook_event_name":%q,"session_id":"root-1","cwd":"C:/fixture"}`, event)
		if _, err := writeAgentHook([]string{"app", "--agent-hook", "codex"}, strings.NewReader(payload), helper.getenv, t.TempDir(), helper); err != nil {
			t.Fatal(err)
		}
		signals := drainHookSignals(t, queue)
		if len(signals) != 1 {
			t.Fatalf("%s queue: %+v", event, signals)
		}
		coordinator.ObserveSessionSignal(signals[0])
		return signals[0]
	}
	send("SessionStart")
	session, ok := manager.AdoptedSessionForTerminal(terminal)
	if !ok {
		t.Fatal("root not adopted")
	}
	working := send("UserPromptSubmit")
	if working.SessionID != session.ID || working.RunID == "" || working.OwnerID != old.OwnerID || working.TerminalID != terminal {
		t.Fatal("helper lost execution fence")
	}
	session, _ = manager.GetSession(session.ID)
	if session.State != pkgagents.SessionWorking {
		t.Errorf("post-binding prompt ignored: %s", session.State)
	}
	if err := coordinator.SetInterest(t.Context(), pkgagents.ActivityInterest{WindowID: "fixture", Purpose: pkgagents.ActivityInterestDialog, SessionID: session.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Refresh(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	calls := reader.calls.Load()
	send("SessionEnd")
	session, _ = manager.GetSession(session.ID)
	if session.State != pkgagents.SessionStopped || manager.HasLiveSessions() {
		t.Errorf("SessionEnd left adopted runtime live: %+v", session)
	}
	if _, ok, err := registry.ResolveActiveBinding(session.ID); err != nil || ok {
		t.Errorf("ended binding retained: %v %v", ok, err)
	}
	if _, ok := terminals.Get(terminal); !ok {
		t.Fatal("SessionEnd closed the host shell")
	}
	if live, err := registry.ResolveTerminal(old.OwnerID, terminal); err != nil || !live {
		t.Fatal("host shell membership removed")
	}
	coordinator.Tick(context.Background(), time.Now().Add(4*time.Second))
	if _, err := coordinator.Refresh(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	if reader.calls.Load() != calls {
		t.Error("ended root still polls")
	}
	// Re-adopt the same provider/session and terminal; old queued fences must
	// not be rebound to the new execution even though those identities match.
	send("SessionStart")
	replacement, _ := manager.GetSession(session.ID)
	next, bound, err := registry.ResolveActiveBinding(session.ID)
	if err != nil || !bound || next.RunID == working.RunID {
		t.Fatal("replacement did not acquire a new run fence")
	}
	stale := working
	stale.Kind = pkgagents.SignalEnded
	manager.ObserveSignal(stale)
	coordinator.ObserveSessionSignal(stale)
	got, _ := manager.GetSession(session.ID)
	if got.State != replacement.State || !manager.HasLiveSessions() {
		t.Fatal("prior-run fence stopped replacement")
	}
	for _, mutate := range []func(*pkgagents.SessionSignal){
		func(s *pkgagents.SessionSignal) { s.OwnerID = "foreign-owner" },
		func(s *pkgagents.SessionSignal) { s.TerminalID = "other-shell" },
	} {
		invalid := working
		invalid.RunID, invalid.Kind = next.RunID, pkgagents.SignalEnded
		mutate(&invalid)
		manager.ObserveSignal(invalid)
		coordinator.ObserveSessionSignal(invalid)
		if !manager.HasLiveSessions() {
			t.Fatal("foreign owner or terminal ended replacement")
		}
	}
}

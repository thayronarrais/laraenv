package documentshutdown

import (
	"errors"
	"testing"
)

func TestDocumentShutdownPendingAndStaleReply(t *testing.T) {
	for _, intents := range [][2]string{{"quit", "update"}, {"update", "quit"}} {
		t.Run(intents[0], func(t *testing.T) {
			var events []RequestEvent
			g := New(func(e RequestEvent) { events = append(events, e) })
			calls := 0
			id, err := g.Request(intents[0], func() error { calls++; return nil })
			if err != nil || id == "" {
				t.Fatal(id, err)
			}
			if len(events) != 0 || calls != 0 {
				t.Fatal("not ready must wait")
			}
			id2, err := g.Request(intents[1], func() error { t.Error("replaced continuation"); return nil })
			if id2 != id || !errors.Is(err, ErrPending) {
				t.Fatal(id2, err)
			}
			if err = g.UpdateState(State{Ready: true, StateVersion: 1}); err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || events[0].Intent != intents[0] {
				t.Fatal(events)
			}
			_ = g.UpdateState(State{Ready: true, StateVersion: 1})
			if len(events) != 1 || calls != 0 {
				t.Fatal("must emit once and wait for reply")
			}
			if g.Resolve(Reply{RequestID: "old", Decision: "allow", StateVersion: 1}) == nil {
				t.Fatal("accepted old ID")
			}
			if err = g.Resolve(Reply{RequestID: id, Decision: "cancel", StateVersion: 1}); err != nil {
				t.Fatal(err)
			}
			if calls != 0 {
				t.Fatal("cancel ran continuation")
			}
			next, _ := g.Request(intents[1], func() error { calls++; return nil })
			if next == id {
				t.Fatal("reused ID")
			}
			if g.Resolve(Reply{RequestID: id, Decision: "allow", StateVersion: 1}) == nil {
				t.Fatal("accepted prior reply")
			}
			if err = g.Resolve(Reply{RequestID: next, Decision: "allow", StateVersion: 1}); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatal(calls)
			}
		})
	}
}

func TestDocumentShutdownRequiresCurrentCleanState(t *testing.T) {
	g := New(func(RequestEvent) {})
	_ = g.UpdateState(State{Ready: true, StateVersion: 1})
	calls := 0
	id, _ := g.Request("quit", func() error { calls++; return nil })
	if calls != 0 {
		t.Fatal("cached clean state bypassed ACK")
	}
	_ = g.UpdateState(State{Ready: true, DirtyCount: 1, StateVersion: 2})
	if g.Resolve(Reply{id, "allow", 1}) == nil {
		t.Fatal("lagged clean reply accepted")
	}
	if g.Resolve(Reply{id, "allow", 2}) == nil {
		t.Fatal("dirty reply accepted")
	}
	if g.UpdateState(State{Ready: true, StateVersion: 1}) == nil {
		t.Fatal("old report accepted")
	}
	if g.UpdateState(State{Ready: true, StateVersion: 2}) == nil {
		t.Fatal("changed state at same version")
	}
	if g.UpdateState(State{Ready: true, DirtyCount: -1, StateVersion: 3}) == nil {
		t.Fatal("negative dirty count")
	}
	_ = g.UpdateState(State{Ready: true, StateVersion: 3})
	_ = g.UpdateState(State{Ready: true, DirtyCount: 1, StateVersion: 4}) // edit during save-all
	if g.Resolve(Reply{id, "allow", 3}) == nil || calls != 0 {
		t.Fatal("save-all raced new edit")
	}
	_ = g.UpdateState(State{Ready: true, StateVersion: 5})
	if err := g.Resolve(Reply{id, "allow", 5}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestDocumentShutdownReentrantAndConcurrentExecution(t *testing.T) {
	var g *Gate
	events := 0
	g = New(func(e RequestEvent) {
		events++
		if err := g.UpdateState(State{Ready: true, StateVersion: 1}); err != nil {
			t.Error(err)
		}
	})
	_ = g.UpdateState(State{Ready: true, StateVersion: 1})
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var id string
	id, _ = g.Request("quit", func() error {
		if next, err := g.Request("update", func() error { t.Error("reentrant execution"); return nil }); next != id || !errors.Is(err, ErrPending) {
			t.Error(next, err)
		}
		close(entered)
		<-release
		return errors.New("failed action")
	})
	go func() { done <- g.Resolve(Reply{id, "allow", 1}) }()
	<-entered
	if g.Resolve(Reply{id, "allow", 1}) == nil {
		t.Fatal("duplicate executing reply accepted")
	}
	if g.Resolve(Reply{id, "cancel", 1}) == nil {
		t.Fatal("canceled executing action")
	}
	if next, err := g.Request("update", func() error { t.Error("concurrent action"); return nil }); next != id || !errors.Is(err, ErrPending) {
		t.Fatal(next, err)
	}
	close(release)
	if err := <-done; err == nil || err.Error() != "failed action" {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatal(events)
	}
	if g.Resolve(Reply{id, "allow", 1}) == nil {
		t.Fatal("failure implicitly retried")
	}
	next, _ := g.Request("quit", func() error { return nil })
	if next == id {
		t.Fatal("new action must require new acknowledgement")
	}
}

func TestFailedContinuationIsExplicitlyReleased(t *testing.T) {
	g := New(func(RequestEvent) {})
	_ = g.UpdateState(State{Ready: true, StateVersion: 1})
	id, _ := g.Request("update", func() error { return errors.New("apply failed") })
	err := g.Resolve(Reply{id, "allow", 1})
	released, ok := err.(interface{ Released() bool })
	if !ok || !released.Released() {
		t.Fatalf("failure must explicitly release request: %v", err)
	}
	next, err := g.Request("quit", func() error { return nil })
	if err != nil || next == id {
		t.Fatal(next, err)
	}
}

func TestStaleCancellationKeepsRequestForCurrentVersion(t *testing.T) {
	g := New(func(RequestEvent) {})
	_ = g.UpdateState(State{Ready: true, StateVersion: 1})
	id, _ := g.Request("quit", func() error { t.Fatal("cancel ran action"); return nil })
	_ = g.UpdateState(State{Ready: true, DirtyCount: 1, StateVersion: 2})
	if err := g.Resolve(Reply{id, "cancel", 1}); err == nil {
		t.Fatal("stale cancel accepted")
	}
	if pending, err := g.Request("update", func() error { return nil }); pending != id || !errors.Is(err, ErrPending) {
		t.Fatal(pending, err)
	}
	if err := g.Resolve(Reply{id, "cancel", 2}); err != nil {
		t.Fatal(err)
	}
	next, err := g.Request("quit", func() error { return nil })
	if err != nil || next == id {
		t.Fatal(next, err)
	}
}

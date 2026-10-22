package notifications

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"lara4ever/internal/agents"
)

type fakeToastSender struct {
	mu    sync.Mutex
	calls []string
	wake  chan struct{}
}

func (f *fakeToastSender) Push(title, message string, sound bool) error {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("%s|%s|%t", title, message, sound))
	f.mu.Unlock()
	select {
	case f.wake <- struct{}{}:
	default:
	}
	return nil
}

func newTestManager(t *testing.T) (*Manager, *Store, *fakeToastSender) {
	t.Helper()
	store, _, err := NewStore(filepath.Join(t.TempDir(), "notifications.json"))
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeToastSender{wake: make(chan struct{}, 10)}
	manager := NewManager(store, sender, nil)
	return manager, store, sender
}

func TestSessionNotificationsDeduplicateAndRespectFocus(t *testing.T) {
	manager, store, sender := newTestManager(t)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	session := agents.AgentSession{
		ID: "s1", ProjectName: "lara4ever", AgentID: agents.AgentCodex,
		Title: "Fix routing", State: agents.SessionWorking, UpdatedAt: now,
	}

	manager.ObserveSession(session, false)
	session.State = agents.SessionError
	session.ExitError = "process exited 1"
	session.UpdatedAt = now.Add(time.Second)
	manager.ObserveSession(session, false)
	select {
	case <-sender.wake:
	case <-time.After(time.Second):
		t.Fatal("expected OS toast")
	}
	if got := len(store.List()); got != 1 {
		t.Fatalf("got %d inbox entries, want 1", got)
	}
	if got := store.UnreadCount(); got != 1 {
		t.Fatalf("got %d unread, want 1", got)
	}

	focused := session
	focused.ID = "s2"
	focused.State = agents.SessionWorking
	focused.UpdatedAt = now.Add(2 * time.Second)
	manager.ObserveSession(focused, true)
	focused.State = agents.SessionError
	focused.UpdatedAt = now.Add(3 * time.Second)
	manager.ObserveSession(focused, true)
	if got := store.UnreadCount(); got != 1 {
		t.Fatalf("focused entry should be read; got %d unread", got)
	}
	time.Sleep(20 * time.Millisecond)
	sender.mu.Lock()
	toastCount := len(sender.calls)
	sender.mu.Unlock()
	if toastCount != 1 {
		t.Fatalf("focused session generated toast; got %d calls", toastCount)
	}
}

func TestMarkReadAndMarkAllRead(t *testing.T) {
	manager, store, _ := newTestManager(t)
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		session := agents.AgentSession{
			ID: fmt.Sprintf("s%d", i), ProjectName: "app", AgentID: agents.AgentCodex,
			Title: "Agent", State: agents.SessionWorking, UpdatedAt: now.Add(time.Duration(i) * time.Second),
		}
		manager.ObserveSession(session, false)
		session.State = agents.SessionError
		session.UpdatedAt = session.UpdatedAt.Add(time.Second)
		manager.ObserveSession(session, false)
	}
	entries := store.List()
	if err := manager.MarkRead(entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := store.UnreadCount(); got != 1 {
		t.Fatalf("got %d unread after MarkRead, want 1", got)
	}
	if err := manager.MarkAllRead(); err != nil {
		t.Fatal(err)
	}
	if got := store.UnreadCount(); got != 0 {
		t.Fatalf("got %d unread after MarkAllRead, want 0", got)
	}
}

func TestDisabledCategoryCreatesNothing(t *testing.T) {
	manager, store, _ := newTestManager(t)
	preferences := DefaultPreferences()
	preferences.Errors = false
	if err := manager.SetPreferences(preferences); err != nil {
		t.Fatal(err)
	}
	session := agents.AgentSession{ID: "s1", State: agents.SessionWorking, UpdatedAt: time.Now()}
	manager.ObserveSession(session, false)
	session.State = agents.SessionError
	manager.ObserveSession(session, false)
	if got := len(store.List()); got != 0 {
		t.Fatalf("disabled error category created %d entries", got)
	}
}

func TestRepeatedProviderErrorsRateLimitOnlyOSToast(t *testing.T) {
	manager, store, sender := newTestManager(t)
	now := time.Now().UTC()
	manager.now = func() time.Time { return now }
	for _, id := range []string{"s1", "s2"} {
		session := agents.AgentSession{ID: id, ProjectName: "app", AgentID: agents.AgentCodex, Title: id, State: agents.SessionWorking, UpdatedAt: now}
		manager.ObserveSession(session, false)
		session.State = agents.SessionError
		session.UpdatedAt = now.Add(time.Second)
		manager.ObserveSession(session, false)
	}
	if got := len(store.List()); got != 2 {
		t.Fatalf("rate limiting must preserve inbox entries; got %d", got)
	}
	select {
	case <-sender.wake:
	case <-time.After(time.Second):
		t.Fatal("expected first toast")
	}
	time.Sleep(20 * time.Millisecond)
	sender.mu.Lock()
	count := len(sender.calls)
	sender.mu.Unlock()
	if count != 1 {
		t.Fatalf("got %d OS toasts for repeated provider errors, want 1", count)
	}
}

func TestUsageThresholdTransitions(t *testing.T) {
	manager, store, sender := newTestManager(t)
	reset := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	observe := func(used float64) {
		manager.ObserveUsage([]agents.UsageSnapshot{{
			Provider: agents.AgentCodex, Status: "available",
			Windows: []agents.UsageWindow{{ID: "five-hour", Label: "5 hour", UsedPct: &used, ResetsAt: &reset}},
		}})
	}

	observe(70) // establishes baseline
	observe(81) // warning crossing
	observe(85) // same band
	observe(96) // critical crossing
	observe(70) // reset locally, but same provider cycle remains deduplicated
	observe(81)

	if got := len(store.List()); got != 2 {
		t.Fatalf("got %d threshold entries, want 2", got)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-sender.wake:
		case <-time.After(time.Second):
			t.Fatal("expected usage toast")
		}
	}
}

func TestNotifyLocalCAUntrustedDedupesPerCAThumbprintAndRespectsPreference(t *testing.T) {
	manager, store, sender := newTestManager(t)

	manager.NotifyLocalCAUntrusted("shop", "THUMB-A")
	manager.NotifyLocalCAUntrusted("shop", "THUMB-A") // same CA again: deduped, not a second entry
	manager.NotifyLocalCAUntrusted("blog", "THUMB-A") // different project, SAME CA: still deduped — one nudge per CA, not per project
	manager.NotifyLocalCAUntrusted("shop", "THUMB-B") // different CA: its own entry

	select {
	case <-sender.wake:
	case <-time.After(time.Second):
		t.Fatal("expected an OS toast for the local CA notice")
	}
	time.Sleep(20 * time.Millisecond)

	entries := store.List()
	if got := len(entries); got != 2 {
		t.Fatalf("got %d entries, want 2 (one per untrusted CA, deduped across projects sharing a CA)", got)
	}
	for _, entry := range entries {
		if entry.Category != CategoryNeedsInput {
			t.Fatalf("entry category = %q, want %q", entry.Category, CategoryNeedsInput)
		}
	}

	preferences := DefaultPreferences()
	preferences.NeedsInput = false
	if err := manager.SetPreferences(preferences); err != nil {
		t.Fatal(err)
	}
	manager.NotifyLocalCAUntrusted("checkout", "THUMB-C")
	if got := len(store.List()); got != 2 {
		t.Fatalf("got %d entries after disabling NeedsInput, want unchanged 2", got)
	}
}

func TestStorePrunesAndPersistsPreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notifications.json")
	store, _, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	for i := 0; i < maxEntries+5; i++ {
		_, _, err := store.Add(Entry{ID: fmt.Sprintf("n%d", i), DedupeKey: fmt.Sprintf("k%d", i), CreatedAt: base.Add(time.Duration(i) * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(store.List()); got != maxEntries {
		t.Fatalf("got %d entries, want %d", got, maxEntries)
	}
	prefs := DefaultPreferences()
	prefs.Sound = false
	if err := store.SetPreferences(prefs); err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Preferences().Sound {
		t.Fatal("sound preference was not persisted")
	}
}

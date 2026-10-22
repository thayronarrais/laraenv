package agents

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestActivityRouterLocalAndIndependentPurposes(t *testing.T) {
	c, _, _, _ := coordinatorFixture(t)
	r := NewActivityRouter(c.opts.OwnerID, c, c.opts.Registry, nil)
	defer r.Close()
	ctx := context.Background()
	for _, p := range []ActivityInterestPurpose{ActivityInterestCounter, ActivityInterestDialog} {
		if err := r.SetInterest(ctx, ActivityInterest{"window", p, "session"}); err != nil {
			t.Fatal(err)
		}
	}
	r.RemoveInterest(ctx, "window", "dialog")
	c.mu.Lock()
	remaining := len(c.interests)
	c.mu.Unlock()
	if remaining != 1 {
		t.Fatal("dialog removal lost counter")
	}
	s, err := r.Get(ctx, "session")
	if err != nil || s.SourceID != c.opts.OwnerID {
		t.Fatalf("local route: %+v %v", s, err)
	}
}
func TestActivityOwnerIdentityRestart(t *testing.T) {
	registry := NewActivityOwnerRegistry(t.TempDir(), nil)
	defer registry.Close()
	identity, err := NewActivityOwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	oldCtx, cancelOld := context.WithCancel(context.Background())
	defer cancelOld()
	old, err := StartActivityOwnerWithIdentity(oldCtx, registry, &activityIPCBackend{}, identity)
	if err != nil {
		t.Fatal(err)
	}
	token := old.Descriptor().Token
	registry.SetTerminal(identity.ID, "term-1", true)
	old.Close()
	if live, _ := registry.ResolveTerminal(identity.ID, "term-1"); live {
		t.Fatal("closed owner retained membership")
	}
	next, err := StartActivityOwnerWithIdentity(context.Background(), registry, &activityIPCBackend{}, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	cancelOld()
	old.Close()
	request := httptest.NewRequest(http.MethodPost, next.Descriptor().Endpoint+"/get", strings.NewReader(`{"sessionId":"session"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	next.serve(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("new owner service accepted old token")
	}
	if d, ok, _ := registry.ResolveOwner(identity.ID); !ok || d.Token == token {
		t.Fatal("restart did not replace token or late close removed owner")
	}
	if live, _ := registry.ResolveTerminal(identity.ID, "term-1"); live {
		t.Fatal("restart invented live membership")
	}
	if err := registry.SetTerminal(identity.ID, "term-1", true); err != nil {
		t.Fatal(err)
	}
	bad := identity
	bad.PID++
	if server, err := StartActivityOwnerWithIdentity(context.Background(), registry, &activityIPCBackend{}, bad); err == nil {
		server.Close()
		t.Fatal("accepted foreign process identity")
	}
	bad = identity
	bad.CreatedAt = bad.CreatedAt.Add(time.Second)
	if server, err := StartActivityOwnerWithIdentity(context.Background(), registry, &activityIPCBackend{}, bad); err == nil {
		server.Close()
		t.Fatal("accepted wrong process creation")
	}
}

func TestActivityRegistryHasActiveBindingsIgnoresEndedAndForeignDead(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	if live, err := c.opts.Registry.HasActiveBindings(); err != nil || !live {
		t.Fatal("live root invisible to mutation guard")
	}
	c.EndRun("session", run.RunID, *now)
	if live, err := c.opts.Registry.HasActiveBindings(); err != nil || live {
		t.Fatal("ended root blocks mutation")
	}
}

type activityDelayedGet struct {
	ActivityBackend
	entered chan struct{}
	release chan struct{}
}

func (b *activityDelayedGet) Get(ctx context.Context, id string) (ActivitySnapshot, error) {
	snapshot, err := b.ActivityBackend.Get(ctx, id)
	close(b.entered)
	<-b.release
	return snapshot, err
}
func TestActivityRouterRejectsReplyAfterRunReplacement(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	backend := &activityDelayedGet{ActivityBackend: c, entered: make(chan struct{}), release: make(chan struct{})}
	router := NewActivityRouter(run.OwnerID, backend, c.opts.Registry, nil)
	defer router.Close()
	result := make(chan error, 1)
	go func() { _, err := router.Get(context.Background(), "session"); result <- err }()
	<-backend.entered
	c.EndRun("session", run.RunID, *now)
	run.RunID = "replacement"
	c.BeginRun(run)
	close(backend.release)
	if err := <-result; err == nil {
		t.Fatal("stale run response crossed router")
	}
}

type activityWaitProbe struct {
	ActivityBackend
	entered chan struct{}
	exited  chan struct{}
}

func (b *activityWaitProbe) Wait(ctx context.Context, id string, revision uint64) (ActivitySnapshot, error) {
	b.entered <- struct{}{}
	defer func() { b.exited <- struct{}{} }()
	return b.ActivityBackend.Wait(ctx, id, revision)
}
func TestActivityRouterRemoteDeduplicatesAndCancelsWatchers(t *testing.T) {
	registry := NewActivityOwnerRegistry(t.TempDir(), nil)
	defer registry.Close()
	identity, err := NewActivityOwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewActivityCoordinator(ActivityCoordinatorOptions{OwnerID: identity.ID, Registry: registry})
	defer coordinator.Close()
	probe := &activityWaitProbe{ActivityBackend: coordinator, entered: make(chan struct{}, 8), exited: make(chan struct{}, 8)}
	server, err := StartActivityOwnerWithIdentity(context.Background(), registry, probe, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	registry.SetTerminal(identity.ID, "term-1", true)
	coordinator.BeginRun(ActivityRun{OwnerID: identity.ID, RunID: "run", StartedAt: time.Now(), Session: AgentSession{ID: "session", AgentID: AgentClaude, TerminalID: "term-1", ProviderSessionID: "root"}})
	events := make(chan ActivitySnapshot, 8)
	router := NewActivityRouter("client", nil, registry, func(_ string, data ...any) { events <- data[0].(ActivitySnapshot) })
	defer router.Close()
	for _, purpose := range []ActivityInterestPurpose{ActivityInterestCounter, ActivityInterestDialog} {
		if err := router.SetInterest(context.Background(), ActivityInterest{"window", purpose, "session"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-probe.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("remote watcher did not wait")
		}
	}
	if len(events) != 1 {
		t.Fatalf("same revision emitted %d times", len(events))
	}
	first := <-events
	if first.SourceID != identity.ID {
		t.Fatal("remote source lost")
	}
	if err := router.RemoveInterest(context.Background(), "window", "dialog"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("removed watcher not cancelled")
	}
	coordinator.mu.Lock()
	remaining := len(coordinator.interests)
	coordinator.mu.Unlock()
	if remaining != 1 {
		t.Fatal("remote dialog removal lost counter")
	}
	router.Close()
	select {
	case <-probe.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("router shutdown did not cancel remaining watcher")
	}
}

func TestActivityRouterPreservesPhotoAfterOwnerLoss(t *testing.T) {
	c, _, _, run := coordinatorFixture(t)
	router := NewActivityRouter(run.OwnerID, c, c.opts.Registry, nil)
	defer router.Close()
	before, err := router.Get(context.Background(), "session")
	if err != nil {
		t.Fatal(err)
	}
	c.opts.Registry.removeOwner(run.OwnerID)
	after, err := router.Get(context.Background(), "session")
	if err != nil || after.RunID != before.RunID || len(after.Nodes) != len(before.Nodes) || !after.Stale || after.MessageCode != ActivityMessageOwnerUnavailable {
		t.Fatalf("lost owner erased photo: %+v %v", after, err)
	}
}

func TestActivityRouterRenewalTransfersOwnerAndCancelsOldWatcher(t *testing.T) {
	old, _, now, run := coordinatorFixture(t)
	probe := &activityWaitProbe{ActivityBackend: old, entered: make(chan struct{}, 8), exited: make(chan struct{}, 8)}
	events := make(chan ActivitySnapshot, 8)
	router := NewActivityRouter(run.OwnerID, probe, old.opts.Registry, func(_ string, data ...any) { events <- data[0].(ActivitySnapshot) })
	defer router.Close()
	interest := ActivityInterest{"window", ActivityInterestDialog, "session"}
	if err := router.SetInterest(context.Background(), interest); err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("old watcher not started")
	}
	old.EndRun("session", run.RunID, *now)
	identity, err := NewActivityOwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	next := NewActivityCoordinator(ActivityCoordinatorOptions{OwnerID: identity.ID, Registry: old.opts.Registry})
	defer next.Close()
	server, err := StartActivityOwnerWithIdentity(context.Background(), old.opts.Registry, next, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	old.opts.Registry.SetTerminal(identity.ID, "term-2", true)
	run.OwnerID = identity.ID
	run.RunID = "new-owner-run"
	run.Session.TerminalID = "term-2"
	next.BeginRun(run)
	if err := router.SetInterest(context.Background(), interest); err != nil {
		t.Fatal(err)
	}
	select {
	case <-probe.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("old owner watcher survived transfer")
	}
	select {
	case snapshot := <-events:
		if snapshot.SourceID != identity.ID || snapshot.RunID != "new-owner-run" {
			t.Fatal("old owner snapshot re-emitted after transfer")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("new owner snapshot not emitted")
	}
	old.mu.Lock()
	count := len(old.interests)
	old.mu.Unlock()
	if count != 0 {
		t.Fatal("old owner interest was not removed")
	}
}

func TestActivityRouterRetainsAtMostTwentyEndedPhotos(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	c.EndRun("session", run.RunID, *now)
	router := NewActivityRouter(run.OwnerID, c, c.opts.Registry, nil)
	defer router.Close()
	for i := 0; i < 25; i++ {
		run.Session.ID = fmt.Sprintf("ended-%d", i)
		run.RunID = fmt.Sprintf("run-%d", i)
		*now = now.Add(time.Second)
		run.StartedAt = *now
		c.BeginRun(run)
		c.EndRun(run.Session.ID, run.RunID, *now)
		if _, err := router.Get(context.Background(), run.Session.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := router.Get(context.Background(), "ended-0"); err == nil {
		t.Fatal("router retained more than twenty ended photographs")
	}
	if _, err := router.Get(context.Background(), "ended-24"); err != nil {
		t.Fatal("router evicted newest ended photograph")
	}
}

type activityOrderedLeaseBackend struct {
	ActivityBackend
	removeEntered chan struct{}
	releaseRemove chan struct{}
	setEntered    chan struct{}
	blockRemove   atomic.Bool
}

func (b *activityOrderedLeaseBackend) SetInterest(ctx context.Context, i ActivityInterest) error {
	err := b.ActivityBackend.SetInterest(ctx, i)
	select {
	case b.setEntered <- struct{}{}:
	default:
	}
	return err
}
func (b *activityOrderedLeaseBackend) RemoveInterest(ctx context.Context, w, p string) error {
	if b.blockRemove.CompareAndSwap(true, false) {
		close(b.removeEntered)
		select {
		case <-b.releaseRemove:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return b.ActivityBackend.RemoveInterest(ctx, w, p)
}
func TestActivityRouterRemovalCannotEraseConcurrentReplacementLease(t *testing.T) {
	c, _, _, run := coordinatorFixture(t)
	backend := &activityOrderedLeaseBackend{ActivityBackend: c, removeEntered: make(chan struct{}), releaseRemove: make(chan struct{}), setEntered: make(chan struct{}, 4)}
	router := NewActivityRouter(run.OwnerID, backend, c.opts.Registry, nil)
	defer router.Close()
	interest := ActivityInterest{"same-window", ActivityInterestDialog, "session"}
	if err := router.SetInterest(context.Background(), interest); err != nil {
		t.Fatal(err)
	}
	<-backend.setEntered
	backend.blockRemove.Store(true)
	removed := make(chan error, 1)
	go func() {
		removed <- router.RemoveInterest(context.Background(), interest.WindowID, string(interest.Purpose))
	}()
	<-backend.removeEntered
	setDone := make(chan error, 1)
	go func() { setDone <- router.SetInterest(context.Background(), interest) }()
	// The blocked removal is the barrier: a replacement backend mutation must
	// wait behind it. The timeout only bounds the assertion of no overtaking.
	overtook := false
	select {
	case <-backend.setEntered:
		overtook = true
	case <-time.After(250 * time.Millisecond):
	}
	close(backend.releaseRemove)
	if err := <-removed; err != nil {
		t.Fatal(err)
	}
	if err := <-setDone; err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	_, present := c.interests[activityInterestKey(interest.WindowID, string(interest.Purpose))]
	c.mu.Unlock()
	if overtook || !present {
		t.Fatal("old removal erased or overtook replacement lease")
	}
}

type activityFailingGet struct {
	ActivityBackend
	fail atomic.Bool
}

func (b *activityFailingGet) Get(ctx context.Context, id string) (ActivitySnapshot, error) {
	if b.fail.Load() {
		return ActivitySnapshot{}, ErrActivityOwnerUnavailable
	}
	return b.ActivityBackend.Get(ctx, id)
}
func TestActivityRouterUnavailableCannotReturnSupersededCachedRun(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	backend := &activityFailingGet{ActivityBackend: c}
	router := NewActivityRouter(run.OwnerID, backend, c.opts.Registry, nil)
	defer router.Close()
	before, err := router.Get(context.Background(), "session")
	if err != nil {
		t.Fatal(err)
	}
	c.EndRun("session", run.RunID, *now)
	run.RunID = "replacement"
	c.BeginRun(run)
	backend.fail.Store(true)
	after, err := router.Get(context.Background(), "session")
	if err == nil && after.RunID == before.RunID {
		t.Fatal("replacement failure returned superseded cached run")
	}
	if err == nil {
		t.Fatal("unavailable replacement should not claim a successful snapshot")
	}
}

type activityCanceledRemoteRemoval struct {
	ActivityBackend
	entered, canceled, release chan struct{}
	finished                   chan error
	block                      atomic.Bool
}

func (b *activityCanceledRemoteRemoval) RemoveInterest(ctx context.Context, window, purpose string) error {
	if b.block.CompareAndSwap(true, false) {
		close(b.entered)
		<-ctx.Done()
		close(b.canceled)
		<-b.release
		err := b.ActivityBackend.RemoveInterest(ctx, window, purpose)
		b.finished <- err
		return err
	}
	return b.ActivityBackend.RemoveInterest(ctx, window, purpose)
}
func TestActivityRouterCanceledRemoteRemovalCannotEraseReplacement(t *testing.T) {
	registry := NewActivityOwnerRegistry(t.TempDir(), nil)
	defer registry.Close()
	identity, err := NewActivityOwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	c := NewActivityCoordinator(ActivityCoordinatorOptions{OwnerID: identity.ID, Registry: registry})
	defer c.Close()
	backend := &activityCanceledRemoteRemoval{ActivityBackend: c, entered: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{}), finished: make(chan error, 1)}
	server, err := StartActivityOwnerWithIdentity(context.Background(), registry, backend, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := registry.SetTerminal(identity.ID, "term-1", true); err != nil {
		t.Fatal(err)
	}
	c.BeginRun(ActivityRun{OwnerID: identity.ID, RunID: "run", StartedAt: time.Now(), Session: AgentSession{ID: "session", AgentID: AgentCodex, TerminalID: "term-1", ProviderSessionID: "root"}})
	router := NewActivityRouter("another-window", nil, registry, nil)
	defer router.Close()
	interest := ActivityInterest{"window", ActivityInterestDialog, "session"}
	if err := router.SetInterest(context.Background(), interest); err != nil {
		t.Fatal(err)
	}
	backend.block.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	removed := make(chan error, 1)
	go func() { removed <- router.RemoveInterest(ctx, interest.WindowID, string(interest.Purpose)) }()
	select {
	case <-backend.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("remote removal did not enter handler")
	}
	cancel()
	if err := <-removed; err == nil {
		t.Fatal("canceled removal unexpectedly succeeded")
	}
	select {
	case <-backend.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not observe client cancellation")
	}
	if err := router.SetInterest(context.Background(), interest); err != nil {
		t.Fatal(err)
	}
	close(backend.release)
	<-backend.finished
	c.mu.Lock()
	_, present := c.interests[activityInterestKey(interest.WindowID, string(interest.Purpose))]
	c.mu.Unlock()
	if !present {
		t.Fatal("canceled remote removal erased replacement lease after client returned")
	}
}

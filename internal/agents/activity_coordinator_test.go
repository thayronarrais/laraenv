package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestActivityCoordinatorRealReaderPreservesHookCoverage(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			c, _, now, run := coordinatorFixture(t)
			c.opts.HooksEnabled = func() bool { return enabled }
			c.opts.Reader = NewCodexActivityReader(func(context.Context, string) (CodexActivityTransport, error) {
				return &activityCodexScript{t: t, pages: []json.RawMessage{codexActivityPage(codexActivityChild("child", "root"))}}, nil
			})
			got, err := c.Refresh(t.Context(), run.Session.ID)
			want := ActivityMessageHooksUnobserved
			if !enabled {
				want = ActivityMessageHooksDisabled
			}
			if err != nil || got.MessageCode != want || got.Status != ActivityPartial || got.Capabilities.LiveState || len(got.Nodes) != 2 {
				t.Fatalf("successful identity read lost coverage: %+v %v", got, err)
			}
			// Disabled collection cannot receive live evidence until enabled.
			c.opts.HooksEnabled = func() bool { return true }
			c.ObserveActivity(activityFixtureEvent(run, *now, ActivitySubagentStart, "child"))
			got, err = c.Refresh(t.Context(), run.Session.ID)
			if err != nil || got.MessageCode != ActivityMessageNone || got.Status != ActivityAvailable || !got.Capabilities.LiveState {
				t.Fatalf("confirmed hook coverage missing: %+v %v", got, err)
			}
			c.opts.HooksEnabled = func() bool { return false }
			got, err = c.Refresh(t.Context(), run.Session.ID)
			if err != nil || got.MessageCode != ActivityMessageHooksDisabled || got.Status != ActivityPartial {
				t.Fatalf("historical live evidence hid current opt-out: %+v %v", got, err)
			}
		})
	}
}

func TestActivityCoordinatorHookCoveragePreservesStrongerNotices(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result ActivityMetadataResult
		err    error
		want   ActivityMessageCode
	}{
		{"unsupported", ActivityMetadataResult{Status: ActivityUnavailable, MessageCode: ActivityMessageUnsupportedProtocol}, nil, ActivityMessageUnsupportedProtocol},
		{"truncated", ActivityMetadataResult{Status: ActivityPartial, MessageCode: ActivityMessageTruncated, Truncated: true}, nil, ActivityMessageTruncated},
		{"trust", ActivityMetadataResult{Status: ActivityPartial, MessageCode: ActivityMessageHookTrustRequired}, nil, ActivityMessageHookTrustRequired},
		{"failure", ActivityMetadataResult{}, errors.New("synthetic read failure"), ActivityMessageRefreshFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, now, run := coordinatorFixture(t)
			c.opts.HooksEnabled = func() bool { return false }
			c.opts.Reader = activityReaderFunc(func(context.Context, ActivityMetadataQuery) (ActivityMetadataResult, error) { return tc.result, tc.err })
			for _, live := range []bool{false, true} {
				if live {
					c.opts.HooksEnabled = func() bool { return true }
					c.ObserveActivity(activityFixtureEvent(run, *now, ActivitySubagentStart, "child"))
				}
				got, err := c.Refresh(t.Context(), run.Session.ID)
				if err != nil || got.MessageCode != tc.want {
					t.Fatalf("coverage hid stronger notice: %+v %v", got, err)
				}
			}
		})
	}
}

func TestActivityCoordinatorOptOutInvalidatesLiveCoverageBehindStrongerNotice(t *testing.T) {
	for _, notice := range []ActivityMessageCode{ActivityMessageTruncated, ActivityMessageUnsupportedProtocol, ActivityMessageHookTrustRequired} {
		t.Run(string(notice), func(t *testing.T) {
			c, _, now, run := coordinatorFixture(t)
			enabled := true
			c.opts.HooksEnabled = func() bool { return enabled }
			c.ObserveActivity(activityFixtureEvent(run, *now, ActivitySubagentStart, "child"))
			c.ObserveActivity(activityFixtureEvent(run, *now, ActivitySubagentStart, "completed"))
			tasks := []ActivityBackgroundTask{{ProviderNodeID: "task", State: ActivityWorking, StartedAt: activityTime(*now)}}
			inventory := activityFixtureEvent(run, *now, ActivityBackgroundInventory, "")
			inventory.BackgroundTasks = &tasks
			c.ObserveActivity(inventory)
			*now = now.Add(time.Second)
			c.ObserveActivity(activityFixtureEvent(run, *now, ActivitySubagentTurnStop, "completed"))
			c.opts.Reader = NewCodexActivityReader(func(context.Context, string) (CodexActivityTransport, error) {
				page, err := json.Marshal(map[string]any{"data": []map[string]any{codexActivityChild("child", "root")}, "nextCursor": "repeat"})
				if err != nil {
					t.Fatal(err)
				}
				return &activityCodexScript{t: t, pages: []json.RawMessage{page, page}}, nil
			})
			if notice != ActivityMessageTruncated {
				c.opts.Reader = activityReaderFunc(func(context.Context, ActivityMetadataQuery) (ActivityMetadataResult, error) {
					return ActivityMetadataResult{Status: ActivityPartial, MessageCode: notice}, nil
				})
			}
			enabled = false
			for _, stillDisabled := range []bool{true, false} {
				enabled = !stillDisabled
				got, err := c.Refresh(t.Context(), run.Session.ID)
				if err != nil || got.MessageCode != notice || got.Stale {
					t.Fatalf("stronger metadata notice changed: %+v %v", got, err)
				}
				if got.Capabilities.LiveState {
					t.Errorf("old live evidence survives opt-out or metadata reenables it: disabled=%v", stillDisabled)
				}
				child, done := got.Nodes[activityFind(got.Nodes, "child")], got.Nodes[activityFind(got.Nodes, "completed")]
				if child.State != ActivityUnknown {
					t.Errorf("open child retained current state across opt-out: %s", child.State)
				}
				if len(got.BackgroundTasks) != 1 || got.BackgroundTasks[0].State != ActivityUnknown || got.BackgroundTasks[0].EndedAt != nil || got.BackgroundTasks[0].StartedAt == nil {
					t.Fatal("open task coverage or interval incorrect")
				}
				if child.StartedAt == nil || child.EndedAt != nil || done.EndedAt == nil || done.EndedAt.Sub(*done.StartedAt) != time.Second {
					t.Fatal("coverage invalidation invented or erased intervals")
				}
			}
			*now = now.Add(time.Second)
			c.ObserveActivity(activityFixtureEvent(run, *now, ActivitySubagentStart, "other-child"))
			got, err := c.Refresh(t.Context(), run.Session.ID)
			if err != nil || !got.Capabilities.LiveState || got.Nodes[activityFind(got.Nodes, "child")].State != ActivityUnknown {
				t.Fatal("other child's evidence restored unconfirmed old child")
			}
			c.ObserveActivity(activityFixtureEvent(run, *now, ActivitySubagentStart, "child"))
			got, err = c.Refresh(t.Context(), run.Session.ID)
			if err != nil || !got.Capabilities.LiveState || got.MessageCode != notice {
				t.Fatal("new live evidence not restored independently of notice")
			}
		})
	}
}

func TestActivityCoordinatorTickInvalidatesOptOutWithoutMetadataRead(t *testing.T) {
	for _, provider := range []AgentID{AgentClaude, AgentCodex} {
		t.Run(string(provider), func(t *testing.T) {
			c, reader, now, run := coordinatorFixture(t)
			c.EndRun(run.Session.ID, run.RunID, *now)
			run.RunID, run.Session.AgentID = "current-run", provider
			c.BeginRun(run)
			enabled := true
			c.opts.HooksEnabled = func() bool { return enabled }
			c.ObserveActivity(activityFixtureEvent(run, *now, ActivitySubagentStart, "child"))
			c.ObserveActivity(activityFixtureEvent(run, *now, ActivitySubagentStart, "completed"))
			*now = now.Add(time.Second)
			c.ObserveActivity(activityFixtureEvent(run, *now, ActivitySubagentTurnStop, "completed"))
			c.opts.Store.MergeMetadata(run.Session.ID, run.RunID, ActivityMetadataResult{MessageCode: ActivityMessageTruncated, Status: ActivityPartial, Truncated: true, ObservedAt: *now})
			enabled = false
			c.Tick(t.Context(), *now)
			got, err := c.Get(t.Context(), run.Session.ID)
			if err != nil || got.Capabilities.LiveState || got.Nodes[activityFind(got.Nodes, "child")].State != ActivityUnknown {
				t.Errorf("runtime opt-out retained live coverage without metadata poll: %+v %v", got, err)
			}
			if got.MessageCode != ActivityMessageTruncated || got.Status != ActivityPartial || got.Stale || got.Nodes[activityFind(got.Nodes, "completed")].EndedAt == nil {
				t.Fatal("coverage invalidation changed banner or completed interval")
			}
			revision := got.Revision
			for i := 0; i < 4; i++ {
				*now = now.Add(100 * time.Millisecond)
				c.Tick(t.Context(), *now)
			}
			got, _ = c.Get(t.Context(), run.Session.ID)
			if got.Revision != revision {
				t.Fatal("disabled ticks churn revisions")
			}
			enabled = true
			c.Tick(t.Context(), *now)
			got, _ = c.Get(t.Context(), run.Session.ID)
			if got.Capabilities.LiveState || reader.count() != 0 {
				t.Fatal("config alone restored live coverage or required a metadata query")
			}
		})
	}
}

type coordinatorReader struct {
	mu          sync.Mutex
	calls       int
	err         error
	entered     chan ActivityMetadataQuery
	release     chan struct{}
	active, max int
}

type activityCheckedContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (c *activityCheckedContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.checked) })
	return err
}
func TestActivityCoordinatorInterestCanceledWhileWaitingForMutationLock(t *testing.T) {
	c, _, _, _ := coordinatorFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checked := &activityCheckedContext{Context: ctx, checked: make(chan struct{})}
	c.mu.Lock()
	finished := make(chan error, 1)
	go func() {
		finished <- c.SetInterest(checked, ActivityInterest{"window", ActivityInterestDialog, "session"})
	}()
	<-checked.checked
	cancel()
	c.mu.Unlock()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled queued interest mutated owner: %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.interests) != 0 {
		t.Fatal("canceled queued interest left a lease")
	}
}

func (r *coordinatorReader) Read(ctx context.Context, q ActivityMetadataQuery) (ActivityMetadataResult, error) {
	r.mu.Lock()
	r.calls++
	r.active++
	if r.active > r.max {
		r.max = r.active
	}
	err := r.err
	r.mu.Unlock()
	defer func() { r.mu.Lock(); r.active--; r.mu.Unlock() }()
	if r.entered != nil {
		r.entered <- q
	}
	if r.release != nil {
		select {
		case <-r.release:
		case <-ctx.Done():
			return ActivityMetadataResult{}, ctx.Err()
		}
	}
	return ActivityMetadataResult{Status: ActivityPartial, MessageCode: ActivityMessageHooksUnobserved}, err
}
func (r *coordinatorReader) count() int { r.mu.Lock(); defer r.mu.Unlock(); return r.calls }
func coordinatorFixture(t *testing.T) (*ActivityCoordinator, *coordinatorReader, *time.Time, ActivityRun) {
	t.Helper()
	now := time.Now().UTC()
	reg := NewActivityOwnerRegistry(t.TempDir(), func(ActivityOwnerIdentity) bool { return true })
	owner := "11111111-1111-4111-8111-111111111111"
	if err := reg.Register(ActivityOwnerDescriptor{Identity: ActivityOwnerIdentity{ID: owner, PID: 1, CreatedAt: now}, Endpoint: "http://127.0.0.1:12345", Token: "synthetic"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetTerminal(owner, "term-1", true); err != nil {
		t.Fatal(err)
	}
	reader := &coordinatorReader{}
	c := NewActivityCoordinator(ActivityCoordinatorOptions{OwnerID: owner, Registry: reg, Reader: reader, Now: func() time.Time { return now }, Executable: func(AgentSession) string { return "synthetic-codex" }})
	run := ActivityRun{OwnerID: owner, RunID: "run-1", StartedAt: now, Session: AgentSession{ID: "session", AgentID: AgentCodex, TerminalID: "term-1", ProviderSessionID: "root", State: SessionWorking}}
	c.BeginRun(run)
	t.Cleanup(func() { c.Close(); reg.Close() })
	return c, reader, &now, run
}
func coordinatorIdle(c *ActivityCoordinator) {
	c.mu.Lock()
	var done []chan struct{}
	for _, r := range c.runs {
		if r.done != nil {
			done = append(done, r.done)
		}
	}
	c.mu.Unlock()
	for _, d := range done {
		<-d
	}
}
func TestActivityCoordinatorIntervalsAndExpiry(t *testing.T) {
	c, r, now, _ := coordinatorFixture(t)
	ctx := context.Background()
	c.SetInterest(ctx, ActivityInterest{WindowID: "w", Purpose: ActivityInterestCounter, SessionID: "session"})
	c.Tick(ctx, *now)
	coordinatorIdle(c)
	if r.count() != 1 {
		t.Fatal("counter did not start")
	}
	*now = now.Add(9 * time.Second)
	c.Tick(ctx, *now)
	coordinatorIdle(c)
	if r.count() != 1 {
		t.Fatal("counter polled before 10s")
	}
	*now = now.Add(time.Second)
	c.Tick(ctx, *now)
	coordinatorIdle(c)
	if r.count() != 2 {
		t.Fatal("counter missed 10s")
	}
	c.SetInterest(ctx, ActivityInterest{WindowID: "w", Purpose: ActivityInterestDialog, SessionID: "session"})
	*now = now.Add(3 * time.Second)
	c.Tick(ctx, *now)
	coordinatorIdle(c)
	if r.count() != 3 {
		t.Fatal("dialog missed 3s")
	}
	*now = now.Add(30 * time.Second)
	c.Tick(ctx, *now)
	coordinatorIdle(c)
	if r.count() != 3 {
		t.Fatal("expired interest polled")
	}
}
func TestActivityCoordinatorIndependentPurposes(t *testing.T) {
	c, r, now, _ := coordinatorFixture(t)
	ctx := context.Background()
	for _, p := range []ActivityInterestPurpose{ActivityInterestCounter, ActivityInterestDialog} {
		c.SetInterest(ctx, ActivityInterest{"w", p, "session"})
	}
	c.Tick(ctx, *now)
	coordinatorIdle(c)
	c.RemoveInterest(ctx, "w", "dialog")
	*now = now.Add(10 * time.Second)
	c.Tick(ctx, *now)
	coordinatorIdle(c)
	if r.count() != 2 {
		t.Fatal("removing dialog removed counter")
	}
}
func TestActivityCoordinatorBackoffAndManualRefresh(t *testing.T) {
	c, r, now, _ := coordinatorFixture(t)
	r.err = errors.New("synthetic failure")
	ctx := context.Background()
	c.SetInterest(ctx, ActivityInterest{"w", ActivityInterestDialog, "session"})
	c.Tick(ctx, *now)
	coordinatorIdle(c)
	for i, delay := range []time.Duration{10 * time.Second, 30 * time.Second, 60 * time.Second} {
		*now = now.Add(delay - time.Nanosecond)
		c.SetInterest(ctx, ActivityInterest{"w", ActivityInterestDialog, "session"})
		c.Tick(ctx, *now)
		coordinatorIdle(c)
		if r.count() != i+1 {
			t.Fatal("retry before backoff")
		}
		*now = now.Add(time.Nanosecond)
		c.Tick(ctx, *now)
		coordinatorIdle(c)
		if r.count() != i+2 {
			t.Fatal("retry missed backoff")
		}
	}
	c.Refresh(ctx, "session")
	if r.count() != 5 {
		t.Fatal("manual retry did not bypass backoff")
	}
}
func TestActivityCoordinatorSerializesExecutable(t *testing.T) {
	c, r, now, run := coordinatorFixture(t)
	ctx := context.Background()
	r.entered = make(chan ActivityMetadataQuery, 2)
	r.release = make(chan struct{})
	run.Session.ID = "second"
	run.RunID = "run-2"
	run.Session.TerminalID = "term-2"
	run.Session.ProviderSessionID = "root-2"
	c.opts.Registry.SetTerminal(run.OwnerID, "term-2", true)
	c.BeginRun(run)
	c.SetInterest(ctx, ActivityInterest{"w1", ActivityInterestDialog, "session"})
	c.SetInterest(ctx, ActivityInterest{"w2", ActivityInterestDialog, "second"})
	c.Tick(ctx, *now)
	<-r.entered
	close(r.release)
	coordinatorIdle(c)
	if r.max != 1 {
		t.Fatal("overlapping refreshes")
	}
	if r.count() != 2 {
		t.Fatal("queued executable refresh lost")
	}
}
func TestActivityCoordinatorPendingAdoptionBound(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	c.EndRun(run.Session.ID, run.RunID, *now)
	run.RunID = "next"
	run.Session.TerminalID = ""
	c.ReserveRun(run)
	for i := 0; i < 150; i++ {
		if i%2 == 0 {
			c.ObserveActivity(ActivityEvent{OwnerID: run.OwnerID, TerminalID: "term-1", SessionID: "session", RunID: "next", Provider: AgentCodex, ProviderSessionID: "root", NodeID: fmt.Sprint(i), Kind: ActivitySubagentStart, ReceivedAt: *now})
		} else {
			c.ObserveSessionSignal(SessionSignal{OwnerID: run.OwnerID, TerminalID: "term-1", SessionID: "session", RunID: "next", Provider: AgentCodex, Kind: SignalDone, CreatedAt: *now})
		}
	}
	if len(c.pending) != 128 {
		t.Fatalf("backlog %d", len(c.pending))
	}
	*now = now.Add(5 * time.Second)
	c.Tick(context.Background(), *now)
	if len(c.pending) != 0 {
		t.Fatal("adoption backlog expired late")
	}
}
func TestActivityCoordinatorBatchesEvents(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	emits := 0
	c.opts.Emit = func(_ string, _ ...any) { emits++ }
	for i := 0; i < 3; i++ {
		c.ObserveActivity(ActivityEvent{OwnerID: run.OwnerID, TerminalID: "term-1", SessionID: "session", RunID: run.RunID, Provider: AgentCodex, ProviderSessionID: "root", NodeID: fmt.Sprint(i), Kind: ActivitySubagentStart, ReceivedAt: *now})
	}
	*now = now.Add(199 * time.Millisecond)
	c.Tick(context.Background(), *now)
	if emits != 0 {
		t.Fatal("batch emitted early")
	}
	*now = now.Add(time.Millisecond)
	c.Tick(context.Background(), *now)
	if emits != 1 {
		t.Fatal("batch not emitted by 200ms")
	}
	s, _ := c.Get(context.Background(), "session")
	if len(s.Nodes) != 4 {
		t.Fatal("batch lost nodes")
	}
}
func TestActivityCoordinatorSignalReplayOutsideLocks(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	c.EndRun("session", run.RunID, *now)
	run.RunID = "next"
	run.Session.ProviderSessionID = ""
	c.ReserveRun(run)
	calls := 0
	c.opts.ObserveSession = func(s SessionSignal) { calls++; c.RootChanged(run.Session, run.RunID) }
	c.ObserveSessionSignal(SessionSignal{OwnerID: run.OwnerID, TerminalID: "term-1", SessionID: "session", RunID: "next", Provider: AgentCodex, Kind: SignalDone, CreatedAt: *now})
	c.BeginRun(run)
	if calls != 1 {
		t.Fatal("reserved signal was not replayed")
	}
}

func TestActivityCoordinatorMetadataKeepsObservedHookCoverage(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	c.ObserveActivity(ActivityEvent{OwnerID: run.OwnerID, TerminalID: "term-1", SessionID: "session", RunID: run.RunID, Provider: AgentCodex, ProviderSessionID: "root", NodeID: "child", Kind: ActivitySubagentStart, ReceivedAt: *now})
	snapshot, err := c.Refresh(context.Background(), "session")
	if err != nil || snapshot.MessageCode == ActivityMessageHooksUnobserved {
		t.Fatal("metadata erased observed hook coverage")
	}
}
func TestActivityCoordinatorRejectsStaleAndClosedEnvelopes(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	event := ActivityEvent{OwnerID: run.OwnerID, TerminalID: "term-1", SessionID: "session", RunID: run.RunID, Provider: AgentCodex, ProviderSessionID: "root", NodeID: "child", Kind: ActivitySubagentStart, ReceivedAt: *now}
	c.EndRun("session", run.RunID, *now)
	run.RunID = "next"
	c.BeginRun(run)
	c.ObserveActivity(event)
	event.RunID = "next"
	c.opts.Registry.SetTerminal(run.OwnerID, "term-1", false)
	c.ObserveActivity(event)
	snapshot, _ := c.Get(context.Background(), "session")
	if len(snapshot.Nodes) != 1 {
		t.Fatal("old run or closed terminal authorized activity")
	}
}
func TestActivityCoordinatorAliasesAndAdoptionReplay(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	c.EndRun("session", run.RunID, *now)
	e := ActivityEvent{OwnerID: run.OwnerID, TerminalID: "term-1", Provider: AgentCodex, ProviderSessionID: "adopt-root", NodeID: "child", Kind: ActivitySubagentStart, ReceivedAt: *now}
	c.ObserveActivity(e)
	*now = now.Add(time.Second)
	run.RunID = "adopt"
	run.StartedAt = *now
	run.Session.Adopted = true
	run.Session.ProviderSessionID = "adopt-root"
	c.BeginRun(run)
	b, ok, err := c.opts.Registry.ResolveProvider(AgentCodex, "child")
	if err != nil || !ok || b.ProviderSessionID != "adopt-root" {
		t.Fatal("verified adopted child alias missing")
	}
	c.EndRun("session", run.RunID, *now)
	if _, ok, _ := c.opts.Registry.ResolveProvider(AgentCodex, "child"); ok {
		t.Fatal("ended child alias remained authorized")
	}
}
func TestActivityCoordinatorFailedSpawnDiscardsSharedBacklog(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	c.EndRun("session", run.RunID, *now)
	run.RunID = "failed"
	c.ReserveRun(run)
	c.ObserveSessionSignal(SessionSignal{OwnerID: run.OwnerID, TerminalID: "term-1", SessionID: "session", RunID: "failed", Provider: AgentCodex, Kind: SignalDone, CreatedAt: *now})
	c.EndRun("session", "failed", *now)
	if len(c.pending) != 0 || len(c.reserved) != 0 {
		t.Fatal("failed spawn retained reservation or pending signals")
	}
}
func TestActivityCoordinatorRestartKeepsSourceRevision(t *testing.T) {
	c, reader, now, run := coordinatorFixture(t)
	old, _ := c.Get(context.Background(), "session")
	c.Close()
	next := NewActivityCoordinator(ActivityCoordinatorOptions{OwnerID: run.OwnerID, Registry: c.opts.Registry, Store: c.opts.Store, Reader: reader, Now: func() time.Time { return *now }})
	defer next.Close()
	run.RunID = "after-toggle"
	next.BeginRun(run)
	snapshot, _ := next.Get(context.Background(), "session")
	if snapshot.SourceID != old.SourceID || snapshot.Revision <= old.Revision {
		t.Fatal("same-source revision moved backwards")
	}
}

func TestActivityCoordinatorLookupAndReaderShareFiveSecondDeadline(t *testing.T) {
	c, r, _, _ := coordinatorFixture(t)
	var lookupDeadline time.Time
	c.opts.LookupExecutable = func(ctx context.Context, _ AgentSession) (string, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) < 4*time.Second {
			t.Error("lookup not bounded by total five seconds")
		}
		lookupDeadline = deadline
		return "synthetic", nil
	}
	c.opts.Reader = activityReaderFunc(func(ctx context.Context, _ ActivityMetadataQuery) (ActivityMetadataResult, error) {
		deadline, _ := ctx.Deadline()
		if deadline != lookupDeadline {
			t.Error("reader received a second timeout budget")
		}
		return ActivityMetadataResult{}, nil
	})
	if _, err := c.Refresh(context.Background(), "session"); err != nil {
		t.Fatal(err)
	}
	if r.count() != 0 {
		t.Fatal("wrong reader used")
	}
}

type activityReaderFunc func(context.Context, ActivityMetadataQuery) (ActivityMetadataResult, error)

func (f activityReaderFunc) Read(ctx context.Context, q ActivityMetadataQuery) (ActivityMetadataResult, error) {
	return f(ctx, q)
}

func TestActivityCoordinatorDrainRetainsEarlyAdoptionActivity(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	c.EndRun("session", run.RunID, *now)
	run.RunID = "adopted"
	run.Session.Adopted = true
	run.Session.ProviderSessionID = "adopt-root"
	event := ActivityEvent{OwnerID: run.OwnerID, TerminalID: "term-1", Provider: AgentCodex, ProviderSessionID: "adopt-root", NodeID: "early-child", Kind: ActivitySubagentStart, ReceivedAt: *now}
	activityDir, _ := c.opts.Registry.QueueDir(run.OwnerID, "activity")
	sessionDir, _ := c.opts.Registry.QueueDir(run.OwnerID, "session")
	if err := WriteActivitySignal(activityDir, event); err != nil {
		t.Fatal(err)
	}
	if err := WriteSignal(sessionDir, SessionSignal{OwnerID: run.OwnerID, TerminalID: "term-1", Provider: AgentCodex, ProviderSessionID: "adopt-root", Kind: SignalStarted, Cwd: "C:/synthetic"}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	c.opts.ObserveSession = func(SessionSignal) { run.StartedAt = *now; c.BeginRun(run) }
	c.drain()
	snapshot, _ := c.Get(context.Background(), "session")
	if len(snapshot.Nodes) != 2 {
		t.Fatal("session-first drain discarded early adoption activity")
	}
}

func TestActivityCoordinatorBatchDeadlineDoesNotSlideWithTickPhase(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	ctx := context.Background()
	*now = now.Add(200 * time.Millisecond)
	c.Tick(ctx, *now)
	emits := 0
	c.opts.Emit = func(string, ...any) { emits++ }
	*now = now.Add(50 * time.Millisecond)
	c.ObserveActivity(ActivityEvent{OwnerID: run.OwnerID, TerminalID: "term-1", SessionID: "session", RunID: run.RunID, Provider: AgentCodex, ProviderSessionID: "root", NodeID: "child", Kind: ActivitySubagentStart, ReceivedAt: *now})
	*now = now.Add(150 * time.Millisecond)
	c.Tick(ctx, *now)
	if emits != 1 {
		t.Fatal("event missed next 200ms batch boundary")
	}
}

func TestActivityCoordinatorLateMetadataCannotChangeReplacement(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	c.opts.Reader = activityReaderFunc(func(ctx context.Context, _ ActivityMetadataQuery) (ActivityMetadataResult, error) {
		entered <- ctx
		<-release
		return ActivityMetadataResult{Nodes: []ActivityMetadataNode{{ProviderNodeID: "old-child", ParentProviderID: "root"}}, Status: ActivityAvailable}, nil
	})
	c.SetInterest(context.Background(), ActivityInterest{"w", ActivityInterestDialog, "session"})
	c.Tick(context.Background(), *now)
	oldContext := <-entered
	c.mu.Lock()
	oldDone := c.runs["session"].done
	c.mu.Unlock()
	c.EndRun("session", run.RunID, *now)
	run.RunID = "replacement"
	c.BeginRun(run)
	select {
	case <-oldContext.Done():
	default:
		t.Fatal("old metadata request was not cancelled")
	}
	close(release)
	<-oldDone
	snapshot, _ := c.Get(context.Background(), "session")
	if snapshot.RunID != "replacement" || len(snapshot.Nodes) != 1 {
		t.Fatal("late metadata changed replacement run")
	}
}

func TestActivityCoordinatorExpiresInterestsAfterRootEnds(t *testing.T) {
	c, _, now, run := coordinatorFixture(t)
	c.SetInterest(context.Background(), ActivityInterest{"window", ActivityInterestDialog, "session"})
	c.EndRun("session", run.RunID, *now)
	*now = now.Add(30 * time.Second)
	c.Tick(context.Background(), *now)
	c.mu.Lock()
	count := len(c.interests)
	c.mu.Unlock()
	if count != 0 {
		t.Fatal("ended root retained expired lease")
	}
}

func TestActivityCoordinatorReservedAdoptionDoesNotRelaxManagedOrFencedRuns(t *testing.T) {
	for _, test := range []struct {
		name              string
		terminalAtReserve string
		fenced            bool
		wantNodes         int
	}{{"adopted-reserve", "term-1", false, 2}, {"managed-resume-reserve", "", false, 1}, {"explicit-run-event", "term-1", true, 1}} {
		t.Run(test.name, func(t *testing.T) {
			c, _, now, run := coordinatorFixture(t)
			c.EndRun("session", run.RunID, *now)
			run.RunID = "next"
			run.Session.Adopted = true
			run.Session.ProviderSessionID = "adopt-root"
			receipt := *now
			*now = now.Add(time.Second)
			run.StartedAt = *now
			reservation := run
			reservation.Session.TerminalID = test.terminalAtReserve
			c.ReserveRun(reservation)
			event := ActivityEvent{OwnerID: run.OwnerID, TerminalID: "term-1", Provider: AgentCodex, ProviderSessionID: "adopt-root", NodeID: "child", Kind: ActivitySubagentStart, ReceivedAt: receipt}
			if test.fenced {
				event.SessionID = run.Session.ID
				event.RunID = run.RunID
			}
			c.ObserveActivity(event)
			c.BeginRun(run)
			snapshot, err := c.Get(context.Background(), "session")
			if err != nil || len(snapshot.Nodes) != test.wantNodes {
				t.Fatalf("%s replay: nodes=%d error=%v", test.name, len(snapshot.Nodes), err)
			}
			if test.wantNodes == 1 && !snapshot.ObservedSince.Equal(run.StartedAt) {
				t.Fatal("managed/fenced run start was lowered")
			}
		})
	}
}

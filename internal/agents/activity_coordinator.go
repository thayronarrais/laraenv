package agents

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ActivityCoordinatorOptions injects metadata-only dependencies. A registry and
// its already registered local owner are required before BeginRun can bind.
type ActivityCoordinatorOptions struct {
	OwnerID          string
	Store            *ActivityStore
	Registry         *ActivityOwnerRegistry
	Reader           ActivityMetadataReader
	Now              func() time.Time
	Emit             EventEmitter
	ObserveSession   func(SessionSignal)
	Executable       func(AgentSession) string
	LookupExecutable func(context.Context, AgentSession) (string, error)
	HooksEnabled     func() bool
}

var ErrActivityInvalidInterest = errors.New("invalid activity interest")

type coordinatorRun struct {
	run        ActivityRun
	last, next time.Time
	failures   int
	done       chan struct{}
	cancel     context.CancelFunc
}
type coordinatorInterest struct {
	interest ActivityInterest
	expires  time.Time
}
type coordinatorPending struct {
	event  *ActivityEvent
	signal *SessionSignal
	at     time.Time
}
type ActivityCoordinator struct {
	mu          sync.Mutex
	opts        ActivityCoordinatorOptions
	runs        map[string]*coordinatorRun
	reserved    map[string]ActivityRun
	interests   map[string]coordinatorInterest
	pending     []coordinatorPending
	dirty       map[string]time.Time
	nextBatch   time.Time
	executables map[string]chan struct{}
	changed     chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	closed      bool
	workers     sync.WaitGroup
}

func NewActivityCoordinator(opts ActivityCoordinatorOptions) *ActivityCoordinator {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Store == nil {
		opts.Store = NewActivityStore(opts.Now)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &ActivityCoordinator{opts: opts, runs: map[string]*coordinatorRun{}, reserved: map[string]ActivityRun{}, interests: map[string]coordinatorInterest{}, dirty: map[string]time.Time{}, nextBatch: opts.Now().Add(200 * time.Millisecond), executables: map[string]chan struct{}{}, changed: make(chan struct{}), ctx: ctx, cancel: cancel}
}
func (c *ActivityCoordinator) ReserveRun(run ActivityRun) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed && run.OwnerID == c.opts.OwnerID {
		c.reserved[run.Session.ID] = run
	}
}
func bindingForRun(run ActivityRun) ActivityBinding {
	return ActivityBinding{OwnerID: run.OwnerID, SessionID: run.Session.ID, RunID: run.RunID, TerminalID: run.Session.TerminalID, Provider: run.Session.AgentID, ProviderSessionID: run.Session.ProviderSessionID}
}
func (c *ActivityCoordinator) BeginRun(run ActivityRun) {
	c.mu.Lock()
	if c.closed || run.OwnerID != c.opts.OwnerID || c.opts.Registry == nil {
		c.mu.Unlock()
		return
	}
	if old := c.runs[run.Session.ID]; old != nil {
		if old.run.RunID == run.RunID {
			c.mu.Unlock()
			return
		}
		if old.cancel != nil {
			old.cancel()
		}
		c.opts.Registry.Release(old.run.Session.ID, old.run.RunID)
	}
	if err := c.opts.Registry.Bind(bindingForRun(run)); err != nil {
		c.mu.Unlock()
		return
	}
	// Adoption is confirmed only now, but its earliest matching pending receipt
	// is already real observation evidence. Preserve that timestamp for replay.
	reservation, reserved := c.reserved[run.Session.ID]
	// Adoption also reserves a run, but already has a real terminal. Managed
	// Launch/Resume reserve with an empty terminal, even when resuming an
	// originally adopted session. Those execution fences must never move.
	adoptedReservation := !reserved || reservation.RunID == run.RunID && reservation.OwnerID == run.OwnerID && reservation.Session.Adopted && reservation.Session.TerminalID == run.Session.TerminalID
	if run.Session.Adopted && adoptedReservation {
		for _, p := range c.pending {
			if e := p.event; e != nil && e.OwnerID == run.OwnerID && e.SessionID == "" && e.RunID == "" && e.TerminalID == run.Session.TerminalID && e.Provider == run.Session.AgentID && e.ProviderSessionID == run.Session.ProviderSessionID && c.fresh(p.at, 5*time.Second) && p.at.Before(run.StartedAt) {
				run.StartedAt = p.at
			}
		}
	}
	delete(c.reserved, run.Session.ID)
	c.runs[run.Session.ID] = &coordinatorRun{run: run}
	c.opts.Store.Begin(run)
	if c.opts.HooksEnabled != nil && !c.opts.HooksEnabled() && activityChildrenProvider(run.Session.AgentID) {
		c.opts.Store.MergeMetadata(run.Session.ID, run.RunID, ActivityMetadataResult{Status: ActivityPartial, MessageCode: ActivityMessageHooksDisabled, ObservedAt: c.opts.Now()})
	}
	c.markLocked(run.Session.ID)
	callbacks := c.replayLocked()
	c.mu.Unlock()
	c.deliver(callbacks)
}
func (c *ActivityCoordinator) BindProvider(id, runID, providerID string) {
	c.mu.Lock()
	r := c.runs[id]
	if c.closed || r == nil || r.run.RunID != runID || providerID == "" || (r.run.Session.ProviderSessionID != "" && r.run.Session.ProviderSessionID != providerID) {
		c.mu.Unlock()
		return
	}
	next := r.run
	next.Session.ProviderSessionID = providerID
	if err := c.opts.Registry.Bind(bindingForRun(next)); err != nil {
		c.mu.Unlock()
		return
	}
	r.run = next
	c.opts.Store.BindProvider(id, runID, providerID)
	c.markLocked(id)
	callbacks := c.replayLocked()
	c.mu.Unlock()
	c.deliver(callbacks)
}
func (c *ActivityCoordinator) RootChanged(session AgentSession, runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.runs[session.ID]
	if c.closed || r == nil || r.run.RunID != runID || session.TerminalID != r.run.Session.TerminalID || session.AgentID != r.run.Session.AgentID {
		return
	}
	r.run.Session = activityRootSession(session)
	c.opts.Store.RootChanged(session, runID)
	c.markLocked(session.ID)
}
func (c *ActivityCoordinator) EndRun(id, runID string, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r := c.runs[id]; r != nil && r.run.RunID == runID {
		if r.cancel != nil {
			r.cancel()
		}
		delete(c.runs, id)
		c.opts.Store.End(id, runID, at)
		c.opts.Registry.Release(id, runID)
		c.markLocked(id)
	}
	if r, ok := c.reserved[id]; ok && r.RunID == runID {
		delete(c.reserved, id)
	}
	pending := c.pending[:0]
	for _, p := range c.pending {
		if p.event != nil && p.event.SessionID == id && p.event.RunID == runID || p.signal != nil && p.signal.SessionID == id && p.signal.RunID == runID {
			continue
		}
		pending = append(pending, p)
	}
	c.pending = pending
}
func (c *ActivityCoordinator) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, r := range c.runs {
		if r.cancel != nil {
			r.cancel()
		}
		c.opts.Registry.Release(id, r.run.RunID)
	}
	c.runs = map[string]*coordinatorRun{}
	c.reserved = map[string]ActivityRun{}
	c.interests = map[string]coordinatorInterest{}
	c.pending = nil
	c.dirty = map[string]time.Time{}
	c.opts.Store.Reset()
	c.notifyLocked()
}
func (c *ActivityCoordinator) Close() error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		c.cancel()
		for id, r := range c.runs {
			if r.cancel != nil {
				r.cancel()
			}
			c.opts.Store.End(id, r.run.RunID, c.opts.Now())
			c.opts.Registry.Release(id, r.run.RunID)
		}
		c.pending = nil
		c.reserved = map[string]ActivityRun{}
		c.interests = map[string]coordinatorInterest{}
		c.notifyLocked()
	}
	c.mu.Unlock()
	c.workers.Wait()
	return nil
}
func (c *ActivityCoordinator) markLocked(id string) {
	if _, ok := c.dirty[id]; !ok {
		c.dirty[id] = c.opts.Now()
	}
}
func (c *ActivityCoordinator) notifyLocked() { close(c.changed); c.changed = make(chan struct{}) }
func (c *ActivityCoordinator) liveLocked(owner, terminal string) bool {
	if c.closed || owner != c.opts.OwnerID || terminal == "" || c.opts.Registry == nil {
		return false
	}
	ok, err := c.opts.Registry.ResolveTerminal(owner, terminal)
	return err == nil && ok
}
func (c *ActivityCoordinator) fresh(at time.Time, ttl time.Duration) bool {
	now := c.opts.Now()
	return !at.IsZero() && !at.After(now) && now.Sub(at) < ttl
}
func (c *ActivityCoordinator) activityLocked(e ActivityEvent) (resolved, pending bool) {
	if !c.liveLocked(e.OwnerID, e.TerminalID) || !activityChildrenProvider(e.Provider) || !c.fresh(e.ReceivedAt, 30*time.Second) || (c.opts.HooksEnabled != nil && !c.opts.HooksEnabled()) {
		return false, false
	}
	var r *coordinatorRun
	if e.SessionID != "" {
		r = c.runs[e.SessionID]
		if r == nil {
			reserve, ok := c.reserved[e.SessionID]
			return false, ok && reserve.RunID == e.RunID && reserve.Session.AgentID == e.Provider
		}
		if r.run.RunID != e.RunID {
			return false, false
		}
	} else {
		if e.RunID != "" {
			return false, false
		}
		for _, candidate := range c.runs {
			if candidate.run.Session.TerminalID == e.TerminalID {
				r = candidate
				break
			}
		}
		if r == nil {
			return false, true
		}
	}
	b := bindingForRun(r.run)
	if b.TerminalID != e.TerminalID || b.Provider != e.Provider {
		return false, false
	}
	if b.ProviderSessionID == "" {
		return false, true
	}
	if b.ProviderSessionID != e.ProviderSessionID {
		return false, false
	}
	active, ok, err := c.opts.Registry.ResolveActiveBinding(b.SessionID)
	if err != nil || !ok || active != b {
		return false, false
	}
	e.SessionID, e.RunID = b.SessionID, b.RunID
	if c.opts.Store.Apply(e) {
		c.aliasesLocked(b.SessionID, b.RunID)
		c.markLocked(b.SessionID)
	}
	return true, false
}
func (c *ActivityCoordinator) signalLocked(s SessionSignal) (*SessionSignal, bool) {
	if !c.liveLocked(s.OwnerID, s.TerminalID) || !activityProvider(s.Provider) || !c.fresh(s.CreatedAt, 30*time.Second) {
		return nil, false
	}
	if s.SessionID == "" && s.RunID == "" && s.Kind == SignalStarted {
		if s.ProviderSessionID != "" {
			if b, ok, err := c.opts.Registry.ResolveProvider(s.Provider, s.ProviderSessionID); err != nil || ok && (b.OwnerID != s.OwnerID || b.TerminalID != s.TerminalID || b.ProviderSessionID != s.ProviderSessionID) {
				return nil, false
			}
		}
		return &s, false
	}
	var r *coordinatorRun
	if s.SessionID != "" {
		r = c.runs[s.SessionID]
		if r == nil {
			reserve, ok := c.reserved[s.SessionID]
			return nil, ok && reserve.RunID == s.RunID && reserve.Session.AgentID == s.Provider
		}
		if s.RunID != r.run.RunID {
			return nil, false
		}
	} else {
		if s.RunID != "" {
			return nil, false
		}
		for _, candidate := range c.runs {
			if candidate.run.Session.TerminalID == s.TerminalID {
				r = candidate
				break
			}
		}
		if r == nil {
			return nil, true
		}
	}
	b := bindingForRun(r.run)
	if b.TerminalID != s.TerminalID || b.Provider != s.Provider || s.ProviderSessionID != "" && s.ProviderSessionID != b.ProviderSessionID {
		return nil, false
	}
	active, ok, err := c.opts.Registry.ResolveActiveBinding(b.SessionID)
	if err != nil || !ok || active != b {
		return nil, false
	}
	// Adopted signals retain the adopted routing shape; managed ones retain
	// their literal execution fence. Never infer a replacement run for a fence.
	return &s, false
}
func (c *ActivityCoordinator) appendPendingLocked(p coordinatorPending) {
	if !c.fresh(p.at, 5*time.Second) {
		return
	}
	c.expireLocked(c.opts.Now())
	if len(c.pending) >= 128 {
		copy(c.pending, c.pending[1:])
		c.pending = c.pending[:127]
	}
	c.pending = append(c.pending, p)
}
func (c *ActivityCoordinator) expireLocked(now time.Time) {
	for key, interest := range c.interests {
		if !now.Before(interest.expires) {
			delete(c.interests, key)
		}
	}
	out := c.pending[:0]
	for _, p := range c.pending {
		if now.Sub(p.at) < 5*time.Second {
			out = append(out, p)
		}
	}
	c.pending = out
}
func (c *ActivityCoordinator) ObserveActivity(e ActivityEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, pending := c.activityLocked(e)
	if pending {
		copy := e
		c.appendPendingLocked(coordinatorPending{event: &copy, at: e.ReceivedAt})
	}
}
func (c *ActivityCoordinator) ObserveSessionSignal(s SessionSignal) {
	c.mu.Lock()
	signal, pending := c.signalLocked(s)
	if pending {
		copy := s
		c.appendPendingLocked(coordinatorPending{signal: &copy, at: s.CreatedAt})
	}
	c.mu.Unlock()
	if signal != nil {
		c.deliver([]SessionSignal{*signal})
	}
}
func (c *ActivityCoordinator) replayLocked() []SessionSignal {
	c.expireLocked(c.opts.Now())
	old := c.pending
	c.pending = nil
	var signals []SessionSignal
	for _, p := range old {
		keep := false
		if p.event != nil {
			_, keep = c.activityLocked(*p.event)
		} else {
			var s *SessionSignal
			s, keep = c.signalLocked(*p.signal)
			if s != nil {
				signals = append(signals, *s)
			}
		}
		if keep {
			c.pending = append(c.pending, p)
		}
	}
	return signals
}
func (c *ActivityCoordinator) deliver(signals []SessionSignal) {
	for _, s := range signals {
		if c.opts.ObserveSession != nil {
			c.opts.ObserveSession(s)
		}
	}
}
func (c *ActivityCoordinator) aliasesLocked(id, run string) {
	snapshot, ok := c.opts.Store.Snapshot(id)
	if !ok {
		return
	}
	ids := []string{}
	for _, n := range snapshot.Nodes {
		if n.ID != snapshot.RootID && n.ProviderNodeID != "" {
			ids = append(ids, n.ProviderNodeID)
		}
	}
	_ = c.opts.Registry.SetProviderAliases(id, run, ids)
}
func activityInterestKey(window, purpose string) string { return window + "\x00" + purpose }
func validActivityInterest(i ActivityInterest) bool {
	return activityValidID(i.WindowID) && activityValidID(i.SessionID) && (i.Purpose == ActivityInterestCounter || i.Purpose == ActivityInterestDialog)
}
func (c *ActivityCoordinator) SetInterest(ctx context.Context, i ActivityInterest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validActivityInterest(i) {
		return ErrActivityInvalidInterest
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// A canceled IPC request may have waited behind another lease mutation.
	// Recheck at the mutation boundary, not only before acquiring this lock.
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.closed {
		return ErrActivityOwnerUnavailable
	}
	if _, ok := c.opts.Store.Snapshot(i.SessionID); !ok {
		return ErrActivityOwnerUnavailable
	}
	c.interests[activityInterestKey(i.WindowID, string(i.Purpose))] = coordinatorInterest{i, c.opts.Now().Add(30 * time.Second)}
	return nil
}
func (c *ActivityCoordinator) RemoveInterest(ctx context.Context, window, purpose string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	delete(c.interests, activityInterestKey(window, purpose))
	return nil
}
func (c *ActivityCoordinator) Get(ctx context.Context, id string) (ActivitySnapshot, error) {
	if err := ctx.Err(); err != nil {
		return ActivitySnapshot{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ActivitySnapshot{}, ErrActivityOwnerUnavailable
	}
	s, ok := c.opts.Store.Snapshot(id)
	if !ok {
		return ActivitySnapshot{}, ErrActivityOwnerUnavailable
	}
	return s, nil
}
func (c *ActivityCoordinator) Wait(ctx context.Context, id string, revision uint64) (ActivitySnapshot, error) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return ActivitySnapshot{}, ErrActivityOwnerUnavailable
		}
		s, ok := c.opts.Store.Snapshot(id)
		ch := c.changed
		c.mu.Unlock()
		if !ok {
			return ActivitySnapshot{}, ErrActivityOwnerUnavailable
		}
		if s.Revision > revision {
			return s, nil
		}
		select {
		case <-ctx.Done():
			return ActivitySnapshot{}, ctx.Err()
		case <-ch:
		}
	}
}
func (c *ActivityCoordinator) intervalLocked(id string, now time.Time) time.Duration {
	var interval time.Duration
	for key, i := range c.interests {
		if !now.Before(i.expires) {
			delete(c.interests, key)
			continue
		}
		if i.interest.SessionID == id {
			if i.interest.Purpose == ActivityInterestDialog {
				return 3 * time.Second
			}
			interval = 10 * time.Second
		}
	}
	return interval
}
func (c *ActivityCoordinator) Tick(ctx context.Context, now time.Time) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.expireLocked(now)
	hooksDisabled := c.opts.HooksEnabled != nil && !c.opts.HooksEnabled()
	for id, r := range c.runs {
		// Coverage changes apply even to event-only providers with no metadata
		// poll. The store advances only when retained evidence actually changes.
		if hooksDisabled && c.opts.Store.disableLiveObservation(id, r.run.RunID) {
			c.markLocked(id)
		}
		interval := c.intervalLocked(id, now)
		if interval > 0 && r.done == nil && !now.Before(r.next) && (r.last.IsZero() || !now.Before(r.last.Add(interval))) {
			c.startLocked(ctx, r)
		}
	}
	var snapshots []ActivitySnapshot
	if !now.Before(c.nextBatch) {
		c.nextBatch = c.nextBatch.Add((now.Sub(c.nextBatch)/(200*time.Millisecond) + 1) * (200 * time.Millisecond))
		for id := range c.dirty {
			if s, ok := c.opts.Store.Snapshot(id); ok {
				snapshots = append(snapshots, s)
			}
			delete(c.dirty, id)
		}
	}
	if len(snapshots) > 0 {
		c.notifyLocked()
	}
	c.mu.Unlock()
	if c.opts.Emit != nil {
		for _, s := range snapshots {
			c.opts.Emit("agent:activity-changed", s)
		}
	}
}
func (c *ActivityCoordinator) Run(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.drain()
			c.Tick(ctx, c.opts.Now())
		}
	}
}
func (c *ActivityCoordinator) drain() {
	if c.opts.Registry == nil {
		return
	}
	// Capture early activity before SessionStart establishes ObservedSince.
	// Otherwise the store correctly rejects those earlier receipts as old-run
	// data even though both envelopes arrived in this same drain interval.
	if path, err := c.opts.Registry.QueueDir(c.opts.OwnerID, "activity"); err == nil {
		_ = DrainActivitySignals(path, c.ObserveActivity)
	}
	if path, err := c.opts.Registry.QueueDir(c.opts.OwnerID, "session"); err == nil {
		_ = DrainSessionSignals(path, c.ObserveSessionSignal)
	}
}
func (c *ActivityCoordinator) startLocked(parent context.Context, r *coordinatorRun) chan struct{} {
	if r.done != nil {
		return r.done
	}
	if r.run.Session.AgentID != AgentCodex || r.run.Session.ProviderSessionID == "" || c.opts.Reader == nil || c.opts.Executable == nil && c.opts.LookupExecutable == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	stop := context.AfterFunc(parent, cancel)
	r.cancel = cancel
	r.done = make(chan struct{})
	done := r.done
	run := r.run
	q := ActivityMetadataQuery{RootProviderID: run.Session.ProviderSessionID, KnownParents: map[string]string{}}
	if snapshot, ok := c.opts.Store.Snapshot(run.Session.ID); ok {
		ids := map[string]string{}
		for _, n := range snapshot.Nodes {
			ids[n.ID] = n.ProviderNodeID
		}
		for _, n := range snapshot.Nodes {
			if n.ID != snapshot.RootID && ids[n.ParentID] != "" {
				q.KnownParents[n.ProviderNodeID] = ids[n.ParentID]
			}
		}
	}
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		defer cancel()
		defer stop()
		var result ActivityMetadataResult
		var err error
		if c.opts.LookupExecutable != nil {
			q.Executable, err = c.opts.LookupExecutable(ctx, run.Session)
		} else {
			q.Executable = c.opts.Executable(run.Session)
		}
		if err == nil && q.Executable == "" {
			err = errors.New("activity executable unavailable")
		}
		if err == nil {
			c.mu.Lock()
			gate := c.executables[q.Executable]
			if gate == nil {
				gate = make(chan struct{}, 1)
				c.executables[q.Executable] = gate
			}
			c.mu.Unlock()
			select {
			case gate <- struct{}{}:
				if ctx.Err() != nil {
					err = ctx.Err()
				} else {
					result, err = c.opts.Reader.Read(ctx, q)
				}
				<-gate
			case <-ctx.Done():
				err = ctx.Err()
			}
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		defer close(done)
		current := c.runs[run.Session.ID]
		if c.closed || current != r {
			return
		}
		r.done = nil
		r.cancel = nil
		active, ok, bindingErr := c.opts.Registry.ResolveActiveBinding(run.Session.ID)
		if bindingErr != nil || !ok || active != bindingForRun(run) {
			return
		}
		now := c.opts.Now()
		r.last = now
		if err != nil {
			r.failures++
			backoff := 10 * time.Second
			if r.failures == 2 {
				backoff = 30 * time.Second
			} else if r.failures >= 3 {
				backoff = 60 * time.Second
			}
			r.next = now.Add(backoff)
			result = ActivityMetadataResult{Status: ActivityPartial, MessageCode: ActivityMessageRefreshFailed, Stale: true, ObservedAt: now}
		} else {
			r.failures = 0
			r.next = time.Time{}
			result.ObservedAt = now
			// Identity discovery does not establish live-hook coverage. Preserve
			// stronger source limitations (errors, incompatibility, truncation).
			if !result.Truncated && (result.Status == ActivityAvailable || result.Status == ActivityPartial) && (result.MessageCode == ActivityMessageNone || result.MessageCode == ActivityMessageHooksUnobserved || result.MessageCode == ActivityMessageHooksDisabled) {
				if c.opts.HooksEnabled != nil && !c.opts.HooksEnabled() {
					result.Status, result.MessageCode = ActivityPartial, ActivityMessageHooksDisabled
				} else if snapshot, ok := c.opts.Store.Snapshot(run.Session.ID); ok && snapshot.Capabilities.LiveState {
					result.Status, result.MessageCode = ActivityAvailable, ActivityMessageNone
				} else {
					result.Status, result.MessageCode = ActivityPartial, ActivityMessageHooksUnobserved
				}
			}
		}
		result.liveObservationDisabled = c.opts.HooksEnabled != nil && !c.opts.HooksEnabled()
		if c.opts.Store.MergeMetadata(run.Session.ID, run.RunID, result) {
			c.aliasesLocked(run.Session.ID, run.RunID)
			c.markLocked(run.Session.ID)
		}
	}()
	return done
}
func (c *ActivityCoordinator) Refresh(ctx context.Context, id string) (ActivitySnapshot, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ActivitySnapshot{}, ErrActivityOwnerUnavailable
	}
	var done chan struct{}
	if r := c.runs[id]; r != nil {
		done = c.startLocked(ctx, r)
	}
	c.mu.Unlock()
	if done != nil {
		select {
		case <-ctx.Done():
			return ActivitySnapshot{}, ctx.Err()
		case <-done:
		}
	}
	return c.Get(ctx, id)
}

var _ ActivityBackend = (*ActivityCoordinator)(nil)
var _ ActivityRuntimeObserver = (*ActivityCoordinator)(nil)

// UnobservedActivitySnapshot exposes only known principal metadata. Empty
// source/run/revision explicitly distinguish it from a collected execution.
func UnobservedActivitySnapshot(session AgentSession, at time.Time) ActivitySnapshot {
	rootID := activityNodeID(session.AgentID, session.ID, "root", "")
	label := activityText(session.Title)
	if label == "" {
		label = string(session.AgentID)
	}
	message := ActivityMessageHooksUnobserved
	if session.ProviderSessionID == "" {
		message = ActivityMessageNoProviderSession
	}
	if !activityChildrenProvider(session.AgentID) {
		message = ActivityMessageUnsupportedProvider
	}
	root := ActivityNode{ID: rootID, ProviderNodeID: session.ProviderSessionID, Kind: ActivityAgent, Label: label, State: activityRootState(session.State), FirstObservedAt: at, LastObservedAt: at, ProjectName: activityText(session.ProjectName), ProjectPath: session.ProjectPath, WorktreePath: session.WorktreePath, TerminalID: session.TerminalID}
	return ActivitySnapshot{SessionID: session.ID, RootID: rootID, Provider: session.AgentID, ObservedSince: at, ObservedAt: at, Status: ActivityUnavailable, MessageCode: message, Nodes: []ActivityNode{root}, BackgroundTasks: []ActivityNode{}}
}

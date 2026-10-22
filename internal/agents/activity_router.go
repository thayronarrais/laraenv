package agents

import (
	"context"
	"sort"
	"sync"
	"time"
)

type activityRoute struct {
	binding ActivityBinding
	backend ActivityBackend
}
type activityRouterInterest struct {
	interest ActivityInterest
	route    activityRoute
	expires  time.Time
	cancel   context.CancelFunc
}

// ActivityRouter resolves ownership for every operation. Its clients carry only
// validated local descriptors; no endpoint or credential enters public events.
type ActivityRouter struct {
	mu         sync.Mutex
	owner      string
	local      ActivityBackend
	registry   *ActivityOwnerRegistry
	emit       EventEmitter
	clients    map[string]*ActivityClient
	interests  map[string]*activityRouterInterest
	leaseLocks map[string]*sync.Mutex
	emitted    map[string]ActivitySnapshot
	snapshots  map[string]ActivitySnapshot
	ctx        context.Context
	cancel     context.CancelFunc
	workers    sync.WaitGroup
	closed     bool
}

func NewActivityRouter(localOwnerID string, local ActivityBackend, registry *ActivityOwnerRegistry, emit EventEmitter) *ActivityRouter {
	ctx, cancel := context.WithCancel(context.Background())
	return &ActivityRouter{owner: localOwnerID, local: local, registry: registry, emit: emit, clients: map[string]*ActivityClient{}, interests: map[string]*activityRouterInterest{}, leaseLocks: map[string]*sync.Mutex{}, emitted: map[string]ActivitySnapshot{}, snapshots: map[string]ActivitySnapshot{}, ctx: ctx, cancel: cancel}
}

// Backend leases reuse window/purpose identities. Keep every mutation for a
// key ordered through its network call, not merely through the local map edit.
func (r *ActivityRouter) lockLease(key string) func() {
	r.mu.Lock()
	lock := r.leaseLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		r.leaseLocks[key] = lock
	}
	r.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func removeActivityLease(backend ActivityBackend, interest ActivityInterest) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = backend.RemoveInterest(ctx, interest.WindowID, string(interest.Purpose))
}
func (r *ActivityRouter) resolve(id string) (activityRoute, error) {
	if r.registry == nil {
		return activityRoute{}, ErrActivityOwnerUnavailable
	}
	b, ok, err := r.registry.ResolveBinding(id)
	if err != nil || !ok {
		return activityRoute{}, ErrActivityOwnerUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return activityRoute{}, ErrActivityOwnerUnavailable
	}
	if b.OwnerID == r.owner && r.local != nil {
		return activityRoute{b, r.local}, nil
	}
	descriptor, ok, err := r.registry.ResolveOwner(b.OwnerID)
	if err != nil || !ok {
		return activityRoute{}, ErrActivityOwnerUnavailable
	}
	client := r.clients[b.OwnerID]
	if client != nil && client.descriptor != descriptor {
		client.Close()
		delete(r.clients, b.OwnerID)
		client = nil
	}
	if client == nil {
		client, err = NewActivityClient(r.registry, b.OwnerID)
		if err != nil {
			return activityRoute{}, err
		}
		r.clients[b.OwnerID] = client
	}
	return activityRoute{b, client}, nil
}
func (r *ActivityRouter) validate(route activityRoute, s ActivitySnapshot) bool {
	b, ok, err := r.registry.ResolveBinding(route.binding.SessionID)
	return err == nil && ok && b == route.binding && s.SessionID == b.SessionID && s.RunID == b.RunID && s.SourceID == b.OwnerID && s.Provider == b.Provider
}
func (r *ActivityRouter) fetch(ctx context.Context, id string, op string, rev uint64) (ActivitySnapshot, error) {
	route, err := r.resolve(id)
	if err != nil {
		return r.unavailable(id, err)
	}
	var s ActivitySnapshot
	switch op {
	case "refresh":
		s, err = route.backend.Refresh(ctx, id)
	case "wait":
		s, err = route.backend.Wait(ctx, id, rev)
	default:
		s, err = route.backend.Get(ctx, id)
	}
	if err != nil {
		return r.unavailable(id, err)
	}
	if !r.validate(route, s) {
		return ActivitySnapshot{}, ErrActivityOwnerUnavailable
	}
	r.mu.Lock()
	r.rememberLocked(s)
	r.mu.Unlock()
	return s, nil
}

func (r *ActivityRouter) unavailable(id string, err error) (ActivitySnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.snapshots[id]
	if !ok || r.closed {
		return ActivitySnapshot{}, err
	}
	// A genuinely lost owner leaves no binding. A known replacement is
	// positive evidence that this cached execution must no longer be returned.
	binding, current, lookupErr := r.registry.ResolveBinding(id)
	if lookupErr != nil || current && (binding.OwnerID != s.SourceID || binding.RunID != s.RunID || binding.Provider != s.Provider) {
		return ActivitySnapshot{}, ErrActivityOwnerUnavailable
	}
	s = cloneActivitySnapshot(s)
	s.Stale, s.Status, s.MessageCode = true, ActivityPartial, ActivityMessageOwnerUnavailable
	r.rememberLocked(s)
	return s, nil
}

// Client windows follow the same ended-photo bound as the owner's store.
// Registry proof distinguishes still-active roots from ended/lost owners,
// including roots that ended after their client interest was removed.
func (r *ActivityRouter) rememberLocked(snapshot ActivitySnapshot) {
	r.snapshots[snapshot.SessionID] = cloneActivitySnapshot(snapshot)
	if len(r.snapshots) <= activityEndedRunLimit {
		return
	}
	ended := make([]ActivitySnapshot, 0)
	for id, s := range r.snapshots {
		binding, active, err := r.registry.ResolveActiveBinding(id)
		if err != nil || !active || binding.OwnerID != s.SourceID || binding.RunID != s.RunID {
			ended = append(ended, s)
		}
	}
	sort.Slice(ended, func(i, j int) bool {
		if ended[i].ObservedAt.Equal(ended[j].ObservedAt) {
			return ended[i].SessionID < ended[j].SessionID
		}
		return ended[i].ObservedAt.Before(ended[j].ObservedAt)
	})
	for len(ended) > activityEndedRunLimit {
		id := ended[0].SessionID
		delete(r.snapshots, id)
		delete(r.emitted, id)
		ended = ended[1:]
	}
}
func (r *ActivityRouter) Get(ctx context.Context, id string) (ActivitySnapshot, error) {
	return r.fetch(ctx, id, "get", 0)
}
func (r *ActivityRouter) Refresh(ctx context.Context, id string) (ActivitySnapshot, error) {
	return r.fetch(ctx, id, "refresh", 0)
}
func (r *ActivityRouter) Wait(ctx context.Context, id string, revision uint64) (ActivitySnapshot, error) {
	return r.fetch(ctx, id, "wait", revision)
}
func (r *ActivityRouter) SetInterest(ctx context.Context, i ActivityInterest) error {
	if !validActivityInterest(i) {
		return ErrActivityInvalidInterest
	}
	key := activityInterestKey(i.WindowID, string(i.Purpose))
	unlock := r.lockLease(key)
	defer unlock()
	route, err := r.resolve(i.SessionID)
	if err != nil {
		return err
	}
	if err = route.backend.SetInterest(ctx, i); err != nil {
		return err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		removeActivityLease(route.backend, i)
		return ErrActivityOwnerUnavailable
	}
	old := r.interests[key]
	if old != nil && old.interest == i && old.route.binding == route.binding && old.route.backend == route.backend {
		old.expires = time.Now().Add(30 * time.Second)
		r.mu.Unlock()
		return nil
	}
	watchCtx, cancel := context.WithCancel(r.ctx)
	item := &activityRouterInterest{i, route, time.Now().Add(30 * time.Second), cancel}
	r.interests[key] = item
	if old != nil {
		old.cancel()
	}
	r.workers.Add(1)
	r.mu.Unlock()
	if old != nil && old.route.backend != route.backend {
		_ = old.route.backend.RemoveInterest(ctx, i.WindowID, string(i.Purpose))
	}
	go r.watch(watchCtx, key, item)
	return nil
}
func (r *ActivityRouter) RemoveInterest(ctx context.Context, window, purpose string) error {
	key := activityInterestKey(window, purpose)
	unlock := r.lockLease(key)
	defer unlock()
	r.mu.Lock()
	item := r.interests[key]
	var backend ActivityBackend
	if item != nil {
		backend = item.route.backend
	}
	delete(r.interests, key)
	if item != nil {
		item.cancel()
	}
	r.mu.Unlock()
	if item != nil {
		return backend.RemoveInterest(ctx, window, purpose)
	}
	return nil
}
func (r *ActivityRouter) watch(ctx context.Context, key string, item *activityRouterInterest) {
	defer r.workers.Done()
	defer func() {
		r.mu.Lock()
		if r.interests[key] == item {
			delete(r.interests, key)
		}
		r.mu.Unlock()
	}()
	var revision uint64
	var source, run string
	for {
		r.mu.Lock()
		active := !r.closed && r.interests[key] == item && time.Now().Before(item.expires)
		route := item.route
		r.mu.Unlock()
		if !active || ctx.Err() != nil {
			return
		}
		current, err := r.resolve(item.interest.SessionID)
		if err == nil && (current.binding != route.binding || current.backend != route.backend) {
			// Cancel each outstanding wait before transferring the interest. The old
			// wait uses a bounded child context and has completed before this branch.
			current, err = r.transferLease(ctx, key, item)
			if err == nil {
				route = current
				revision = 0
			}
		}
		if err == nil {
			requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			var s ActivitySnapshot
			if revision == 0 {
				s, err = route.backend.Get(requestCtx, item.interest.SessionID)
			} else {
				s, err = route.backend.Wait(requestCtx, item.interest.SessionID, revision)
			}
			cancel()
			if err == nil && r.validate(route, s) {
				if source != s.SourceID || run != s.RunID {
					revision = 0
				}
				source, run = s.SourceID, s.RunID
				if s.Revision > revision {
					revision = s.Revision
					r.emitCurrent(ctx, key, item, route, s)
				}
			}
		}
		// Local observations already emit through the coordinator. Waiting still
		// detects ownership changes; a short retry prevents an unavailable owner
		// or unchanged/no-content response from spinning.
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (r *ActivityRouter) transferLease(ctx context.Context, key string, item *activityRouterInterest) (activityRoute, error) {
	unlock := r.lockLease(key)
	defer unlock()
	r.mu.Lock()
	active := !r.closed && r.interests[key] == item && ctx.Err() == nil
	old := item.route
	r.mu.Unlock()
	if !active {
		return activityRoute{}, ErrActivityOwnerUnavailable
	}
	current, err := r.resolve(item.interest.SessionID)
	if err != nil {
		return activityRoute{}, err
	}
	if current.binding == old.binding && current.backend == old.backend {
		return current, nil
	}
	if err := old.backend.RemoveInterest(ctx, item.interest.WindowID, string(item.interest.Purpose)); err != nil && ctx.Err() != nil {
		return activityRoute{}, err
	}
	if err := current.backend.SetInterest(ctx, item.interest); err != nil {
		return activityRoute{}, err
	}
	r.mu.Lock()
	active = !r.closed && r.interests[key] == item && ctx.Err() == nil
	if active {
		item.route = current
	}
	r.mu.Unlock()
	if !active {
		removeActivityLease(current.backend, item.interest)
		return activityRoute{}, ErrActivityOwnerUnavailable
	}
	return current, nil
}
func (r *ActivityRouter) emitCurrent(ctx context.Context, key string, item *activityRouterInterest, route activityRoute, s ActivitySnapshot) {
	if route.binding.OwnerID == r.owner || ctx.Err() != nil || !r.validate(route, s) {
		return
	}
	r.mu.Lock()
	old := r.emitted[s.SessionID]
	valid := !r.closed && r.interests[key] == item && ctx.Err() == nil && (old.SourceID != s.SourceID || old.RunID != s.RunID || old.Revision < s.Revision)
	if valid {
		r.emitted[s.SessionID] = s
		r.rememberLocked(s)
	}
	r.mu.Unlock()
	if valid && r.emit != nil && ctx.Err() == nil {
		r.emit("agent:activity-changed", s)
	}
}
func (r *ActivityRouter) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.cancel()
	items := r.interests
	r.interests = map[string]*activityRouterInterest{}
	locks := make(map[string]*sync.Mutex, len(r.leaseLocks))
	for key, lock := range r.leaseLocks {
		locks[key] = lock
	}
	clients := r.clients
	r.mu.Unlock()
	for key, lock := range locks {
		lock.Lock()
		if item := items[key]; item != nil {
			item.cancel()
			removeActivityLease(item.route.backend, item.interest)
		}
		lock.Unlock()
	}
	for _, client := range clients {
		client.Close()
	}
	r.workers.Wait()
	return nil
}

var _ ActivityBackend = (*ActivityRouter)(nil)

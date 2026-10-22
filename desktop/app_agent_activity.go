package main

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/i18n"
)

// Process lifetime ownership survives feature toggles. Only observed PTY
// membership is replayed when the HTTP collector restarts.
type appAgentActivity struct {
	mu               sync.Mutex
	lifecycle        chan struct{}
	identity         pkgagents.ActivityOwnerIdentity
	registry         *pkgagents.ActivityOwnerRegistry
	store            *pkgagents.ActivityStore
	terminals        map[string]bool
	coordinator      *pkgagents.ActivityCoordinator
	router           *pkgagents.ActivityRouter
	server           *pkgagents.ActivityOwnerServer
	cancel           context.CancelFunc
	done             chan struct{}
	reader           pkgagents.ActivityMetadataReader
	executable       func(pkgagents.AgentSession) string
	lookupExecutable func(context.Context, pkgagents.AgentSession) (string, error)
	emit             pkgagents.EventEmitter
}

func (a *App) initAgentActivity(root string) {
	identity, _ := pkgagents.NewActivityOwnerIdentity()
	state := &appAgentActivity{identity: identity, registry: pkgagents.NewActivityOwnerRegistry(root, nil), store: pkgagents.NewActivityStore(nil), terminals: map[string]bool{}, lifecycle: make(chan struct{}, 1), reader: pkgagents.NewCodexActivityReader(nil)}
	state.lookupExecutable = func(ctx context.Context, session pkgagents.AgentSession) (string, error) {
		// Read only completed detection results; a concurrent catalog refresh
		// must not consume the activity request's five-second deadline.
		if a.agents != nil {
			path, err := a.agents.CachedActivityExecutable(ctx, session.AgentID)
			if path != "" || err != nil {
				return path, err
			}
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return exec.LookPath("codex")
	}
	state.emit = func(event string, data ...any) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, event, data...)
		}
	}
	a.agentActivity = state
	if a.terminals != nil {
		a.terminals.SetOwnerID(identity.ID)
		a.terminals.SetActivityTerminalObserver(a.activityTerminalChanged)
	}
}
func (a *App) foreignActivityRoot(session pkgagents.AgentSession) bool {
	state := a.agentActivity
	if state == nil {
		return false
	}
	binding, ok, err := state.registry.ResolveActiveBinding(session.ID)
	return err == nil && ok && binding.OwnerID != state.identity.ID && binding.TerminalID == session.TerminalID && binding.Provider == session.AgentID
}
func (a *App) activityTerminalChanged(id string, live bool) {
	state := a.agentActivity
	if state == nil {
		return
	}
	state.mu.Lock()
	if live {
		state.terminals[id] = true
	} else {
		delete(state.terminals, id)
	}
	if state.server != nil {
		_ = state.registry.SetTerminal(state.identity.ID, id, live)
	}
	state.mu.Unlock()
}
func (a *App) startAgentActivity(ctx context.Context) {
	state := a.agentActivity
	if state == nil || a.commandBar || a.cfg == nil || !a.GetAgentSessionsEnabled() || a.agents == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.server != nil || state.identity.ID == "" {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	lookup := state.lookupExecutable
	if state.executable != nil {
		lookup = nil
	}
	observeSession := func(signal pkgagents.SessionSignal) { a.observeAgentActivitySession(runCtx, signal) }
	coordinator := pkgagents.NewActivityCoordinator(pkgagents.ActivityCoordinatorOptions{OwnerID: state.identity.ID, Store: state.store, Registry: state.registry, Reader: state.reader, Emit: state.emit, ObserveSession: observeSession, Executable: state.executable, LookupExecutable: lookup, HooksEnabled: a.agentHooksWanted})
	server, err := pkgagents.StartActivityOwnerWithIdentity(runCtx, state.registry, coordinator, state.identity)
	if err != nil {
		cancel()
		coordinator.Close()
		return
	}
	for terminal := range state.terminals {
		_ = state.registry.SetTerminal(state.identity.ID, terminal, true)
	}
	state.server, state.coordinator = server, coordinator
	state.router = pkgagents.NewActivityRouter(state.identity.ID, coordinator, state.registry, state.emit)
	state.cancel = cancel
	state.done = make(chan struct{})
	a.agents.SetActivityHooksEnabled(a.agentHooksWanted())
	a.agents.SetActivityObserver(coordinator, state.identity.ID)
	go func() { defer close(state.done); coordinator.Run(runCtx) }()
}

// Root adoption can block in project resolution before the manager has a live
// run. Serialize that whole callback against shared metadata mutations. Queued
// callbacks from a closing service exit on cancellation instead of crossing a
// successful reset or blocking the collector's shutdown while reset owns the gate.
func (a *App) observeAgentActivitySession(ctx context.Context, signal pkgagents.SessionSignal) {
	// Only an unfenced start can create a root after project resolution.
	// Existing-root updates revalidate manager state, and can be replayed
	// synchronously by BeginRun while the creating callback owns this gate.
	if signal.Kind != pkgagents.SignalStarted || signal.SessionID != "" || signal.RunID != "" {
		if ctx.Err() == nil {
			a.agents.ObserveSignal(signal)
		}
		return
	}
	state := a.agentActivity
	select {
	case state.lifecycle <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-state.lifecycle }()
	if ctx.Err() == nil {
		a.agents.ObserveSignal(signal)
	}
}

func (a *App) lockAgentActivityLifecycle() func() {
	if a.agentActivity == nil {
		return func() {}
	}
	state := a.agentActivity
	state.lifecycle <- struct{}{}
	return func() { <-state.lifecycle }
}
func (a *App) stopAgentActivity() {
	state := a.agentActivity
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.server == nil {
		return
	}
	a.agents.SetActivityObserver(nil, "")
	state.cancel()
	if state.router != nil {
		_ = state.router.Close()
	}
	_ = state.coordinator.Close()
	<-state.done
	_ = state.server.Close()
	state.router = nil
	state.coordinator = nil
	state.server = nil
	state.cancel = nil
	state.done = nil
}
func (a *App) activityMutationAllowed() error {
	if a.agentActivity == nil {
		return nil
	}
	live, err := a.agentActivity.registry.HasActiveBindings()
	if err != nil {
		return err
	}
	if live {
		return i18n.Errorf("errors.app.stopLiveAgentSessionsFirst", nil)
	}
	return nil
}
func (a *App) activityBackend(sessionID string) (*pkgagents.ActivityRouter, error) {
	if err := a.requireAgentSessionsEnabled(); err != nil {
		return nil, err
	}
	if a.agents == nil {
		return nil, pkgagents.ErrActivityOwnerUnavailable
	}
	if _, ok := a.agents.GetSession(sessionID); !ok {
		return nil, pkgagents.ErrActivityOwnerUnavailable
	}
	state := a.agentActivity
	if state == nil {
		return nil, pkgagents.ErrActivityOwnerUnavailable
	}
	state.mu.Lock()
	router := state.router
	state.mu.Unlock()
	if router == nil {
		return nil, pkgagents.ErrActivityOwnerUnavailable
	}
	return router, nil
}
func (a *App) activityContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}
func (a *App) GetAgentActivity(sessionID string) (pkgagents.ActivitySnapshot, error) {
	backend, err := a.activityBackend(sessionID)
	if err != nil {
		if a.agentActivity != nil && a.agents != nil && a.cfg != nil && a.GetAgentSessionsEnabled() {
			return a.unobservedAgentActivity(sessionID)
		}
		return pkgagents.ActivitySnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(a.activityContext(), 6*time.Second)
	defer cancel()
	snapshot, err := backend.Get(ctx, sessionID)
	if err != nil {
		return a.unobservedAgentActivity(sessionID)
	}
	return snapshot, nil
}
func (a *App) SetAgentActivityInterest(interest pkgagents.ActivityInterest) error {
	backend, err := a.activityBackend(interest.SessionID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.activityContext(), 6*time.Second)
	defer cancel()
	err = backend.SetInterest(ctx, interest)
	if errors.Is(err, pkgagents.ErrActivityOwnerUnavailable) {
		if _, known, lookupErr := a.agentActivity.registry.ResolveBinding(interest.SessionID); lookupErr == nil && !known {
			return nil
		}
	}
	return err
}
func (a *App) RemoveAgentActivityInterest(windowID, purpose string) error {
	if err := a.requireAgentSessionsEnabled(); err != nil {
		return err
	}
	state := a.agentActivity
	if state == nil {
		return nil
	}
	state.mu.Lock()
	backend := state.router
	state.mu.Unlock()
	if backend == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(a.activityContext(), 6*time.Second)
	defer cancel()
	return backend.RemoveInterest(ctx, windowID, purpose)
}
func (a *App) RefreshAgentActivity(sessionID string) (pkgagents.ActivitySnapshot, error) {
	backend, err := a.activityBackend(sessionID)
	if err != nil {
		if a.agentActivity != nil && a.agents != nil && a.cfg != nil && a.GetAgentSessionsEnabled() {
			return a.unobservedAgentActivity(sessionID)
		}
		return pkgagents.ActivitySnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(a.activityContext(), 6*time.Second)
	defer cancel()
	snapshot, err := backend.Refresh(ctx, sessionID)
	if err != nil {
		return a.unobservedAgentActivity(sessionID)
	}
	return snapshot, nil
}

func (a *App) unobservedAgentActivity(sessionID string) (pkgagents.ActivitySnapshot, error) {
	session, ok := a.agents.GetSession(sessionID)
	if !ok {
		return pkgagents.ActivitySnapshot{}, pkgagents.ErrActivityOwnerUnavailable
	}
	if snapshot, observed := a.agentActivity.store.Snapshot(sessionID); observed {
		binding, current, err := a.agentActivity.registry.ResolveBinding(sessionID)
		if err != nil || current && (binding.OwnerID != snapshot.SourceID || binding.RunID != snapshot.RunID || binding.Provider != snapshot.Provider) {
			return pkgagents.ActivitySnapshot{}, pkgagents.ErrActivityOwnerUnavailable
		}
		snapshot.Status, snapshot.Stale, snapshot.MessageCode = pkgagents.ActivityPartial, true, pkgagents.ActivityMessageOwnerUnavailable
		return snapshot, nil
	}
	snapshot := pkgagents.UnobservedActivitySnapshot(session, time.Now().UTC())
	if _, known, err := a.agentActivity.registry.ResolveBinding(sessionID); err != nil || known {
		snapshot.MessageCode = pkgagents.ActivityMessageOwnerUnavailable
	}
	return snapshot, nil
}

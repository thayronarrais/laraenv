package agents

import (
	"time"

	"github.com/google/uuid"
)

// ActivityRuntimeObserver receives only structured root metadata. ReserveRun is
// memory-only: it precedes PTY creation and does not prove terminal liveness.
// BeginRun confirms the persisted root and its actual terminal binding.
// Implementations must validate run IDs when asynchronous callbacks arrive.
type ActivityRuntimeObserver interface {
	ReserveRun(ActivityRun)
	BeginRun(ActivityRun)
	BindProvider(sessionID, runID, providerID string)
	RootChanged(session AgentSession, runID string)
	EndRun(sessionID, runID string, at time.Time)
	Reset()
}

// RuntimeReconciliationGuard returns true only when an external live owner has
// been proved for a persisted session. Such roots are left for that owner to
// reconcile. The default constructor repairs all orphaned persisted runtimes.
type RuntimeReconciliationGuard func(AgentSession) bool

// SetActivityObserver is configured before launches/adoption begin. Callbacks
// never execute with Manager.mu held and cannot inspect terminal output.
func (m *Manager) SetActivityObserver(observer ActivityRuntimeObserver, ownerID string) {
	m.mu.Lock()
	m.activityObserver, m.activityOwnerID = observer, ownerID
	m.mu.Unlock()
}

// SetActivityHooksEnabled controls transient activity hooks in managed launches.
// Root lifecycle hooks keep their existing independent behavior.
func (m *Manager) SetActivityHooksEnabled(enabled bool) {
	m.mu.Lock()
	m.activityHooksEnabled = enabled
	m.mu.Unlock()
}

func (m *Manager) configureLifecycleInvocation(invocation Invocation, provider AgentID, run ActivityRun) (Invocation, error) {
	m.mu.Lock()
	observing, hooksEnabled := m.activityObserver != nil, m.activityHooksEnabled
	m.mu.Unlock()
	var identity []ActivityLifecycleIdentity
	if observing {
		identity = append(identity, ActivityLifecycleIdentity{RunID: run.RunID, OwnerID: run.OwnerID})
	}
	configured, err := addLifecycleSignals(invocation, provider, run.Session.ID, m.eventExe, identity...)
	if err != nil || !observing || !hooksEnabled {
		return configured, err
	}
	return addActivitySignals(configured, provider, m.eventExe)
}

func (m *Manager) activityTerminalEnvironment(sessionID, runID string) map[string]string {
	env := agentTerminalEnvironment(sessionID, runID)
	m.mu.Lock()
	enabled := m.activityObserver != nil && m.activityHooksEnabled
	m.mu.Unlock()
	env["LARAENV_ACTIVITY_HOOKS"] = "0"
	if enabled {
		env["LARAENV_ACTIVITY_HOOKS"] = "1"
	}
	return env
}

func (m *Manager) newActivityRunLocked(session AgentSession) ActivityRun {
	delete(m.adoptedRuns, session.ID)
	run := ActivityRun{Session: activityRootSession(session), RunID: uuid.NewString(), OwnerID: m.activityOwnerID, StartedAt: m.now().UTC()}
	m.activityRuns[session.ID] = run
	return run
}

func (m *Manager) reserveActivityRun(session AgentSession) ActivityRun {
	// A prior stopped session's terminal is not evidence for the new run.
	session.TerminalID = ""
	m.mu.Lock()
	run := m.newActivityRunLocked(session)
	observer := m.activityObserver
	m.mu.Unlock()
	if observer != nil {
		observer.ReserveRun(run)
	}
	return run
}

func (m *Manager) beginActivityRun(session AgentSession, runID string) {
	m.mu.Lock()
	run, ok := m.activityRuns[session.ID]
	if !ok || run.RunID != runID {
		m.mu.Unlock()
		return
	}
	run.Session = activityRootSession(session)
	m.activityRuns[session.ID] = run
	m.runtime[session.ID] = session.TerminalID
	observer := m.activityObserver
	m.mu.Unlock()
	if observer != nil {
		observer.BeginRun(run)
	}
}

func (m *Manager) cancelActivityRun(sessionID, runID string) {
	m.mu.Lock()
	run, ok := m.activityRuns[sessionID]
	if !ok || run.RunID != runID {
		m.mu.Unlock()
		return
	}
	delete(m.activityRuns, sessionID)
	delete(m.adoptedRuns, sessionID)
	observer := m.activityObserver
	at := m.now().UTC()
	m.mu.Unlock()
	if observer != nil {
		observer.EndRun(sessionID, runID, at)
	}
}

func (m *Manager) activityRootChanged(session AgentSession, runID string) {
	m.mu.Lock()
	run, ok := m.activityRuns[session.ID]
	if !ok || run.RunID != runID || m.runtime[session.ID] != session.TerminalID {
		m.mu.Unlock()
		return
	}
	session = activityRootSession(session)
	run.Session = session
	m.activityRuns[session.ID] = run
	observer := m.activityObserver
	m.mu.Unlock()
	if observer != nil {
		observer.RootChanged(session, runID)
	}
}

// The runtime observer consumes root metadata only, never the terminal preview
// retained by the existing sessions UI. Clone slices before exposing the DTO.
func activityRootSession(session AgentSession) AgentSession {
	session = cloneSession(session)
	session.LastPreview = ""
	return session
}

func (m *Manager) notifyActivityRoot(session AgentSession) {
	m.mu.Lock()
	run := m.activityRuns[session.ID]
	m.mu.Unlock()
	m.activityRootChanged(session, run.RunID)
}

func (m *Manager) notifyActivityEnd(run ActivityRun, at time.Time) {
	if run.RunID == "" {
		return
	}
	m.mu.Lock()
	observer := m.activityObserver
	m.mu.Unlock()
	if observer != nil {
		observer.EndRun(run.Session.ID, run.RunID, at)
	}
}

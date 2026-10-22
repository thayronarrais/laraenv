package agents

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"lara4ever/internal/i18n"
	"lara4ever/internal/pty"
)

const (
	defaultAgentCols = 120
	defaultAgentRows = 40
)

// AgentSessionEnv marks terminals that Manager.Launch/Resume own. The global
// provider hooks installed for adopted sessions exit early when it is set,
// because those sessions already receive per-launch lifecycle signals.
const AgentSessionEnv = "LARAENV_AGENT_SESSION"

type terminalRegistry interface {
	NewCommand(executable string, args []string, cols, rows int, extraPathDirs []string, cwd string, envOverrides map[string]string) (string, pty.Session, error)
	Get(id string) (pty.Session, bool)
	Close(id string) error
}

type WorkspaceResolver func(AgentLaunchOptions) (projectPath, branch, worktreePath string, err error)
type PathPrefixResolver func(projectName string) []string
type RuntimePathResolver func(projectName, projectPath string) ([]string, error)
type EventEmitter func(event string, data ...any)

// PathResolver maps a working directory to the LaraEnv project (or managed
// worktree) that contains it. It returns false for anything outside the
// projects LaraEnv knows, which is what keeps foreign agents out of the list.
type PathResolver func(cwd string) (ProjectLocation, bool)

type Manager struct {
	catalog        *Catalog
	store          *SessionStore
	terminals      terminalRegistry
	workspace      WorkspaceResolver
	pathPrefixes   PathPrefixResolver
	runtimePathFor RuntimePathResolver
	emit           EventEmitter
	discover       SessionIDDiscoverer
	eventExe       string
	pathResolver   PathResolver

	mu                   sync.Mutex
	runtime              map[string]string // LaraEnv session ID -> terminal ID
	activityRuns         map[string]ActivityRun
	adoptedRuns          map[string]string // Current adopted execution, independent of persisted history.
	activityObserver     ActivityRuntimeObserver
	activityOwnerID      string
	activityHooksEnabled bool
	focusedID            string
	now                  func() time.Time
	captureDelay         time.Duration
	captureAttempts      int
}

func (m *Manager) SetEventExecutable(path string) { m.eventExe = strings.TrimSpace(path) }

func (m *Manager) SetPathResolver(resolver PathResolver) {
	m.mu.Lock()
	m.pathResolver = resolver
	m.mu.Unlock()
}

// SetRuntimePathResolver selects runtimes for the actual checkout at launch.
// Existing sessions keep their environment until explicitly resumed/restarted.
func (m *Manager) SetRuntimePathResolver(resolver RuntimePathResolver) {
	m.mu.Lock()
	m.runtimePathFor = resolver
	m.mu.Unlock()
}

func (m *Manager) resolveRuntimePaths(projectName, projectPath string) ([]string, error) {
	m.mu.Lock()
	resolver := m.runtimePathFor
	m.mu.Unlock()
	if resolver != nil {
		return resolver(projectName, projectPath)
	}
	if m.pathPrefixes != nil {
		return m.pathPrefixes(projectName), nil
	}
	return nil, nil
}

func NewManager(catalog *Catalog, store *SessionStore, terminals terminalRegistry, workspace WorkspaceResolver, pathPrefixes PathPrefixResolver, emit EventEmitter, guards ...RuntimeReconciliationGuard) *Manager {
	manager := &Manager{
		catalog: catalog, store: store, terminals: terminals, workspace: workspace,
		pathPrefixes: pathPrefixes, emit: emit, discover: DiscoverSessionIDs,
		runtime: make(map[string]string), now: time.Now, captureDelay: 750 * time.Millisecond, captureAttempts: 20,
		activityRuns: make(map[string]ActivityRun), adoptedRuns: make(map[string]string), activityHooksEnabled: true,
	}
	manager.reconcilePersistedRuntime(guards...)
	return manager
}

func (m *Manager) reconcilePersistedRuntime(guards ...RuntimeReconciliationGuard) {
	for _, session := range m.store.List("", true) {
		preserve := false
		for _, guard := range guards {
			if guard != nil && guard(session) {
				preserve = true
				break
			}
		}
		if preserve {
			continue
		}
		if session.State == SessionError && session.ExitError != "" && isClosedReadError(session.ExitError) {
			session.State = SessionStopped
			session.ExitError = ""
			session.TerminalID = ""
			session.UpdatedAt = m.now().UTC()
			_ = m.store.Upsert(session)
			continue
		}
		if session.TerminalID == "" && !isLiveState(session.State) {
			continue
		}
		session.TerminalID = ""
		if isLiveState(session.State) {
			session.State = SessionStopped
		}
		session.UpdatedAt = m.now().UTC()
		_ = m.store.Upsert(session)
	}
}

func (m *Manager) ListProfiles(ctx context.Context) []AgentProfile {
	return m.catalog.Profiles(ctx)
}

func (m *Manager) RefreshProfiles(ctx context.Context) []AgentProfile {
	profiles := m.catalog.Refresh(ctx)
	m.emitEvent("agent:profiles-changed", profiles)
	return profiles
}

func (m *Manager) ListSessions(projectName string, includeArchived bool) []AgentSession {
	return m.store.List(projectName, includeArchived)
}

func (m *Manager) GetSession(id string) (AgentSession, bool) { return m.store.Get(id) }

func (m *Manager) SessionsForWorktree(worktreePath string) []AgentSession {
	key := NormalizeProjectPath(worktreePath)
	var result []AgentSession
	for _, session := range m.store.List("", true) {
		if session.WorktreePath != "" && NormalizeProjectPath(session.WorktreePath) == key {
			result = append(result, session)
		}
	}
	return result
}

func (m *Manager) ArchiveWorktreeSessions(worktreePath string) error {
	sessions := m.SessionsForWorktree(worktreePath)
	for _, session := range sessions {
		if m.runtimeTerminal(session.ID) != "" {
			return i18n.Errorf("errors.agents.sessionStillRunning", map[string]any{"title": session.Title})
		}
	}
	for _, session := range sessions {
		if session.Archived {
			continue
		}
		archived, err := m.store.Archive(session.ID)
		if err != nil {
			return err
		}
		m.emitEvent("agent:session-changed", archived)
	}
	return nil
}

func (m *Manager) Launch(ctx context.Context, options AgentLaunchOptions) (AgentSession, error) {
	if options.Mode == "" {
		options.Mode = LaunchCurrent
	}
	profile, err := m.profile(ctx, options.AgentID)
	if err != nil {
		return AgentSession{}, err
	}
	options.AdditionalDirectories, err = validateAdditionalDirectories(profile, options.AdditionalDirectories)
	if err != nil {
		return AgentSession{}, err
	}
	projectPath, branch, worktreePath, err := m.workspace(options)
	if err != nil {
		return AgentSession{}, i18n.Wrapf(err, "errors.agents.resolveWorkspaceFailed", map[string]any{"project": options.ProjectName})
	}
	additionalDirectories := excludePrimaryDirectory(projectPath, options.AdditionalDirectories)
	if options.Mode == LaunchCurrent {
		m.warnSharedCheckout(options.ProjectName, projectPath)
	}

	id := uuid.NewString()
	providerSessionID := ""
	if options.AgentID == AgentClaude {
		// Claude supports caller-assigned UUIDs, which makes cold resume reliable
		// without scanning its private session files.
		providerSessionID = uuid.NewString()
	}
	var knownProviderSessions map[string]struct{}
	invocation, err := BuildLaunchInvocation(profile, projectPath, providerSessionID, additionalDirectories...)
	if err != nil {
		return AgentSession{}, err
	}
	pathDirs, err := m.resolveRuntimePaths(options.ProjectName, invocation.Cwd)
	if err != nil {
		return AgentSession{}, err
	}
	if providerSessionID == "" && m.discover != nil {
		discoveryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		knownProviderSessions, _ = m.discover(discoveryCtx, profile, projectPath)
		cancel()
	}
	now := m.now().UTC()
	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = profile.Label
	}
	session := AgentSession{
		ID: id, ProjectName: options.ProjectName, ProjectPath: projectPath,
		AdditionalDirectories: additionalDirectories,
		AgentID:               options.AgentID, LaunchMode: options.Mode, GitBranch: branch,
		BaseRef: options.BaseRef, WorktreePath: worktreePath,
		ProviderSessionID: providerSessionID, Title: title, State: SessionStarting,
		StartedAt: now, UpdatedAt: now, LastActivityAt: now,
	}
	run := m.reserveActivityRun(session)
	invocation, err = m.configureLifecycleInvocation(invocation, profile.ID, run)
	if err != nil {
		m.cancelActivityRun(session.ID, run.RunID)
		return AgentSession{}, err
	}

	terminalID, terminalSession, err := m.terminals.NewCommand(
		invocation.Executable, invocation.Args, defaultAgentCols, defaultAgentRows,
		pathDirs, invocation.Cwd, m.activityTerminalEnvironment(session.ID, run.RunID),
	)
	if err != nil {
		m.cancelActivityRun(session.ID, run.RunID)
		return AgentSession{}, i18n.Wrapf(err, "errors.agents.launchFailed", map[string]any{"agent": profile.Label, "project": options.ProjectName})
	}
	session.TerminalID = terminalID
	if err := m.store.Upsert(session); err != nil {
		_ = m.terminals.Close(terminalID)
		m.cancelActivityRun(session.ID, run.RunID)
		return AgentSession{}, err
	}
	m.beginActivityRun(session, run.RunID)
	m.emitEvent("agent:session-changed", session)
	go m.pump(session.ID, terminalID, terminalSession)
	if knownProviderSessions != nil {
		go m.captureProviderSessionID(ctx, session.ID, run.RunID, terminalID, profile, projectPath, knownProviderSessions)
	}
	return session, nil
}

func (m *Manager) captureProviderSessionID(parent context.Context, sessionID, runID, terminalID string, profile AgentProfile, cwd string, before map[string]struct{}) {
	// Capture the discoverer as well: replacing configuration while this older
	// asynchronous read finishes must not redirect it to another runtime.
	discover := m.discover
	for attempt := 0; attempt < m.captureAttempts; attempt++ {
		select {
		case <-parent.Done():
			return
		case <-time.After(m.captureDelay):
		}
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		after, err := discover(ctx, profile, cwd)
		cancel()
		if err != nil {
			continue
		}
		var discovered string
		for id := range after {
			if _, existed := before[id]; existed {
				continue
			}
			if discovered != "" {
				return // ambiguous: never risk resuming the wrong conversation
			}
			discovered = id
		}
		if discovered == "" {
			continue
		}
		m.mu.Lock()
		run, live := m.activityRuns[sessionID]
		session, ok := m.store.Get(sessionID)
		if !live || run.RunID != runID || m.runtime[sessionID] != terminalID || !ok || session.TerminalID != terminalID || session.ProviderSessionID != "" || session.AgentID != profile.ID || NormalizeProjectPath(session.ProjectPath) != NormalizeProjectPath(cwd) {
			m.mu.Unlock()
			return
		}
		session.ProviderSessionID = discovered
		err = m.store.Upsert(session)
		observer := m.activityObserver
		m.mu.Unlock()
		if err == nil {
			if observer != nil {
				observer.BindProvider(sessionID, runID, discovered)
			}
			m.emitEvent("agent:session-changed", session)
		}
		return
	}
}

func (m *Manager) Resume(ctx context.Context, id string) (AgentSession, error) {
	session, ok := m.store.Get(id)
	if !ok {
		return AgentSession{}, i18n.Errorf("errors.agents.sessionNotFound", map[string]any{"id": id})
	}
	if terminalID := m.runtimeTerminal(id); terminalID != "" {
		return AgentSession{}, i18n.Errorf("errors.agents.sessionAlreadyRunning", map[string]any{"id": id, "terminal": terminalID})
	}
	profile, err := m.profile(ctx, session.AgentID)
	if err != nil {
		return AgentSession{}, err
	}
	additionalDirectories, err := validateAdditionalDirectories(profile, session.AdditionalDirectories)
	if err != nil {
		return AgentSession{}, i18n.Wrapf(err, "errors.agents.resumeSessionFailed", map[string]any{"agent": profile.Label})
	}
	if err := m.verifyProviderSession(ctx, profile, session.ProjectPath, session.ProviderSessionID); err != nil {
		now := m.now().UTC()
		session.TerminalID = ""
		session.State = SessionError
		session.ExitError = err.Error()
		session.UpdatedAt = now
		session.Unread = true
		if storeErr := m.store.Upsert(session); storeErr != nil {
			return AgentSession{}, i18n.Wrapf(err, "errors.agents.resumeUpdateMetadataFailed", map[string]any{"storeError": storeErr})
		}
		m.emitEvent("agent:session-changed", session)
		return AgentSession{}, err
	}
	options := AgentLaunchOptions{
		ProjectName: session.ProjectName, AgentID: session.AgentID, Mode: session.LaunchMode,
		Branch: session.GitBranch, BaseRef: session.BaseRef, WorktreeName: filepathBase(session.WorktreePath),
		AdditionalDirectories: additionalDirectories,
	}
	projectPath, branch, worktreePath, err := m.workspace(options)
	if err != nil {
		return AgentSession{}, i18n.Wrapf(err, "errors.agents.resolveWorkspaceFailed", map[string]any{"project": session.ProjectName})
	}
	additionalDirectories = excludePrimaryDirectory(projectPath, additionalDirectories)
	invocation, err := BuildResumeInvocation(profile, projectPath, session.ProviderSessionID, additionalDirectories...)
	if err != nil {
		return AgentSession{}, i18n.Wrapf(err, "errors.agents.resumeSessionFailed", map[string]any{"agent": profile.Label})
	}
	pathDirs, err := m.resolveRuntimePaths(session.ProjectName, invocation.Cwd)
	if err != nil {
		return AgentSession{}, err
	}
	run := m.reserveActivityRun(session)
	invocation, err = m.configureLifecycleInvocation(invocation, profile.ID, run)
	if err != nil {
		m.cancelActivityRun(session.ID, run.RunID)
		return AgentSession{}, i18n.Wrapf(err, "errors.agents.configureLifecycleSignalsFailed", map[string]any{"agent": profile.Label})
	}
	terminalID, terminalSession, err := m.terminals.NewCommand(
		invocation.Executable, invocation.Args, defaultAgentCols, defaultAgentRows,
		pathDirs, invocation.Cwd, m.activityTerminalEnvironment(session.ID, run.RunID),
	)
	if err != nil {
		m.cancelActivityRun(session.ID, run.RunID)
		return AgentSession{}, i18n.Wrapf(err, "errors.agents.resumeForProjectFailed", map[string]any{"agent": profile.Label, "project": session.ProjectName})
	}
	now := m.now().UTC()
	session.ProjectPath = projectPath
	session.AdditionalDirectories = additionalDirectories
	session.GitBranch = branch
	session.WorktreePath = worktreePath
	session.TerminalID = terminalID
	session.State = SessionStarting
	session.UpdatedAt = now
	session.LastActivityAt = now
	session.ExitError = ""
	session.Archived = false
	if err := m.store.Upsert(session); err != nil {
		_ = m.terminals.Close(terminalID)
		m.cancelActivityRun(session.ID, run.RunID)
		return AgentSession{}, err
	}
	m.beginActivityRun(session, run.RunID)
	m.emitEvent("agent:session-changed", session)
	go m.pump(id, terminalID, terminalSession)
	return session, nil
}

// verifyProviderSession's two error keys — errors.agents.providerSessionMissing
// and errors.agents.providerSessionNotFound — must keep the literal English
// prefix "provider conversation is unavailable" in every locale; the rest of
// each message translates normally. frontend/src/components/terminal/
// agentRestore.ts matches on that exact substring (case-insensitively) to
// detect an unresumable agent session and offer the restore-from-scratch
// fallback, regardless of the active UI language. Both keys are listed in
// langs/.do-not-translate.json, which tools/langs/check.mjs enforces —
// dropping or rewording that prefix silently breaks agent restore.
func (m *Manager) verifyProviderSession(ctx context.Context, profile AgentProfile, cwd, providerSessionID string) error {
	providerSessionID = strings.TrimSpace(providerSessionID)
	if m.discover == nil {
		return nil
	}
	if providerSessionID == "" {
		return i18n.Errorf("errors.agents.providerSessionMissing", map[string]any{"agent": profile.Label, "cwd": cwd})
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	known, err := m.discover(discoveryCtx, profile, cwd)
	cancel()
	if err != nil {
		return nil // Metadata discovery is best-effort; do not block a valid resume.
	}
	if _, exists := known[providerSessionID]; exists {
		return nil
	}
	return i18n.Errorf("errors.agents.providerSessionNotFound", map[string]any{"agent": profile.Label, "session": providerSessionID, "cwd": cwd})
}

func (m *Manager) Stop(id string) error {
	terminalID := m.runtimeTerminal(id)
	if terminalID == "" {
		return nil
	}
	if err := m.terminals.Close(terminalID); err != nil {
		return err
	}
	m.finish(id, terminalID, nil)
	return nil
}

func (m *Manager) Rename(id, title string) (AgentSession, error) {
	session, err := m.store.Rename(id, title)
	if err == nil {
		m.emitEvent("agent:session-changed", session)
	}
	return session, err
}

func (m *Manager) Archive(id string) (AgentSession, error) {
	if terminalID := m.runtimeTerminal(id); terminalID != "" {
		return AgentSession{}, i18n.Errorf("errors.agents.stopBeforeArchiving", nil)
	}
	session, err := m.store.Archive(id)
	if err == nil {
		m.emitEvent("agent:session-changed", session)
	}
	return session, err
}

func (m *Manager) MarkRead(id string) (AgentSession, error) {
	m.SetFocused(id)
	session, err := m.store.MarkRead(id)
	if err == nil {
		m.emitEvent("agent:session-changed", session)
	}
	return session, err
}

// SetFocused updates the currently visible agent terminal. An empty ID clears
// focus when the terminal page or application window is no longer visible.
func (m *Manager) SetFocused(id string) {
	m.mu.Lock()
	m.focusedID = id
	m.mu.Unlock()
}

func (m *Manager) Flush() error { return m.store.Flush() }

func (m *Manager) HasLiveSessions() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runtime) > 0 || len(m.activityRuns) > 0
}

func (m *Manager) ResetMetadata() (string, error) {
	if m.HasLiveSessions() {
		return "", i18n.Errorf("errors.agents.stopBeforeResettingMetadata", nil)
	}
	for _, session := range m.store.List("", true) {
		if strings.TrimSpace(session.WorktreePath) == "" {
			continue
		}
		if _, err := os.Stat(session.WorktreePath); err == nil {
			return "", i18n.Errorf("errors.agents.removeWorktreeBeforeReset", map[string]any{"title": session.Title})
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", i18n.Wrapf(err, "errors.agents.inspectWorktreeFailed", map[string]any{"title": session.Title})
		}
	}
	archived, err := m.store.Reset()
	if err == nil {
		m.SetFocused("")
		m.mu.Lock()
		observer := m.activityObserver
		m.mu.Unlock()
		if observer != nil {
			observer.Reset()
		}
		m.emitEvent("agent:sessions-reset", archived)
	}
	return archived, err
}

func (m *Manager) ObserveSignal(signal SessionSignal) {
	if signal.CreatedAt.IsZero() || time.Since(signal.CreatedAt) > 10*time.Minute || signal.CreatedAt.After(time.Now().Add(2*time.Minute)) {
		return
	}
	if signal.SessionID == "" && signal.TerminalID != "" {
		m.observeAdoptedSignal(signal)
		return
	}
	m.mu.Lock()
	if m.runtime[signal.SessionID] == "" {
		m.mu.Unlock()
		return
	}
	// Modern managed hooks carry an immutable execution fence, including Codex
	// daemon notifications that no longer have the launch environment. Legacy
	// ownerless signals remain compatible only without an activity observer.
	if m.activityObserver != nil || signal.OwnerID != "" || signal.RunID != "" {
		run, ok := m.activityRuns[signal.SessionID]
		if !ok || signal.OwnerID != run.OwnerID || signal.RunID != run.RunID || signal.TerminalID != m.runtime[signal.SessionID] {
			m.mu.Unlock()
			return
		}
	}
	if m.adoptedRuns[signal.SessionID] != "" {
		m.mu.Unlock()
		m.observeAdoptedSignal(signal)
		return
	}
	session, ok := m.store.Get(signal.SessionID)
	if !ok {
		m.mu.Unlock()
		return
	}
	switch signal.Kind {
	case SignalDone:
		session.State = SessionDone
	case SignalNeedsInput:
		session.State = SessionNeedsInput
	case SignalError:
		session.State = SessionError
		session.ExitError = "provider reported a turn failure"
	default:
		m.mu.Unlock()
		return
	}
	session.UpdatedAt = signal.CreatedAt.UTC()
	session.LastActivityAt = signal.CreatedAt.UTC()
	if m.focusedID != signal.SessionID {
		session.Unread = true
	}
	err := m.store.Upsert(session)
	m.mu.Unlock()
	if err == nil {
		m.emitEvent("agent:session-changed", session)
	}
}

// IsFocused reports whether the session is the one the user most recently
// opened. Notification code uses this to avoid interrupting the user with an
// OS toast for activity they are already looking at.
func (m *Manager) IsFocused(id string) bool { return m.isFocused(id) }

// HasLiveSharedCheckout reports whether switching the project's main checkout
// would move files underneath an active current/branch agent session.
func (m *Manager) HasLiveSharedCheckout(projectName string) bool {
	for _, session := range m.store.List(projectName, false) {
		if (session.LaunchMode == LaunchCurrent || session.LaunchMode == LaunchBranch) && m.runtimeTerminal(session.ID) != "" {
			return true
		}
	}
	return false
}

func IsLiveState(state SessionState) bool { return isLiveState(state) }

func (m *Manager) profile(ctx context.Context, id AgentID) (AgentProfile, error) {
	for _, profile := range m.catalog.Profiles(ctx) {
		if profile.ID != id {
			continue
		}
		if !profile.Available {
			return AgentProfile{}, i18n.Errorf("errors.agents.agentUnavailableWithReason", map[string]any{"agent": profile.Label, "reason": profile.Reason})
		}
		return profile, nil
	}
	return AgentProfile{}, i18n.Errorf("errors.agents.unknownAgent", map[string]any{"agent": id})
}

func (m *Manager) pump(sessionID, terminalID string, terminalSession pty.Session) {
	buffer := make([]byte, 4096)
	for {
		n, err := terminalSession.Read(buffer)
		if n > 0 {
			chunk := string(buffer[:n])
			m.emitEvent("term:data:"+terminalID, chunk)
			m.recordOutput(sessionID, terminalID, chunk)
		}
		if err != nil {
			m.emitEvent("term:exit:"+terminalID, err.Error())
			m.finish(sessionID, terminalID, err)
			return
		}
	}
}

func (m *Manager) recordOutput(sessionID, terminalID, chunk string) {
	m.mu.Lock()
	if m.runtime[sessionID] != terminalID {
		m.mu.Unlock()
		return
	}
	session, ok := m.store.Get(sessionID)
	if !ok {
		m.mu.Unlock()
		return
	}
	now := m.now().UTC()
	session.State = SessionWorking
	session.UpdatedAt = now
	session.LastActivityAt = now
	session.LastPreview = SanitizePreview(session.LastPreview + chunk)
	err := m.store.Upsert(session)
	m.mu.Unlock()
	if err == nil {
		m.emitEvent("agent:session-changed", session)
	}
}

func (m *Manager) finish(sessionID, terminalID string, readErr error) {
	m.mu.Lock()
	if m.runtime[sessionID] != terminalID {
		m.mu.Unlock()
		return
	}
	delete(m.runtime, sessionID)
	delete(m.adoptedRuns, sessionID)
	run := m.activityRuns[sessionID]
	delete(m.activityRuns, sessionID)
	session, ok := m.store.Get(sessionID)
	if !ok {
		m.mu.Unlock()
		m.notifyActivityEnd(run, m.now().UTC())
		return
	}
	now := m.now().UTC()
	session.TerminalID = ""
	session.UpdatedAt = now
	session.LastActivityAt = now
	session.State = SessionStopped
	if readErr != nil && !errors.Is(readErr, io.EOF) && !isClosedReadError(readErr.Error()) {
		session.State = SessionError
		session.ExitError = readErr.Error()
	}
	if session.State == SessionError && m.focusedID != sessionID {
		session.Unread = true
	}
	err := m.store.Upsert(session)
	m.mu.Unlock()
	m.notifyActivityEnd(run, now)
	if err == nil {
		m.emitEvent("agent:session-changed", session)
	}
}

func (m *Manager) runtimeTerminal(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runtime[sessionID]
}

func (m *Manager) isFocused(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.focusedID == sessionID
}

func (m *Manager) emitEvent(event string, data ...any) {
	if event == "agent:session-changed" && len(data) > 0 {
		if session, ok := data[0].(AgentSession); ok {
			m.notifyActivityRoot(session)
		}
	}
	if m.emit != nil {
		m.emit(event, data...)
	}
}

func (m *Manager) warnSharedCheckout(projectName, projectPath string) {
	projectKey := NormalizeProjectPath(projectPath)
	for _, session := range m.store.List(projectName, false) {
		if session.ProjectKey != projectKey || m.runtimeTerminal(session.ID) == "" {
			continue
		}
		m.emitEvent(
			"agent:shared-checkout",
			i18n.T("backend.agents.sharedCheckoutWarning", map[string]any{"title": session.Title}),
		)
		return
	}
}

func isLiveState(state SessionState) bool {
	switch state {
	case SessionStarting, SessionWorking, SessionNeedsInput, SessionDone, SessionIdle:
		return true
	default:
		return false
	}
}

func isClosedReadError(errText string) bool {
	message := strings.ToLower(errText)
	return strings.Contains(message, "closed") ||
		strings.Contains(message, "operation has been canceled") ||
		strings.Contains(message, "the handle is invalid") ||
		strings.Contains(message, "invalid handle")
}

func agentTerminalEnvironment(sessionID string, runIDs ...string) map[string]string {
	env := map[string]string{
		"TERM":          "xterm-256color",
		"COLORTERM":     "truecolor",
		"TERM_PROGRAM":  "LaraEnv",
		"NO_COLOR":      "",
		AgentSessionEnv: sessionID,
	}
	if len(runIDs) > 0 {
		env["LARAENV_AGENT_RUN"] = runIDs[0]
	}
	return env
}

func filepathBase(path string) string {
	path = strings.TrimRight(path, `\/`)
	if path == "" {
		return ""
	}
	if index := strings.LastIndexAny(path, `\/`); index >= 0 {
		return path[index+1:]
	}
	return path
}

// observeAdoptedSignal handles hook events from LaraEnv-owned terminals that
// did not go through Launch. One live agent is tracked per terminal.
func (m *Manager) observeAdoptedSignal(signal SessionSignal) {
	switch signal.Kind {
	case SignalStarted:
		m.adoptSession(signal)
	case SignalEnded:
		m.endAdoptedSession(signal)
	case SignalWorking, SignalDone, SignalNeedsInput, SignalError:
		m.updateAdoptedState(signal)
	}
}

// staleProviderSignal reports whether a signal naming a provider session
// refers to one other than the session currently bound to the terminal. This
// guards against SessionEnd/state hooks for a provider session /clear (or a
// fresh start) already replaced: Claude fires SessionEnd(old) and
// SessionStart(new) back to back, and if those hook processes race so
// started(new) is recorded before ended(old) arrives, the stale ended/state
// signal must not touch the new session. An empty id on either side is
// inconclusive (older provider versions, or a session not yet captured) and
// never blocks the signal.
func staleProviderSignal(signalProviderSessionID, boundProviderSessionID string) bool {
	return signalProviderSessionID != "" && boundProviderSessionID != "" && signalProviderSessionID != boundProviderSessionID
}

func (m *Manager) adoptSession(signal SessionSignal) {
	m.mu.Lock()
	resolver := m.pathResolver
	m.mu.Unlock()
	if resolver == nil || strings.TrimSpace(signal.Cwd) == "" {
		return
	}
	location, ok := resolver(signal.Cwd)
	if !ok {
		return
	}
	now := m.now().UTC()

	m.mu.Lock()

	if (signal.SessionID != "" || signal.RunID != "") && m.adoptedSignalSessionLocked(signal) == "" {
		m.mu.Unlock()
		return
	}
	if current := m.sessionForTerminalLocked(signal.TerminalID); current != "" && m.adoptedRuns[current] == "" {
		m.mu.Unlock()
		return
	}
	// A terminal runs one agent at a time: /clear or a fresh start replaces it.
	var stoppedPrevious AgentSession
	var stoppedRun ActivityRun
	var stoppedMetadataErr error
	stoppedAny := false
	var session AgentSession
	found := false
	if previousID := m.sessionForTerminalLocked(signal.TerminalID); previousID != "" {
		if previous, ok := m.store.Get(previousID); ok {
			if previous.AgentID == signal.Provider && previous.ProviderSessionID == signal.ProviderSessionID {
				// The terminal binding also identifies starts with no provider
				// session ID yet; repeating one cannot create a second root.
				session, found = previous, true
			} else {
				stoppedRun = m.activityRuns[previousID]
				if updated, stopped, stopErr := m.stopAdoptedLocked(previousID, signal.TerminalID); stopped {
					stoppedPrevious, stoppedAny = updated, true
					stoppedMetadataErr = stopErr
				} else {
					m.mu.Unlock()
					return
				}
			}
		}
	}

	if !found && signal.ProviderSessionID != "" {
		for _, candidate := range m.store.List("", true) {
			if candidate.AgentID != signal.Provider || candidate.ProviderSessionID != signal.ProviderSessionID {
				continue
			}
			// A session already live in a different terminal must not be
			// reused here: doing so would silently orphan that terminal's
			// runtime binding. Only reuse a session that is not live, or is
			// live in this same terminal (e.g. a resume racing this signal).
			if liveTerminal := m.runtime[candidate.ID]; liveTerminal != "" && liveTerminal != signal.TerminalID {
				continue
			}
			session, found = candidate, true
			break
		}
	}
	if !found {
		session = AgentSession{
			ID: uuid.NewString(), AgentID: signal.Provider, Title: providerLabel(signal.Provider),
			ProviderSessionID: signal.ProviderSessionID, StartedAt: now, Adopted: true,
		}
	}
	session.ProjectName = location.ProjectName
	session.ProjectPath = location.ProjectPath
	session.WorktreePath = location.WorktreePath
	session.GitBranch = location.Branch
	session.LaunchMode = location.Mode
	if session.LaunchMode == "" {
		session.LaunchMode = LaunchCurrent
	}
	session.TerminalID = signal.TerminalID
	session.State = SessionIdle
	session.ExitError = ""
	session.Archived = false
	session.Unread = false
	session.UpdatedAt = now
	session.LastActivityAt = now

	err := m.store.Upsert(session)
	var run ActivityRun
	newRun := false
	if err == nil {
		run = m.activityRuns[session.ID]
		if run.RunID == "" || m.runtime[session.ID] != signal.TerminalID {
			run = m.newActivityRunLocked(session)
			newRun = true
		}
		run.Session = activityRootSession(session)
		m.activityRuns[session.ID] = run
		m.adoptedRuns[session.ID] = run.RunID
		m.runtime[session.ID] = signal.TerminalID
	}
	observer := m.activityObserver
	m.mu.Unlock()

	if stoppedAny {
		m.notifyActivityEnd(stoppedRun, now)
		if stoppedMetadataErr == nil {
			m.emitEvent("agent:session-changed", stoppedPrevious)
		}
	}
	if err == nil {
		if newRun && observer != nil {
			observer.ReserveRun(run)
			observer.BeginRun(run)
		}
		m.emitEvent("agent:session-changed", session)
	}
}

func (m *Manager) updateAdoptedState(signal SessionSignal) {
	m.mu.Lock()
	sessionID := m.adoptedSignalSessionLocked(signal)
	if sessionID == "" {
		m.mu.Unlock()
		return
	}
	session, ok := m.store.Get(sessionID)
	if !ok {
		m.mu.Unlock()
		return
	}
	if staleProviderSignal(signal.ProviderSessionID, session.ProviderSessionID) {
		m.mu.Unlock()
		return
	}
	switch signal.Kind {
	case SignalWorking:
		session.State = SessionWorking
	case SignalDone:
		session.State = SessionDone
	case SignalNeedsInput:
		session.State = SessionNeedsInput
	case SignalError:
		session.State = SessionError
		session.ExitError = "provider reported a turn failure"
	}
	session.UpdatedAt = signal.CreatedAt.UTC()
	session.LastActivityAt = signal.CreatedAt.UTC()
	if signal.Kind != SignalWorking && m.focusedID != sessionID {
		session.Unread = true
	}
	err := m.store.Upsert(session)
	m.mu.Unlock()
	if err == nil {
		m.emitEvent("agent:session-changed", session)
	}
}

// endAdoptedSession stops the session bound to signal.TerminalID for a
// SessionEnd hook. Claude fires SessionEnd(old) and SessionStart(new) back to
// back on /clear; if those hook processes race so started(new) is recorded
// before ended(old) arrives, staleProviderSignal recognizes this SessionEnd
// still names the replaced provider session and drops it instead of stopping
// (and orphaning) the session that just replaced it.
func (m *Manager) endAdoptedSession(signal SessionSignal) {
	m.mu.Lock()
	sessionID := m.adoptedSignalSessionLocked(signal)
	if sessionID == "" {
		m.mu.Unlock()
		return
	}
	session, ok := m.store.Get(sessionID)
	if !ok {
		m.mu.Unlock()
		return
	}
	if staleProviderSignal(signal.ProviderSessionID, session.ProviderSessionID) {
		m.mu.Unlock()
		return
	}
	run := m.activityRuns[sessionID]
	updated, stopped, err := m.stopAdoptedLocked(sessionID, signal.TerminalID)
	m.mu.Unlock()
	if stopped {
		m.notifyActivityEnd(run, updated.UpdatedAt)
		if err == nil {
			m.emitEvent("agent:session-changed", updated)
		}
	}
}

// TerminalClosed is called when a plain LaraEnv (or SSH) terminal's PTY read
// loop exits. Any adopted session bound to it stops unconditionally — there
// is no SessionEnd payload to compare provider ids against, only the fact
// that the terminal itself is gone.
func (m *Manager) TerminalClosed(terminalID string) {
	m.mu.Lock()
	sessionID := m.sessionForTerminalLocked(terminalID)
	m.mu.Unlock()
	if sessionID == "" {
		return
	}
	m.stopAdopted(sessionID, terminalID)
}

func (m *Manager) stopAdopted(sessionID, terminalID string) {
	m.mu.Lock()
	run := m.activityRuns[sessionID]
	session, stopped, err := m.stopAdoptedLocked(sessionID, terminalID)
	m.mu.Unlock()
	if stopped {
		m.notifyActivityEnd(run, session.UpdatedAt)
		if err == nil {
			m.emitEvent("agent:session-changed", session)
		}
	}
}

// stopAdoptedLocked stops the session bound to terminalID, assuming m.mu is
// already held. It only ever stops a current adopted execution, so a
// terminal-close notification can never reach into (and stop) a session
// Manager.Launch owns. The bool reports runtime removal independently of the
// metadata error: a terminal exit ends observation even if persistence fails.
// Callers always end a removed run, but emit persisted changes only on success.
func (m *Manager) stopAdoptedLocked(sessionID, terminalID string) (AgentSession, bool, error) {
	if m.runtime[sessionID] != terminalID || m.adoptedRuns[sessionID] == "" {
		return AgentSession{}, false, nil
	}
	session, ok := m.store.Get(sessionID)
	if !ok {
		return AgentSession{}, false, nil
	}
	delete(m.runtime, sessionID)
	delete(m.activityRuns, sessionID)
	delete(m.adoptedRuns, sessionID)
	now := m.now().UTC()
	session.TerminalID = ""
	session.State = SessionStopped
	session.UpdatedAt = now
	session.LastActivityAt = now
	err := m.store.Upsert(session)
	return session, true, err
}

// AdoptedSessionForTerminal returns the live session bound to a terminal that
// was adopted through a provider hook (never a Launch-owned terminal).
func (m *Manager) AdoptedSessionForTerminal(terminalID string) (AgentSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sessionID := m.sessionForTerminalLocked(terminalID)
	if sessionID == "" || m.adoptedRuns[sessionID] == "" {
		return AgentSession{}, false
	}
	session, ok := m.store.Get(sessionID)
	if !ok {
		return AgentSession{}, false
	}
	return session, true
}

// Recheck the immutable fence under the mutation lock: a replacement may
// have started after ObserveSignal chose the adopted lifecycle route.
func (m *Manager) adoptedSignalSessionLocked(signal SessionSignal) string {
	id := m.sessionForTerminalLocked(signal.TerminalID)
	run, ok := m.activityRuns[id]
	if !ok || m.adoptedRuns[id] != run.RunID || signal.Provider != run.Session.AgentID {
		return ""
	}
	if signal.OwnerID != "" && signal.OwnerID != run.OwnerID {
		return ""
	}
	if signal.SessionID != "" || signal.RunID != "" {
		if signal.SessionID != id || signal.RunID != run.RunID || signal.OwnerID != run.OwnerID {
			return ""
		}
	}
	return id
}

func (m *Manager) sessionForTerminalLocked(terminalID string) string {
	for sessionID, boundTerminal := range m.runtime {
		if boundTerminal == terminalID {
			return sessionID
		}
	}
	return ""
}

func providerLabel(id AgentID) string {
	for _, definition := range builtInProviders() {
		if definition.id == id {
			return definition.label
		}
	}
	return string(id)
}

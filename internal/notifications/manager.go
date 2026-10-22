package notifications

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"lara4ever/internal/agents"
	"lara4ever/internal/i18n"
)

type ToastSender interface {
	Push(title, message string, sound bool) error
}

type EventEmitter func(event string, data ...any)

type Manager struct {
	store  *Store
	toasts ToastSender
	emit   EventEmitter
	now    func() time.Time

	mu             sync.Mutex
	sessionStates  map[string]agents.SessionState
	usageThreshold map[string]int
	lastToast      map[string]time.Time
}

func NewManager(store *Store, toasts ToastSender, emit EventEmitter) *Manager {
	return &Manager{
		store: store, toasts: toasts, emit: emit, now: time.Now,
		sessionStates: make(map[string]agents.SessionState), usageThreshold: make(map[string]int), lastToast: make(map[string]time.Time),
	}
}

func (m *Manager) List() []Entry { return m.store.List() }

func (m *Manager) UnreadCount() int { return m.store.UnreadCount() }

func (m *Manager) Preferences() Preferences { return m.store.Preferences() }

func (m *Manager) SetPreferences(preferences Preferences) error {
	if err := m.store.SetPreferences(preferences); err != nil {
		return err
	}
	m.changed()
	return nil
}

func (m *Manager) MarkRead(id string) error {
	if err := m.store.MarkRead(id); err != nil {
		return err
	}
	m.changed()
	return nil
}

func (m *Manager) MarkAllRead() error {
	if err := m.store.MarkAllRead(); err != nil {
		return err
	}
	m.changed()
	return nil
}

func (m *Manager) ObserveSession(session agents.AgentSession, focused bool) {
	m.mu.Lock()
	previous, seen := m.sessionStates[session.ID]
	m.sessionStates[session.ID] = session.State
	m.mu.Unlock()
	if !seen || previous == session.State || session.Archived {
		return
	}

	preferences := m.store.Preferences()
	entry := Entry{Target: Target{ProjectName: session.ProjectName, SessionID: session.ID, Provider: string(session.AgentID)}, Read: focused}
	switch session.State {
	case agents.SessionNeedsInput:
		if !preferences.NeedsInput {
			return
		}
		entry.Category, entry.Severity = CategoryNeedsInput, "warning"
		entry.Title = i18n.T("backend.notifications.sessionNeedsInput", map[string]any{"title": session.Title})
		entry.Message = projectMessage(session.ProjectName, session.LastPreview)
	case agents.SessionDone:
		if !preferences.Completion {
			return
		}
		entry.Category, entry.Severity = CategoryCompletion, "success"
		entry.Title = i18n.T("backend.notifications.sessionDone", map[string]any{"title": session.Title})
		entry.Message = projectMessage(session.ProjectName, session.LastPreview)
	case agents.SessionError:
		if !preferences.Errors {
			return
		}
		entry.Category, entry.Severity = CategoryError, "error"
		entry.Title = i18n.T("backend.notifications.sessionError", map[string]any{"title": session.Title})
		entry.Message = projectMessage(session.ProjectName, firstNonEmpty(session.ExitError, session.LastPreview))
	default:
		return
	}
	entry.DedupeKey = fmt.Sprintf("session:%s:%s:%s", session.ID, session.State, session.UpdatedAt.UTC().Format(time.RFC3339Nano))
	m.publish(entry, !focused, preferences)
}

func (m *Manager) ObserveUsage(snapshots []agents.UsageSnapshot) {
	preferences := m.store.Preferences()
	for _, snapshot := range snapshots {
		if snapshot.Status != "available" {
			continue
		}
		for _, window := range snapshot.Windows {
			if window.UsedPct == nil {
				continue
			}
			level := threshold(*window.UsedPct)
			key := string(snapshot.Provider) + ":" + window.ID
			m.mu.Lock()
			previous, seen := m.usageThreshold[key]
			m.usageThreshold[key] = level
			m.mu.Unlock()
			if !seen || level <= previous || level < 80 || !preferences.UsageWarnings {
				continue
			}
			severity := "warning"
			if level >= 95 {
				severity = "error"
			}
			cycle := "unknown-reset"
			if window.ResetsAt != nil {
				cycle = window.ResetsAt.UTC().Format(time.RFC3339)
			}
			entry := Entry{
				DedupeKey: fmt.Sprintf("usage:%s:%s:%d:%s", snapshot.Provider, window.ID, level, cycle),
				Category:  CategoryUsage, Severity: severity,
				Title: i18n.T("backend.notifications.usageReachedTitle", map[string]any{
					"provider": providerLabel(snapshot.Provider), "percent": level,
				}),
				Message: i18n.T("backend.notifications.usageMessage", map[string]any{
					"label": window.Label, "percent": fmt.Sprintf("%.0f", *window.UsedPct),
				}),
				Target: Target{Provider: string(snapshot.Provider)},
			}
			m.publish(entry, true, preferences)
		}
	}
}

// NotifyLocalCAUntrusted tells the user the local certificate authority
// isn't trusted by this machine yet — the state switching a project's HTTPS
// on leaves them in when nothing (successfully) auto-installed it. Fires
// regardless of install kind: a portable copy never auto-trusts (by
// design), but an MSI install's best-effort auto-trust can also fail (not
// elevated, group policy), leaving it in the identical state.
//
// Deduped on the CA's thumbprint, not the project name: trusting one CA
// fixes every project signed by it, so one nudge per untrusted CA is the
// right granularity — re-toggling the same project's SSL, or turning SSL on
// for a second project signed by the same still-untrusted CA, doesn't repeat
// it, but a different (e.g. later-created) CA still raises its own.
func (m *Manager) NotifyLocalCAUntrusted(projectName, caThumbprint string) {
	preferences := m.store.Preferences()
	if !preferences.NeedsInput {
		return
	}
	entry := Entry{
		DedupeKey: "local-ca-untrusted:" + caThumbprint,
		Category:  CategoryNeedsInput,
		Severity:  "warning",
		Title:     i18n.T("backend.notifications.localCAUntrustedTitle", nil),
		Message:   projectMessage(projectName, i18n.T("backend.notifications.localCAUntrustedMessage", nil)),
		Target:    Target{ProjectName: projectName},
	}
	m.publish(entry, true, preferences)
}

func (m *Manager) publish(entry Entry, allowToast bool, preferences Preferences) {
	now := m.now().UTC()
	entry.ID = uuid.NewString()
	entry.CreatedAt = now
	created, added, err := m.store.Add(entry)
	if err != nil || !added {
		return
	}
	m.changed()
	if !allowToast || !preferences.OSToast || m.toasts == nil || !m.allowToast(entry, now) {
		return
	}
	go func() { _ = m.toasts.Push(created.Title, created.Message, preferences.Sound) }()
}

func (m *Manager) allowToast(entry Entry, now time.Time) bool {
	scope := entry.Target.SessionID
	if entry.Category == CategoryError || entry.Category == CategoryUsage {
		scope = entry.Target.Provider
	}
	key := string(entry.Category) + ":" + entry.Severity + ":" + scope
	m.mu.Lock()
	defer m.mu.Unlock()
	if last := m.lastToast[key]; !last.IsZero() && now.Sub(last) < time.Minute {
		return false
	}
	m.lastToast[key] = now
	return true
}

func (m *Manager) changed() {
	if m.emit != nil {
		m.emit("notification:changed", m.store.UnreadCount())
	}
}

func threshold(used float64) int {
	if used >= 95 {
		return 95
	}
	if used >= 80 {
		return 80
	}
	return 0
}

func projectMessage(project, detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return project
	}
	if len([]rune(detail)) > 240 {
		runes := []rune(detail)
		detail = string(runes[len(runes)-240:])
	}
	return project + " · " + detail
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func providerLabel(provider agents.AgentID) string {
	switch provider {
	case agents.AgentCodex:
		return "Codex"
	case agents.AgentClaude:
		return "Claude Code"
	case agents.AgentKimi:
		return "Kimi Code"
	case agents.AgentOpenCode:
		return "OpenCode"
	default:
		return string(provider)
	}
}

package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"lara4ever/internal/atomicfile"
	"lara4ever/internal/i18n"
)

const (
	currentStoreVersion = 1
	MaxPreviewRunes     = 2000
)

var (
	ansiCSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	ansiOSC = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
)

type sessionFile struct {
	Version  int            `json:"version"`
	Sessions []AgentSession `json:"sessions"`
}

type StoreNotice struct {
	Message       string `json:"message"`
	PreservedPath string `json:"preservedPath,omitempty"`
}

type SessionStore struct {
	mu       sync.RWMutex
	path     string
	sessions map[string]AgentSession
	notice   func(StoreNotice)
	now      func() time.Time
}

func NewSessionStore(path string, notice func(StoreNotice)) (*SessionStore, error) {
	store := &SessionStore{
		path:     path,
		sessions: make(map[string]AgentSession),
		notice:   notice,
		now:      time.Now,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SessionStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	file, err := decodeSessionFile(data)
	if err != nil {
		preserved := s.path + ".corrupt-" + s.now().UTC().Format("20060102T150405.000000000Z")
		if renameErr := os.Rename(s.path, preserved); renameErr != nil {
			return i18n.Wrapf(err, "errors.agents.decodeSessionsFailed", map[string]any{"renameError": renameErr})
		}
		if s.notice != nil {
			// Safe to call i18n.T here: this only runs from NewSessionStore,
			// which app.go's NewApp calls — and main.go now loads catalogues
			// into i18n.Global() before NewApp runs at all (see LoadCatalogues's
			// doc comment in app.go). Confirmed by reading that ordering, not
			// assumed.
			s.notice(StoreNotice{Message: i18n.T("backend.agents.sessionHistoryMalformed", nil), PreservedPath: preserved})
		}
		return nil
	}
	for _, session := range file.Sessions {
		normalizeSession(&session)
		if session.ID != "" {
			s.sessions[session.ID] = session
		}
	}
	return nil
}

func decodeSessionFile(data []byte) (sessionFile, error) {
	var file sessionFile
	if err := json.Unmarshal(data, &file); err == nil && (file.Version > 0 || file.Sessions != nil) {
		if file.Version > currentStoreVersion {
			return sessionFile{}, i18n.Errorf("errors.agents.unsupportedSessionSchema", map[string]any{"version": file.Version})
		}
		file.Version = currentStoreVersion
		return file, nil
	}

	// Pre-version prototype builds stored the session slice directly.
	var legacy []AgentSession
	if err := json.Unmarshal(data, &legacy); err != nil {
		return sessionFile{}, err
	}
	return sessionFile{Version: currentStoreVersion, Sessions: legacy}, nil
}

func (s *SessionStore) List(projectName string, includeArchived bool) []AgentSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]AgentSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		if projectName != "" && !strings.EqualFold(session.ProjectName, projectName) {
			continue
		}
		if session.Archived && !includeArchived {
			continue
		}
		result = append(result, cloneSession(session))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result
}

func (s *SessionStore) Get(id string) (AgentSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return cloneSession(session), ok
}

func (s *SessionStore) Upsert(session AgentSession) error {
	if strings.TrimSpace(session.ID) == "" {
		return i18n.Errorf("errors.agents.sessionIdRequired", nil)
	}
	normalizeSession(&session)
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.sessions[session.ID]
	s.sessions[session.ID] = session
	if err := s.saveLocked(); err != nil {
		if existed {
			s.sessions[session.ID] = previous
		} else {
			delete(s.sessions, session.ID)
		}
		return err
	}
	return nil
}

func (s *SessionStore) MarkRead(id string) (AgentSession, error) {
	return s.update(id, func(session *AgentSession) { session.Unread = false })
}

func (s *SessionStore) Rename(id, title string) (AgentSession, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return AgentSession{}, i18n.Errorf("errors.agents.sessionTitleRequired", nil)
	}
	return s.update(id, func(session *AgentSession) { session.Title = title })
}

func (s *SessionStore) Archive(id string) (AgentSession, error) {
	return s.update(id, func(session *AgentSession) {
		session.Archived = true
		session.Unread = false
	})
}

func (s *SessionStore) update(id string, mutate func(*AgentSession)) (AgentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return AgentSession{}, i18n.Errorf("errors.agents.sessionNotFound", map[string]any{"id": id})
	}
	previous := session
	mutate(&session)
	session.UpdatedAt = s.now().UTC()
	normalizeSession(&session)
	s.sessions[id] = session
	if err := s.saveLocked(); err != nil {
		s.sessions[id] = previous
		return AgentSession{}, err
	}
	return cloneSession(session), nil
}

func (s *SessionStore) Purge(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.sessions[id]
	if !ok {
		return nil
	}
	delete(s.sessions, id)
	if err := s.saveLocked(); err != nil {
		s.sessions[id] = previous
		return err
	}
	return nil
}

func (s *SessionStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// Reset archives LaraEnv's session metadata and starts a fresh store. It does
// not inspect or modify any provider-owned history, authentication, or state.
func (s *SessionStore) Reset() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	previous := s.sessions
	archived := ""
	if _, err := os.Stat(s.path); err == nil {
		archived = s.path + ".reset-" + s.now().UTC().Format("20060102T150405.000000000Z")
		if err := os.Rename(s.path, archived); err != nil {
			return "", i18n.Wrapf(err, "errors.agents.archiveSessionMetadataFailed", nil)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	s.sessions = make(map[string]AgentSession)
	if err := s.saveLocked(); err != nil {
		s.sessions = previous
		if archived != "" {
			_ = os.Remove(s.path)
			_ = os.Rename(archived, s.path)
		}
		return "", err
	}
	return archived, nil
}

func (s *SessionStore) saveLocked() error {
	sessions := make([]AgentSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID < sessions[j].ID })
	data, err := json.MarshalIndent(sessionFile{Version: currentStoreVersion, Sessions: sessions}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(s.path, append(data, '\n'), 0644)
}

func normalizeSession(session *AgentSession) {
	session.AdditionalDirectories = append([]string(nil), session.AdditionalDirectories...)
	session.ProjectPath = filepath.Clean(strings.TrimSpace(session.ProjectPath))
	session.ProjectKey = NormalizeProjectPath(session.ProjectPath)
	session.LastPreview = SanitizePreview(session.LastPreview)
	if session.LaunchMode == "" {
		session.LaunchMode = LaunchCurrent
	}
}

func cloneSession(session AgentSession) AgentSession {
	session.AdditionalDirectories = append([]string(nil), session.AdditionalDirectories...)
	return session
}

func NormalizeProjectPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return strings.ToLower(filepath.Clean(path))
}

func SanitizePreview(preview string) string {
	preview = ansiOSC.ReplaceAllString(preview, "")
	preview = ansiCSI.ReplaceAllString(preview, "")
	preview = strings.ReplaceAll(preview, "\r\n", "\n")
	preview = strings.ReplaceAll(preview, "\r", "\n")
	preview = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, preview)
	runes := []rune(preview)
	if len(runes) > MaxPreviewRunes {
		runes = runes[len(runes)-MaxPreviewRunes:]
	}
	return strings.TrimSpace(string(runes))
}

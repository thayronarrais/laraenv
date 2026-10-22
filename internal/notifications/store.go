package notifications

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"lara4ever/internal/atomicfile"
	"lara4ever/internal/i18n"
)

const (
	storeVersion = 1
	maxEntries   = 200
)

type Category string

const (
	CategoryCompletion Category = "completion"
	CategoryNeedsInput Category = "needs-input"
	CategoryError      Category = "error"
	CategoryUsage      Category = "usage"
)

type Target struct {
	ProjectName string `json:"projectName,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	Provider    string `json:"provider,omitempty"`
}

type Entry struct {
	ID        string    `json:"id"`
	DedupeKey string    `json:"-"`
	Category  Category  `json:"category"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Severity  string    `json:"severity"`
	Target    Target    `json:"target"`
	CreatedAt time.Time `json:"createdAt"`
	Read      bool      `json:"read"`
}

type Preferences struct {
	OSToast       bool `json:"osToast"`
	Sound         bool `json:"sound"`
	Completion    bool `json:"completion"`
	NeedsInput    bool `json:"needsInput"`
	Errors        bool `json:"errors"`
	UsageWarnings bool `json:"usageWarnings"`
}

func DefaultPreferences() Preferences {
	return Preferences{OSToast: true, Sound: true, Completion: true, NeedsInput: true, Errors: true, UsageWarnings: true}
}

type persistedEntry struct {
	Entry
	DedupeKey string `json:"dedupeKey"`
}

type storeFile struct {
	Version     int              `json:"version"`
	Entries     []persistedEntry `json:"entries"`
	Preferences Preferences      `json:"preferences"`
}

type StoreNotice struct {
	Message       string
	PreservedPath string
}

type Store struct {
	mu          sync.RWMutex
	path        string
	entries     []persistedEntry
	preferences Preferences
	now         func() time.Time
}

func NewStore(path string) (*Store, *StoreNotice, error) {
	s := &Store{path: path, preferences: DefaultPreferences(), now: time.Now}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, nil, err
	}
	notice, err := s.load()
	return s, notice, err
}

func (s *Store) load() (*StoreNotice, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil || file.Version < 1 || file.Version > storeVersion {
		if err == nil {
			err = i18n.Errorf("errors.notifications.unsupportedSchema", map[string]any{"version": file.Version})
		}
		preserved := s.path + ".corrupt-" + s.now().UTC().Format("20060102T150405.000000000Z")
		if renameErr := os.Rename(s.path, preserved); renameErr != nil {
			// i18n conversion note: the original was fmt.Errorf("decode
			// notifications: %w (also failed to preserve corrupt file: %v)",
			// err, renameErr) — err first, renameErr last. i18n.Wrapf's
			// Error() is fixed as "own message: wrapped.Error()", so this now
			// renders as "(also failed to preserve corrupt file: <renameErr>):
			// <err>" — reordered but no information lost, matching the same
			// precedent in internal/agents' store.go (task-18b).
			return nil, i18n.Wrapf(err, "errors.notifications.decodeFailed", map[string]any{"renameError": renameErr.Error()})
		}
		return &StoreNotice{Message: i18n.T("backend.notifications.historyMalformed", nil), PreservedPath: preserved}, nil
	}
	s.entries = file.Entries
	s.preferences = file.Preferences
	return nil, nil
}

func (s *Store) List() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Entry, len(s.entries))
	for i, entry := range s.entries {
		result[i] = entry.Entry
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}

func (s *Store) UnreadCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, entry := range s.entries {
		if !entry.Read {
			count++
		}
	}
	return count
}

func (s *Store) Add(entry Entry) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.entries {
		if entry.DedupeKey != "" && existing.DedupeKey == entry.DedupeKey {
			return existing.Entry, false, nil
		}
	}
	previous := append([]persistedEntry(nil), s.entries...)
	s.entries = append(s.entries, persistedEntry{Entry: entry, DedupeKey: entry.DedupeKey})
	sort.SliceStable(s.entries, func(i, j int) bool { return s.entries[i].CreatedAt.After(s.entries[j].CreatedAt) })
	if len(s.entries) > maxEntries {
		s.entries = s.entries[:maxEntries]
	}
	if err := s.saveLocked(); err != nil {
		s.entries = previous
		return Entry{}, false, err
	}
	return entry, true, nil
}

func (s *Store) MarkRead(id string) error {
	return s.updateEntries(func(entries []persistedEntry) bool {
		for i := range entries {
			if entries[i].ID == id && !entries[i].Read {
				entries[i].Read = true
				return true
			}
		}
		return false
	})
}

func (s *Store) MarkAllRead() error {
	return s.updateEntries(func(entries []persistedEntry) bool {
		changed := false
		for i := range entries {
			if !entries[i].Read {
				entries[i].Read = true
				changed = true
			}
		}
		return changed
	})
}

func (s *Store) updateEntries(change func([]persistedEntry) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := append([]persistedEntry(nil), s.entries...)
	if !change(s.entries) {
		return nil
	}
	if err := s.saveLocked(); err != nil {
		s.entries = previous
		return err
	}
	return nil
}

func (s *Store) Preferences() Preferences {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.preferences
}

func (s *Store) SetPreferences(preferences Preferences) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.preferences
	s.preferences = preferences
	if err := s.saveLocked(); err != nil {
		s.preferences = previous
		return err
	}
	return nil
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(storeFile{Version: storeVersion, Entries: s.entries, Preferences: s.preferences}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(s.path, append(data, '\n'), 0644)
}

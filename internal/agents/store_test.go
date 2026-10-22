package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoreRoundTripAndOperations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-sessions.json")
	store, err := NewSessionStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	session := AgentSession{
		ID: "session-1", ProjectName: "Demo", ProjectPath: filepath.Join(t.TempDir(), "Demo"),
		AgentID: AgentCodex, State: SessionWorking, Title: "Implement feature",
		StartedAt: now, UpdatedAt: now, LastActivityAt: now,
		LastPreview:           "\x1b]0;private title\x07\x1b[31mhello\x1b[0m",
		Unread:                true,
		AdditionalDirectories: []string{filepath.Join(t.TempDir(), "shared"), filepath.Join(t.TempDir(), "server")},
	}
	if err := store.Upsert(session); err != nil {
		t.Fatal(err)
	}

	loaded, err := NewSessionStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := loaded.Get(session.ID)
	if !ok {
		t.Fatal("session missing after reload")
	}
	if got.LastPreview != "hello" || got.ProjectKey != NormalizeProjectPath(session.ProjectPath) {
		t.Fatalf("unexpected normalized session: %#v", got)
	}
	if !reflect.DeepEqual(got.AdditionalDirectories, session.AdditionalDirectories) {
		t.Fatalf("additional context was not restored: %#v", got.AdditionalDirectories)
	}
	loaded.now = func() time.Time { return now.Add(time.Minute) }
	if _, err := loaded.MarkRead(session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.Rename(session.ID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.Archive(session.ID); err != nil {
		t.Fatal(err)
	}
	if got := loaded.List("demo", false); len(got) != 0 {
		t.Fatalf("archived session leaked into default list: %#v", got)
	}
	if got := loaded.List("DEMO", true); len(got) != 1 || got[0].Title != "Renamed" || got[0].Unread {
		t.Fatalf("unexpected archived session: %#v", got)
	}
	if err := loaded.Purge(session.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Get(session.ID); ok {
		t.Fatal("session still exists after purge")
	}
}

func TestStoreDoesNotShareAdditionalDirectorySlices(t *testing.T) {
	store, err := NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	directories := []string{`C:\shared`}
	if err := store.Upsert(AgentSession{ID: "context", ProjectPath: `C:\primary`, AdditionalDirectories: directories}); err != nil {
		t.Fatal(err)
	}
	directories[0] = "changed input"
	got, _ := store.Get("context")
	if got.AdditionalDirectories[0] != `C:\shared` {
		t.Fatal("caller changed saved context through Upsert input")
	}
	got.AdditionalDirectories[0] = "changed result"
	listed := store.List("", true)
	if listed[0].AdditionalDirectories[0] != `C:\shared` {
		t.Fatal("caller changed saved context through Get result")
	}
	listed[0].AdditionalDirectories[0] = "changed list"
	updated, err := store.Rename("context", "Renamed")
	if err != nil {
		t.Fatal(err)
	}
	if updated.AdditionalDirectories[0] != `C:\shared` {
		t.Fatal("caller changed saved context through List result")
	}
	updated.AdditionalDirectories[0] = "changed update result"
	got, _ = store.Get("context")
	if got.AdditionalDirectories[0] != `C:\shared` {
		t.Fatal("caller changed saved context through Rename result")
	}
}

func TestStoreConcurrentWritesRemainValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-sessions.json")
	store, err := NewSessionStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for i := 0; i < 32; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			id := "session-" + time.Unix(int64(index), 0).UTC().Format("150405")
			if err := store.Upsert(AgentSession{ID: id, ProjectName: "demo", ProjectPath: t.TempDir()}); err != nil {
				t.Errorf("upsert %d: %v", index, err)
			}
		}(i)
	}
	wait.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file sessionFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("persisted file is invalid JSON: %v", err)
	}
	if len(file.Sessions) != 32 {
		t.Fatalf("got %d sessions, want 32", len(file.Sessions))
	}
}

func TestStorePreservesMalformedInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-sessions.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	var notice StoreNotice
	store, err := NewSessionStore(path, func(got StoreNotice) { notice = got })
	if err != nil {
		t.Fatal(err)
	}
	if len(store.List("", true)) != 0 {
		t.Fatal("malformed store should start empty")
	}
	if notice.PreservedPath == "" {
		t.Fatal("expected a recovery notice with preserved path")
	}
	if _, err := os.Stat(notice.PreservedPath); err != nil {
		t.Fatalf("preserved malformed file missing: %v", err)
	}
}

func TestStoreMigratesLegacySlice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-sessions.json")
	legacy, err := json.Marshal([]AgentSession{{ID: "legacy", ProjectName: "Demo", ProjectPath: "."}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSessionStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("legacy"); !ok {
		t.Fatal("legacy session was not migrated")
	}
	if err := store.Flush(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Fatalf("migrated schema version missing: %s", data)
	}
}

func TestStoreSanitizesAndBoundsPreview(t *testing.T) {
	preview := "\x1b]9;secret\x07\x1b[32m" + strings.Repeat("é", MaxPreviewRunes+25) + "\x1b[0m"
	got := SanitizePreview(preview)
	if strings.Contains(got, "\x1b") {
		t.Fatal("preview still contains escape data")
	}
	if len([]rune(got)) != MaxPreviewRunes {
		t.Fatalf("preview has %d runes, want %d", len([]rune(got)), MaxPreviewRunes)
	}
}

func TestStoreResetArchivesPreviousMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-sessions.json")
	store, err := NewSessionStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 9, 4, 20, 0, 0, 123, time.UTC) }
	if err := store.Upsert(AgentSession{ID: "keep-in-archive", ProjectName: "demo", ProjectPath: `C:\projects\demo`}); err != nil {
		t.Fatal(err)
	}

	archived, err := store.Reset()
	if err != nil {
		t.Fatal(err)
	}
	if archived == "" {
		t.Fatal("reset did not return an archive path")
	}
	if len(store.List("", true)) != 0 {
		t.Fatal("in-memory session metadata was not reset")
	}
	archivedData, err := os.ReadFile(archived)
	if err != nil {
		t.Fatalf("archived metadata is missing: %v", err)
	}
	if !strings.Contains(string(archivedData), "keep-in-archive") {
		t.Fatal("archived metadata lost the previous session")
	}
	reloaded, err := NewSessionStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List("", true)) != 0 {
		t.Fatal("fresh metadata file is not empty")
	}
}

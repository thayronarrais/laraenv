package config

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lara4ever/internal/paths"
)

func TestDocumentWorkspaceJSONIsClonedAndLocal(t *testing.T) {
	s := newTestStore(t)
	raw := json.RawMessage(`{"version":1,"tabs":[],"activeTabIndex":-1,"visible":false,"panelPercent":45}`)
	want := string(raw)
	if err := s.SetDocumentWorkspace(raw); err != nil {
		t.Fatal(err)
	}
	raw[0] = '!'
	if got := string(s.GetDocumentWorkspace()); got != want {
		t.Fatal("retained caller slice", got)
	}
	read := s.GetDocumentWorkspace()
	read[0] = '!'
	snapshot := s.Get()
	snapshot.DocumentWorkspace[0] = '!'
	if got := string(s.GetDocumentWorkspace()); got != want {
		t.Fatal("exposed internal slice", got)
	}
	again, err := NewStore(s.path)
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, again.GetDocumentWorkspace()); err != nil {
		t.Fatal(err)
	}
	if compact.String() != want {
		t.Fatal("not persisted locally")
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestSetAndResolveProjectDevCommand(t *testing.T) {
	s := newTestStore(t)

	// Unset → empty.
	if got := s.ProjectDevCommand("app"); got != "" {
		t.Fatalf("unset: want \"\", got %q", got)
	}

	// Per-project override wins.
	if err := s.SetProjectDevCommand("app", "composer run dev"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := s.ProjectDevCommand("app"); got != "composer run dev" {
		t.Fatalf("override: want %q, got %q", "composer run dev", got)
	}

	// Global default applies to projects without an override.
	s.data.Projects.DefaultDevCommand = "npm run watch"
	if got := s.ProjectDevCommand("other"); got != "npm run watch" {
		t.Fatalf("default: want %q, got %q", "npm run watch", got)
	}
	// Override still beats the default.
	if got := s.ProjectDevCommand("app"); got != "composer run dev" {
		t.Fatalf("override over default: want %q, got %q", "composer run dev", got)
	}

	// Whitespace/empty clears the override.
	if err := s.SetProjectDevCommand("app", "  "); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := s.data.Projects.DevCommands["app"]; ok {
		t.Fatalf("clear: entry should be deleted")
	}
}

func TestSetAgentSessionsEnabledPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Get().Experimental.AgentSessions {
		t.Fatal("agent sessions must be opt-in by default")
	}
	if err := store.SetAgentSessionsEnabled(true); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Get().Experimental.AgentSessions {
		t.Fatal("agent sessions setting was not persisted")
	}
}

// TestAgentHooksOptOutMigration proves the old "agentHooks" opt-in key from
// existing config files is ignored (not migrated) and that the new
// AgentHooksDisabled opt-out defaults to false (hooks wanted) regardless of
// what the stale key said.
func TestAgentHooksOptOutMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"experimental":{"agentSessions":true,"agentHooks":true}}`), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Get().Experimental.AgentHooksDisabled {
		t.Fatal("stale agentHooks key must not set AgentHooksDisabled")
	}

	if err := store.SetAgentHooksDisabled(true); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Get().Experimental.AgentHooksDisabled {
		t.Fatal("agentHooksDisabled setting was not persisted")
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"agentHooksDisabled": true`) {
		t.Fatalf("expected persisted config to contain agentHooksDisabled: true, got %s", b)
	}
}

// A config.json written by an older version has no "projects.root" key.
// Updating must backfill it to the resolved install root — leaving it empty
// would point the app at nothing and the user's projects would disappear
// from the UI.
func TestNewStoreBackfillsProjectsRootForLegacyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"projects":{"tld":"test"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := store.Get().Projects.Root; got != paths.ProjectsRoot() {
		t.Fatalf("Projects.Root = %q, want %q", got, paths.ProjectsRoot())
	}
}

// A fresh install has no config.json at all. It never enters the migration
// branch, so it needs the same backfill.
func TestNewStoreSetsProjectsRootWhenNoConfigFileExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := store.Get().Projects.Root; got != paths.ProjectsRoot() {
		t.Fatalf("Projects.Root = %q, want %q", got, paths.ProjectsRoot())
	}
}

func TestSetAndGetLanguage(t *testing.T) {
	s := newTestStore(t)

	if got := s.Language(); got != "" {
		t.Fatalf("unset: want \"\", got %q", got)
	}
	if err := s.SetLanguage("pt-BR"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if got := s.Language(); got != "pt-BR" {
		t.Fatalf("want pt-BR, got %q", got)
	}

	// A second process reads the same file and must see it.
	reopened, err := NewStore(s.path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if got := reopened.Language(); got != "pt-BR" {
		t.Fatalf("after reopen: want pt-BR, got %q", got)
	}
}

func TestWatchReportsAnExternalWrite(t *testing.T) {
	s := newTestStore(t)

	changes := make(chan string, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Watch(ctx, func(c Config) { changes <- c.Language })

	// Another process writing the same file is what this simulates: detached
	// terminal windows and the command bar are separate processes, so the file
	// is the only thing that unites them.
	other, err := NewStore(s.path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// The watcher goroutine may not have reached fw.Add() yet, and a write that
	// lands before it does is lost with no second event to recover from. So
	// keep writing until it acknowledges rather than betting on one attempt.
	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	if err := other.SetLanguage("pt-BR"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	for {
		select {
		case got := <-changes:
			if got != "pt-BR" {
				t.Fatalf("watch reported %q, want pt-BR", got)
			}
			return
		case <-tick.C:
			if err := other.SetLanguage("pt-BR"); err != nil {
				t.Fatalf("SetLanguage: %v", err)
			}
		case <-deadline:
			t.Fatal("watch never fired")
		}
	}
}

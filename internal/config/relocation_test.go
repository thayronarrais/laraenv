package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/paths"
)

// configJSON builds the file body without any escaped backslashes in the
// source: %q renders a Windows path as valid JSON.
func configJSON(installRoot, projectsRoot string) string {
	return fmt.Sprintf(`{"installRoot":%q,"projects":{"root":%q}}`, installRoot, projectsRoot)
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func loadStore(t *testing.T, body string) *Store {
	t.Helper()
	s, err := NewStore(writeConfig(t, body))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// First run after upgrading: the config predates the field, so there is no
// move to detect. Adopt the current root and rewrite nothing.
func TestNewStoreAdoptsTheCurrentRootWhenNoneWasRecorded(t *testing.T) {
	s := loadStore(t, fmt.Sprintf(`{"projects":{"root":%q}}`, `C:\somewhere\www`))

	if got := s.Get().InstallRoot; !paths.SameRoot(got, paths.Root()) {
		t.Errorf("InstallRoot = %q, want the current root %q", got, paths.Root())
	}
	if got := s.Get().Projects.Root; got != `C:\somewhere\www` {
		t.Errorf("projects root was rewritten to %q on a first run", got)
	}
	if s.RelocatedFrom() != "" {
		t.Errorf("reported a relocation from %q", s.RelocatedFrom())
	}
}

// The environment was last used somewhere else: stored paths under the old
// root follow it, and the move is reported so the caller can fix up the state
// files and PHP inis, which this package does not own.
func TestNewStoreRebasesStoredPathsWhenTheRootMoved(t *testing.T) {
	old := `E:\stick\LaraEnv`
	s := loadStore(t, configJSON(old, filepath.Join(old, "www")))

	if got, want := s.Get().Projects.Root, filepath.Join(paths.Root(), "www"); got != want {
		t.Errorf("Projects.Root = %q, want %q", got, want)
	}
	if s.RelocatedFrom() != old {
		t.Errorf("RelocatedFrom = %q, want %q", s.RelocatedFrom(), old)
	}
	if !paths.SameRoot(s.Get().InstallRoot, paths.Root()) {
		t.Errorf("InstallRoot not updated: %q", s.Get().InstallRoot)
	}
}

// A projects folder deliberately kept outside the environment stays put.
func TestNewStoreLeavesAProjectsRootOutsideTheEnvironmentAlone(t *testing.T) {
	s := loadStore(t, configJSON(`E:\stick\LaraEnv`, `D:\code`))

	if got := s.Get().Projects.Root; got != `D:\code` {
		t.Errorf("Projects.Root = %q, want it untouched", got)
	}
}

// Same directory spelled differently is not a move - Windows ignores case.
func TestNewStoreDoesNotRelocateOnACaseOnlyDifference(t *testing.T) {
	shouted := strings.ToUpper(paths.Root())
	s := loadStore(t, configJSON(shouted, filepath.Join(paths.Root(), "www")))

	if s.RelocatedFrom() != "" {
		t.Errorf("treated a case difference as a move from %q", s.RelocatedFrom())
	}
}

// The move must survive the restart that follows it.
func TestRelocationIsPersisted(t *testing.T) {
	old := `E:\stick\LaraEnv`
	path := writeConfig(t, configJSON(old, filepath.Join(old, "www")))

	if _, err := NewStore(path); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if reloaded.RelocatedFrom() != "" {
		t.Errorf("relocated twice; the new root was not written back")
	}
	if got, want := reloaded.Get().Projects.Root, filepath.Join(paths.Root(), "www"); got != want {
		t.Errorf("Projects.Root = %q, want %q", got, want)
	}
}

func TestNewStorePreservesCustomProjectRootsContainingLegacyName(t *testing.T) {
	for _, root := range []string{
		`C:\code\lara4ever\www`,
		`D:\lara4ever\www`,
		`C:\lara4ever-copy\www`,
	} {
		t.Run(root, func(t *testing.T) {
			path := writeConfig(t, configJSON("", root))
			for load := 0; load < 2; load++ {
				store, err := NewStore(path)
				if err != nil {
					t.Fatal(err)
				}
				if got := store.Get().Projects.Root; got != root {
					t.Fatalf("load %d: custom Projects.Root = %q, want %q", load, got, root)
				}
			}
		})
	}
}

func TestNewStoreMigratesOnlyUnrecordedLegacyInstallRoot(t *testing.T) {
	for _, tc := range []struct {
		name        string
		installRoot string
		projects    string
		want        string
	}{
		{"legacy default", "", `C:\lara4ever\www`, `C:\laraenv\www`},
		{"legacy mixed case", "", `c:\LaRa4EvEr\MyProjects`, `c:\laraenv\MyProjects`},
		{"recorded custom projects", paths.Root(), `C:\lara4ever\www`, `C:\lara4ever\www`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, configJSON(tc.installRoot, tc.projects))
			for load := 0; load < 2; load++ {
				store, err := NewStore(path)
				if err != nil {
					t.Fatal(err)
				}
				if got := store.Get().Projects.Root; got != tc.want {
					t.Fatalf("load %d: Projects.Root = %q, want %q", load, got, tc.want)
				}
				if got := store.Get().InstallRoot; !paths.SameRoot(got, paths.Root()) {
					t.Fatalf("load %d: InstallRoot = %q, want current root %q", load, got, paths.Root())
				}
			}
		})
	}
}

func TestNewStoreRebasesRecordedLegacyRootBeforeReload(t *testing.T) {
	old := `C:\lara4ever`
	path := writeConfig(t, configJSON(old, `C:\lara4ever\www`))
	for load := 0; load < 2; load++ {
		store, err := NewStore(path)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := store.Get().Projects.Root, filepath.Join(paths.Root(), "www"); got != want {
			t.Fatalf("load %d: Projects.Root = %q, want relocated root %q", load, got, want)
		}
		if load == 0 && store.RelocatedFrom() != old {
			t.Fatalf("RelocatedFrom = %q, want %q", store.RelocatedFrom(), old)
		}
		if load == 1 && store.RelocatedFrom() != "" {
			t.Fatalf("relocation repeated after reload from %q", store.RelocatedFrom())
		}
	}
}

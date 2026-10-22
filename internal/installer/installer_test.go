package installer

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writePgTree(t *testing.T, root, pgVersion string, withControl bool) {
	t.Helper()
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeMarker(root, pgVersion); err != nil {
		t.Fatal(err)
	}
	if withControl {
		extDir := filepath.Join(root, "share", "extension")
		if err := os.MkdirAll(extDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extDir, "vector.control"), []byte("default_version = '0.8.6'\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIsInstalled_Pgvector(t *testing.T) {
	m := NewManager(t.TempDir(), func(Progress) {})

	t.Run("installed when control file exists and PG major matches", func(t *testing.T) {
		root := t.TempDir()
		writePgTree(t, root, "18.3", true)
		s := Source{Kind: KindPgvector, Version: "0.8.6-pg18", InstallTo: root}
		if !m.IsInstalled(s) {
			t.Fatal("expected pgvector to report installed for matching PG major")
		}
	})

	t.Run("not installed when PG major differs", func(t *testing.T) {
		root := t.TempDir()
		writePgTree(t, root, "18.3", true)
		s := Source{Kind: KindPgvector, Version: "0.8.6-pg17", InstallTo: root}
		if m.IsInstalled(s) {
			t.Fatal("expected pgvector NOT installed when entry targets a different PG major")
		}
	})

	t.Run("not installed without vector.control", func(t *testing.T) {
		root := t.TempDir()
		writePgTree(t, root, "18.3", false)
		s := Source{Kind: KindPgvector, Version: "0.8.6-pg18", InstallTo: root}
		if m.IsInstalled(s) {
			t.Fatal("expected pgvector NOT installed without vector.control")
		}
	})
}

// Installing pgvector extracts into the shared postgres tree; it must NOT
// overwrite the postgres .lara4ever-version marker, or the app would stop
// reporting PostgreSQL itself as installed.
func TestInstallFromFile_PgvectorPreservesPostgresMarker(t *testing.T) {
	root := t.TempDir()
	writePgTree(t, root, "18.3", false)

	zipPath := filepath.Join(t.TempDir(), "pgvector.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"lib/vector.dll":                    "fake dll bytes",
		"share/extension/vector.control":    "default_version = '0.8.6'\n",
		"share/extension/vector--0.8.6.sql": "-- sql",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	m := NewManager(t.TempDir(), func(Progress) {})
	s := Source{Kind: KindPgvector, Version: "0.8.6-pg18", InstallTo: root}
	if err := m.InstallFromFile(s, zipPath); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(root, markerFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != "18.3" {
		t.Fatalf("postgres marker overwritten: got %q, want %q", got, "18.3")
	}
	if _, err := os.Stat(filepath.Join(root, "lib", "vector.dll")); err != nil {
		t.Fatal("vector.dll not extracted into postgres tree")
	}
	if !m.IsInstalled(s) {
		t.Fatal("pgvector should report installed after InstallFromFile")
	}
}

func TestIsInstalledManagedRuntimeRejectsUnknownExecutableVersion(t *testing.T) {
	for _, kind := range []Kind{KindPHP, KindNodejs} {
		for _, marker := range []bool{false, true} {
			name := string(kind) + " without marker"
			if marker {
				name = string(kind) + " with matching marker"
			}
			t.Run(name, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "8.3.30")
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, sentinels[kind]), []byte("unverified executable"), 0600); err != nil {
					t.Fatal(err)
				}
				if marker {
					if err := writeMarker(root, "8.3.30"); err != nil {
						t.Fatal(err)
					}
				}
				manager := NewManager(t.TempDir(), nil)
				if manager.IsInstalled(Source{Kind: kind, Version: "8.3.30", InstallTo: root}) {
					t.Fatal("unknown runtime version was accepted from the marker or directory name")
				}
			})
		}
	}
}

func TestBackfillMarkersDoesNotInventManagedRuntimeVersions(t *testing.T) {
	for _, kind := range []Kind{KindPHP, KindNodejs} {
		t.Run(string(kind), func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, sentinels[kind]), []byte("unknown runtime"), 0600); err != nil {
				t.Fatal(err)
			}
			BackfillMarkers([]Source{{Kind: kind, Version: "8.3.30", InstallTo: root}})
			if marker, err := os.ReadFile(filepath.Join(root, markerFile)); !os.IsNotExist(err) {
				t.Fatalf("backfill invented marker %q: %v", marker, err)
			}
		})
	}
}

func TestBackfillMarkersPreservesOtherKinds(t *testing.T) {
	for _, kind := range []Kind{KindNginx, KindPython} {
		t.Run(string(kind), func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, sentinels[kind]), []byte("legacy binary"), 0600); err != nil {
				t.Fatal(err)
			}
			source := Source{Kind: kind, Version: "1.2.3", InstallTo: root}
			BackfillMarkers([]Source{source})
			manager := NewManager(t.TempDir(), nil)
			if !manager.IsInstalled(source) {
				t.Fatal("legacy nonruntime marker stopped reporting installed")
			}
			if err := writeMarker(root, "1.2.2"); err != nil {
				t.Fatal(err)
			}
			BackfillMarkers([]Source{source})
			marker, err := os.ReadFile(filepath.Join(root, markerFile))
			if err != nil || string(marker) != "1.2.2" {
				t.Fatalf("existing marker changed: %q %v", marker, err)
			}
		})
	}
}

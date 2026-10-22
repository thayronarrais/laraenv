//go:build windows

package installer

import (
	"os"
	"path/filepath"
	"testing"

	"lara4ever/internal/runtimeinventory"
)

// A Windows version-resource fixture exercises the public inventory reader
// without launching an executable or replacing its global cache with a mock.
func copyRuntimeVersionFixture(t *testing.T, kind Kind) (string, string) {
	t.Helper()
	fixture := filepath.Join(os.Getenv("SystemRoot"), "System32", "kernel32.dll")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	executable := filepath.Join(root, sentinels[kind])
	if err := os.WriteFile(executable, data, 0600); err != nil {
		t.Fatal(err)
	}
	metadata, err := runtimeinventory.Read(executable)
	if err != nil || metadata.FullVersion == "" {
		t.Fatalf("version fixture=%+v %v", metadata, err)
	}
	return root, metadata.FullVersion
}

func TestIsInstalledManagedRuntimeUsesActualVersionInsteadOfMarker(t *testing.T) {
	for _, kind := range []Kind{KindPHP, KindNodejs} {
		t.Run(string(kind), func(t *testing.T) {
			root, actual := copyRuntimeVersionFixture(t, kind)
			if err := writeMarker(root, "8.3.30"); err != nil {
				t.Fatal(err)
			}
			manager := NewManager(t.TempDir(), nil)
			if manager.IsInstalled(Source{Kind: kind, Version: "8.3.30", InstallTo: root}) {
				t.Fatal("catalog marker hid a different executable version")
			}
			if !manager.IsInstalled(Source{Kind: kind, Version: "v" + actual, InstallTo: root}) {
				t.Fatalf("real executable version %q was rejected because marker differs", actual)
			}
			if err := os.Remove(filepath.Join(root, markerFile)); err != nil {
				t.Fatal(err)
			}
			if !manager.IsInstalled(Source{Kind: kind, Version: actual, InstallTo: root}) {
				t.Fatal("verified runtime requires an install marker")
			}
		})
	}
}

func TestBackfillMarkersUsesActualRuntimeVersionAndRepairsStaleMarker(t *testing.T) {
	for _, kind := range []Kind{KindPHP, KindNodejs} {
		t.Run(string(kind), func(t *testing.T) {
			root, actual := copyRuntimeVersionFixture(t, kind)
			source := Source{Kind: kind, Version: "8.3.30", InstallTo: root}
			BackfillMarkers([]Source{source})
			marker, err := os.ReadFile(filepath.Join(root, markerFile))
			if err != nil || string(marker) != actual {
				t.Fatalf("marker=%q want executable version %q: %v", marker, actual, err)
			}
			if err := writeMarker(root, source.Version); err != nil {
				t.Fatal(err)
			}
			BackfillMarkers([]Source{source})
			marker, err = os.ReadFile(filepath.Join(root, markerFile))
			if err != nil || string(marker) != actual {
				t.Fatalf("stale marker was not repaired: %q %v", marker, err)
			}
		})
	}
}

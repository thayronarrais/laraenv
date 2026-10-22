package runtimeinventory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInventoryCachesExecutableMetadataUntilFileChanges(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "php.exe")
	if err := os.WriteFile(exe, []byte("old binary"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	inventory := New()
	inventory.readMetadata = func(string) (Metadata, error) {
		calls++
		if calls == 1 {
			return Metadata{FullVersion: "8.4.2"}, nil
		}
		return Metadata{FullVersion: "8.4.20"}, nil
	}
	for i := 0; i < 2; i++ {
		got, err := inventory.Read(exe)
		if err != nil || got.FullVersion != "8.4.2" {
			t.Fatalf("read = %+v, %v", got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("metadata reads = %d; want cached read", calls)
	}
	if err := os.WriteFile(exe, []byte("replacement binary"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := inventory.Read(exe)
	if err != nil || got.FullVersion != "8.4.20" || calls != 2 {
		t.Fatalf("replacement = %+v, %v (%d reads)", got, err, calls)
	}
}

func TestInventoryNeverClaimsCatalogMarkerAsExecutableVersion(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "php.exe")
	if err := os.WriteFile(exe, []byte("binary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lara4ever-version"), []byte("8.4.20"), 0600); err != nil {
		t.Fatal(err)
	}
	inventory := New()
	inventory.readMetadata = func(string) (Metadata, error) { return Metadata{}, errors.New("version resource absent") }
	got, err := inventory.Read(exe)
	if err == nil || got.FullVersion != "" {
		t.Fatalf("unknown metadata = %+v, %v; want explicit unknown", got, err)
	}
}

func TestInventoryDoesNotReturnRemovedExecutableFromCache(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "node.exe")
	if err := os.WriteFile(exe, []byte("binary"), 0600); err != nil {
		t.Fatal(err)
	}
	inventory := New()
	inventory.readMetadata = func(string) (Metadata, error) { return Metadata{FullVersion: "22.22.2"}, nil }
	if _, err := inventory.Read(exe); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}
	if got, err := inventory.Read(exe); err == nil || got.FullVersion != "" {
		t.Fatalf("removed = %+v, %v", got, err)
	}
}

//go:build !windows

package workspacefiles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityRejectsReplacedRootBehindOpenHandle(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "checkout")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(path, path+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if canonical, _, err := directoryIdentity(root, path); err == nil {
		t.Fatalf("old root handle was identified as replacement %q", canonical)
	}
}

func TestIdentityRejectsReplacedFileBehindOpenHandle(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "note.md")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Rename(path, path+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if canonical, err := openedCanonical(f, path); err == nil {
		t.Fatalf("old file handle was identified as replacement %q", canonical)
	}
}

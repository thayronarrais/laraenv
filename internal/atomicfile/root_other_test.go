//go:build !windows

package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRootStageDuplicationFailureReportsRemoval(t *testing.T) {
	root, dir := rootFixture(t)
	native, err := openDirectory(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	stage, err := newRootStage(native, root, ".test-stage", fixtureStagePermissions(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.retained.Close(); err != nil {
		t.Fatal(err)
	}
	duplicateErr := errors.New("duplicate failed")
	got, err := retainRootStage(stage.writer, root, stage.name, func(_ *os.File) (*os.File, error) {
		if err := root.Close(); err != nil {
			t.Fatal(err)
		}
		return nil, duplicateErr
	})
	if got != nil || !errors.Is(err, duplicateErr) || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("lost removal failure: stage=%v err=%v", got, err)
	}
	if _, err := stage.writer.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("writer left open: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, stage.name)); err != nil {
		t.Fatal(err)
	}
	assertRootContents(t, dir, "original")
}

func TestReplaceInRootParentSwap(t *testing.T) {
	base := t.TempDir()
	checkout := filepath.Join(base, "checkout")
	parent := filepath.Join(checkout, "docs")
	outside := filepath.Join(base, "outside")
	moved := filepath.Join(checkout, "moved")
	for _, dir := range []string{parent, outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(checkout)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	called := false
	err = ReplaceInRoot(root, "docs/note.md", []byte("new"), func(_ *os.Root, _ string) error {
		called = true
		if err := os.Rename(parent, moved); err != nil {
			return err
		}
		return os.Symlink(outside, parent)
	})
	if err == nil || !called {
		t.Fatalf("accepted swapped parent: %v callback=%v", err, called)
	}
	assertRootContents(t, moved, "original")
	assertRootContents(t, outside, "original")
}

func TestReplaceInRootRejectsInternalSymlink(t *testing.T) {
	root, dir := rootFixture(t)
	if err := os.Symlink("note.md", filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceInRoot(root, "link.md", []byte("new"), nil); err == nil {
		t.Fatal("edited through symlink")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "note.md"))
	if string(data) != "original" {
		t.Fatalf("target changed: %q", data)
	}
}

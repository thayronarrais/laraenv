package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func rootFixture(t *testing.T) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root, dir
}

func fixtureStagePermissions(t *testing.T, root *os.Root) permissionState {
	t.Helper()
	file, err := root.Open("note.md")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	permissions, err := readPermissions(file)
	if err != nil {
		t.Fatal(err)
	}
	return permissions
}
func assertRootContents(t *testing.T, dir, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("data=%q want=%q", data, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "note.md" {
			t.Fatalf("leftover %s", e.Name())
		}
	}
}
func TestReplaceInRootFailureCleanup(t *testing.T) {
	root, dir := rootFixture(t)
	sentinel := errors.New("revision changed")
	err := ReplaceInRoot(root, "note.md", []byte("new"), func(parent *os.Root, leaf string) error {
		if leaf != "note.md" {
			t.Fatalf("leaf=%q", leaf)
		}
		data, e := parent.ReadFile(leaf)
		if e != nil || string(data) != "original" {
			t.Fatalf("pinned read=%q %v", data, e)
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v", err)
	}
	assertRootContents(t, dir, "original")
}
func TestReplaceInRootTargetChanges(t *testing.T) {
	for _, action := range []string{"remove", "replace", "permissions"} {
		t.Run(action, func(t *testing.T) {
			root, dir := rootFixture(t)
			err := ReplaceInRoot(root, "note.md", []byte("new"), func(parent *os.Root, leaf string) error {
				switch action {
				case "remove":
					return parent.Remove(leaf)
				case "replace":
					if err := parent.Remove(leaf); err != nil {
						return err
					}
					return parent.WriteFile(leaf, []byte("replacement"), 0600)
				default:
					return parent.Chmod(leaf, 0400)
				}
			})
			t.Cleanup(func() { os.Chmod(filepath.Join(dir, "note.md"), 0600) })
			if err == nil {
				t.Fatal("accepted changed target")
			}
			entries, _ := os.ReadDir(dir)
			for _, entry := range entries {
				if entry.Name() != "note.md" {
					t.Fatalf("leftover %s", entry.Name())
				}
			}
			if action != "remove" {
				want := "original"
				if action == "replace" {
					want = "replacement"
				}
				assertRootContents(t, dir, want)
			}
		})
	}
}
func TestReplaceInRootRejectsMissingAndInvalid(t *testing.T) {
	root, dir := rootFixture(t)
	for _, path := range []string{"missing.md", "../note.md", "/note.md", "note.md:stream", "./note.md"} {
		if err := ReplaceInRoot(root, path, []byte("new"), nil); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	assertRootContents(t, dir, "original")
}

// These wrappers fault only one OS lifecycle boundary; all file creation,
// permission operations, writing, cleanup and byte assertions remain real.
type failingStage struct {
	stagedFile
	operation string
	failure   error
}

func (s *failingStage) preserve(p permissionState) error {
	if s.operation == "preserve" {
		return s.failure
	}
	return s.stagedFile.preserve(p)
}
func (s *failingStage) verify(p permissionState) error {
	if s.operation == "verify" {
		return s.failure
	}
	return s.stagedFile.verify(p)
}
func (s *failingStage) Write(p []byte) (int, error) {
	if s.operation == "write" {
		n, _ := s.stagedFile.Write(p[:1])
		return n, s.failure
	}
	return s.stagedFile.Write(p)
}
func (s *failingStage) Sync() error {
	if s.operation == "sync" {
		return s.failure
	}
	return s.stagedFile.Sync()
}
func (s *failingStage) Close() error {
	err := s.stagedFile.Close()
	if s.operation == "close" {
		return errors.Join(err, s.failure)
	}
	return err
}
func (s *failingStage) applyMode(p permissionState) error {
	if s.operation == "mode" {
		return s.failure
	}
	return s.stagedFile.applyMode(p)
}

func TestReplaceInRootStageFailures(t *testing.T) {
	for _, operation := range []string{"preserve", "verify", "write", "sync", "close", "mode"} {
		t.Run(operation, func(t *testing.T) {
			root, dir := rootFixture(t)
			native, err := openDirectory(root, ".")
			if err != nil {
				t.Fatal(err)
			}
			defer native.Close()
			original, err := openTarget(native, "note.md")
			if err != nil {
				t.Fatal(err)
			}
			defer original.Close()
			permissions, err := readPermissions(original)
			if err != nil {
				t.Fatal(err)
			}
			stage, err := newRootStage(native, root, ".test-stage", permissions)
			if err != nil {
				t.Fatal(err)
			}
			failure := errors.New("injected " + operation + " error")
			err = replaceStaged(root, "note.md", ".test-stage", []byte("new"), permissions, &failingStage{stage, operation, failure}, func() error { return nil })
			if !errors.Is(err, failure) {
				t.Fatalf("error=%v", err)
			}
			assertRootContents(t, dir, "original")
		})
	}
}

func TestRootStageDuplicationFailureReportsCleanupAndClose(t *testing.T) {
	for _, closeParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "close-error", true: "cleanup-and-close-errors"}[closeParent], func(t *testing.T) {
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
			// Reuse the real exclusive-creation writer with its production rights.
			if err := stage.retained.Close(); err != nil {
				t.Fatal(err)
			}
			duplicateErr := errors.New("duplicate failed")
			got, err := retainRootStage(stage.writer, root, stage.name, func(writer *os.File) (*os.File, error) {
				if closeParent {
					if err := root.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				return nil, duplicateErr
			})
			if got != nil || !errors.Is(err, duplicateErr) || !errors.Is(err, os.ErrClosed) {
				t.Fatalf("lost duplication/cleanup/close error: stage=%v err=%v", got, err)
			}
			if closeParent {
				// Both the relative removal and handle fallback are unavailable;
				// report that failure rather than silently hiding the leftover.
				if _, statErr := os.Stat(filepath.Join(dir, stage.name)); statErr != nil {
					t.Fatal(statErr)
				}
				if removeErr := os.Remove(filepath.Join(dir, stage.name)); removeErr != nil {
					t.Fatal(removeErr)
				}
			}
			assertRootContents(t, dir, "original")
		})
	}
}

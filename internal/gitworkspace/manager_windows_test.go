//go:build windows

package gitworkspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
	"lara4ever/internal/i18n"
)

func windowsDirectoryAliases(t *testing.T, path string) (string, string) {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	long, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	input, err := windows.UTF16PtrFromString(long)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	n, err := windows.GetShortPathName(input, &buffer[0], uint32(len(buffer)))
	if err != nil || n >= uint32(len(buffer)) {
		t.Fatalf("short directory alias: length=%d error=%v", n, err)
	}
	short := windows.UTF16ToString(buffer[:n])
	if strings.EqualFold(long, short) {
		t.Skip("the test volume does not provide a distinct Windows short path")
	}
	longInfo, err := os.Stat(long)
	if err != nil {
		t.Fatal(err)
	}
	shortInfo, err := os.Stat(short)
	if err != nil || !os.SameFile(longInfo, shortInfo) {
		t.Fatalf("fixture paths must identify the same directory: %v", err)
	}
	return long, short
}

func TestSamePathMatchesWindowsShortDirectoryAlias(t *testing.T) {
	long, short := windowsDirectoryAliases(t, filepath.Join(t.TempDir(), "long directory identity fixture"))
	if !samePath(long, short) {
		t.Fatalf("same directory rejected: long=%q short=%q", long, short)
	}
	if samePath(long, t.TempDir()) {
		t.Fatal("different directories have the same identity")
	}
}

func TestManagerReusesAndRemovesWindowsShortWorktreeAlias(t *testing.T) {
	repo := testRepository(t)
	_, shortRoot := windowsDirectoryAliases(t, filepath.Join(t.TempDir(), "long managed worktree directory"))
	manager := NewManager(shortRoot)
	request := ResolveRequest{Mode: ModeWorktree, Branch: "feature/alias", CreateBranch: true, BaseRef: "main", WorktreeName: "alias"}
	resolved, err := manager.Resolve(context.Background(), repo, request)
	if err != nil {
		t.Fatal(err)
	}
	request.CreateBranch = false
	if _, err := manager.Resolve(context.Background(), repo, request); err != nil {
		t.Fatalf("reuse registered alias: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resolved.Path, "dirty.txt"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Remove(context.Background(), repo, resolved.Path); err == nil {
		t.Fatal("dirty worktree was removed")
	}
	if err := os.Remove(filepath.Join(resolved.Path, "dirty.txt")); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(resolved.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Remove(context.Background(), repo, canonical); err != nil {
		t.Fatalf("remove registered alias: %v", err)
	}
	if _, err := os.Stat(canonical); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree remains: %v", err)
	}
}

func TestManagerRejectsOutsideRegisteredWorktreeJunction(t *testing.T) {
	repo := testRepository(t)
	outside := NewManager(filepath.Join(t.TempDir(), "outside-worktrees"))
	resolved, err := outside.Resolve(context.Background(), repo, ResolveRequest{Mode: ModeWorktree, Branch: "feature/outside", CreateBranch: true, BaseRef: "main", WorktreeName: "outside"})
	if err != nil {
		t.Fatal(err)
	}
	managedRoot := t.TempDir()
	junction := filepath.Join(managedRoot, "outside-junction")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, resolved.Path).CombinedOutput(); err != nil {
		t.Fatalf("junction fixture: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(junction) })
	manager := NewManager(managedRoot)
	manager.run = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if len(args) > 1 && args[0] == "worktree" && args[1] == "remove" {
			t.Fatal("outside worktree reached Git removal")
		}
		return runGit(ctx, dir, args...)
	}
	for _, candidate := range []string{junction, filepath.Join(junction, "not-created-yet")} {
		t.Run(filepath.Base(candidate), func(t *testing.T) {
			var containment *i18n.Error
			if err := ensureContained(managedRoot, candidate); !errors.As(err, &containment) || containment.Key != "errors.gitworkspace.worktreeDestinationEscapesRoot" {
				t.Fatalf("outside junction must fail physical containment: %v", err)
			}
		})
	}
	err = manager.Remove(context.Background(), repo, junction)
	var localized *i18n.Error
	if !errors.As(err, &localized) || localized.Key != "errors.gitworkspace.worktreeDestinationEscapesRoot" {
		t.Fatalf("outside junction must fail containment: %v", err)
	}
	if _, err := os.Stat(resolved.Path); err != nil {
		t.Fatalf("outside worktree changed: %v", err)
	}
}

func TestManagerRefusesMainCheckoutThroughWindowsAlias(t *testing.T) {
	repo := testRepository(t)
	long, short := windowsDirectoryAliases(t, repo)
	manager := NewManager(filepath.Dir(long))
	err := manager.Remove(context.Background(), repo, short)
	var localized *i18n.Error
	if !errors.As(err, &localized) || localized.Key != "errors.gitworkspace.mainCheckoutCannotBeRemoved" {
		t.Fatalf("main checkout alias must remain protected: %v", err)
	}
	if _, err := os.Stat(repo); err != nil {
		t.Fatalf("main checkout changed: %v", err)
	}
}

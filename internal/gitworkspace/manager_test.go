package gitworkspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManagerCurrentModeDoesNotMutateCheckout(t *testing.T) {
	repo := testRepository(t)
	manager := NewManager(filepath.Join(t.TempDir(), "worktrees"))
	before := gitOutput(t, repo, "status", "--porcelain=v1", "--branch")
	resolved, err := manager.Resolve(context.Background(), repo, ResolveRequest{Mode: ModeCurrent})
	if err != nil {
		t.Fatal(err)
	}
	after := gitOutput(t, repo, "status", "--porcelain=v1", "--branch")
	if before != after || resolved.Path != repo || resolved.Branch != "main" {
		t.Fatalf("current mode mutated checkout or resolved incorrectly: %#v", resolved)
	}
}

func TestManagerRefusesDirtyBranchSwitch(t *testing.T) {
	repo := testRepository(t)
	gitRun(t, repo, "branch", "feature")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("dirty"), 0644); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(filepath.Join(t.TempDir(), "worktrees"))
	if _, err := manager.Resolve(context.Background(), repo, ResolveRequest{Mode: ModeBranch, Branch: "feature"}); err == nil {
		t.Fatal("dirty branch switch unexpectedly succeeded")
	}
	if branch := gitOutput(t, repo, "branch", "--show-current"); branch != "main" {
		t.Fatalf("branch changed to %q", branch)
	}
}

func TestManagerSwitchesExistingBranch(t *testing.T) {
	repo := testRepository(t)
	gitRun(t, repo, "branch", "feature")
	manager := NewManager(filepath.Join(t.TempDir(), "worktrees"))
	resolved, err := manager.Resolve(context.Background(), repo, ResolveRequest{Mode: ModeBranch, Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Branch != "feature" || gitOutput(t, repo, "branch", "--show-current") != "feature" {
		t.Fatalf("existing branch was not selected: %#v", resolved)
	}
}

func TestManagerKeepsSlashNamedLocalBranchLocal(t *testing.T) {
	repo := testRepository(t)
	gitRun(t, repo, "branch", "feature/nested")
	manager := NewManager(filepath.Join(t.TempDir(), "worktrees"))
	info := manager.Inspect(context.Background(), repo)
	for _, branch := range info.Branches {
		if branch.Name == "feature/nested" {
			if branch.Remote {
				t.Fatal("slash-named local branch was marked remote")
			}
			return
		}
	}
	t.Fatal("slash-named local branch missing from inspection")
}

func TestManagerCreatesContainedWorktreeAndLeavesMainCheckoutAlone(t *testing.T) {
	repo := testRepository(t)
	managedRoot := filepath.Join(t.TempDir(), "managed-worktrees")
	manager := NewManager(managedRoot)
	resolved, err := manager.Resolve(context.Background(), repo, ResolveRequest{
		Mode: ModeWorktree, Branch: "feature/new-ui", CreateBranch: true,
		BaseRef: "main", WorktreeName: "new-ui",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureContained(managedRoot, resolved.Path); err != nil {
		t.Fatal(err)
	}
	if resolved.WorktreePath == "" || gitOutput(t, resolved.Path, "branch", "--show-current") != "feature/new-ui" {
		t.Fatalf("worktree was not created correctly: %#v", resolved)
	}
	if gitOutput(t, repo, "branch", "--show-current") != "main" {
		t.Fatal("main checkout branch changed while creating worktree")
	}
}

func TestManagerRejectsEscapingWorktreeName(t *testing.T) {
	repo := testRepository(t)
	manager := NewManager(filepath.Join(t.TempDir(), "worktrees"))
	if _, err := manager.Resolve(context.Background(), repo, ResolveRequest{
		Mode: ModeWorktree, Branch: "feature", CreateBranch: true, BaseRef: "main", WorktreeName: "../outside",
	}); err == nil {
		t.Fatal("escaping worktree name unexpectedly succeeded")
	}
}

func TestManagerRemovesOnlyCleanManagedWorktree(t *testing.T) {
	repo := testRepository(t)
	managedRoot := filepath.Join(t.TempDir(), "managed-worktrees")
	manager := NewManager(managedRoot)
	resolved, err := manager.Resolve(context.Background(), repo, ResolveRequest{
		Mode: ModeWorktree, Branch: "feature/remove", CreateBranch: true, BaseRef: "main", WorktreeName: "remove",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resolved.Path, "dirty.txt"), []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := manager.Remove(context.Background(), repo, resolved.Path); err == nil {
		t.Fatal("dirty worktree was removed")
	}
	if _, err := os.Stat(resolved.Path); err != nil {
		t.Fatalf("dirty worktree was not preserved: %v", err)
	}
	if err := os.Remove(filepath.Join(resolved.Path, "dirty.txt")); err != nil {
		t.Fatal(err)
	}
	if err := manager.Remove(context.Background(), repo, resolved.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(resolved.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clean worktree still exists: %v", err)
	}
}

func testRepository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "init", "-b", "main")
	gitRun(t, repo, "config", "user.email", "tests@example.invalid")
	gitRun(t, repo, "config", "user.name", "LaraEnv Tests")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("demo"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-m", "initial")
	return repo
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(bytesTrimSpace(output))
}

func bytesTrimSpace(data []byte) []byte {
	start, end := 0, len(data)
	for start < end && (data[start] == ' ' || data[start] == '\n' || data[start] == '\r' || data[start] == '\t') {
		start++
	}
	for end > start && (data[end-1] == ' ' || data[end-1] == '\n' || data[end-1] == '\r' || data[end-1] == '\t') {
		end--
	}
	return data[start:end]
}

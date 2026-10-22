package sourcecontrol

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Catches traversing nested repositories or Git administrative directories.
func TestDiffNestedAndPrivatePaths(t *testing.T) {
	s, source, dir := fixture(t)
	if err := os.Mkdir(filepath.Join(dir, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, dir, "child/file.txt", "base\n")
	gitFixture(t, dir, "add", "child/file.txt")
	gitFixture(t, dir, "commit", "-m", "child")
	writeFixture(t, dir, "child/file.txt", "nested\n")
	gitFixture(t, filepath.Join(dir, "child"), "init")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	got := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, "child/file.txt", "worktree")))
	if got.Kind != "unsupported" || got.CanOpenWorktree || got.Modified.Text != "" || got.Message == "" {
		t.Fatalf("nested repository traversed: %#v", got)
	}
}

// Catches following symlink leaves/ancestors, even to another directory inside the root.
func TestDiffLinkedPaths(t *testing.T) {
	s, source, dir := fixture(t)
	if err := os.Mkdir(filepath.Join(dir, "parent"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, dir, "parent/file.txt", "base\n")
	writeFixture(t, dir, "target/file.txt", "secret\n")
	gitFixture(t, dir, "add", "parent/file.txt")
	gitFixture(t, dir, "commit", "-m", "parent")
	if err := os.Remove(filepath.Join(dir, "parent/file.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "parent")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "parent")); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		// Directory junctions exercise the Windows reparse-point boundary without
		// requiring the account's optional symbolic-link privilege.
		cmd := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(dir, "parent"), filepath.Join(dir, "target"))
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("temporary junction: %s: %v", output, err)
		}
	}
	snap := snapshot(t, s.Inspect(context.Background(), source))
	for _, path := range []string{"parent/file.txt"} {
		got := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, path, "worktree")))
		if got.Kind != "unsupported" || got.CanOpenWorktree || got.Modified.Text != "" {
			t.Fatalf("followed link %q: %#v", path, got)
		}
	}
	// Git links are also blocked when materialized as regular files by Windows
	// core.symlinks=false. Never treat the stored link target as document text.
	writeFixture(t, dir, "link.txt", "target/file.txt")
	gitFixture(t, dir, "add", "link.txt")
	oid := strings.TrimSpace(string(gitFixture(t, dir, "rev-parse", ":link.txt")))
	gitFixture(t, dir, "update-index", "--cacheinfo", "120000,"+oid+",link.txt")
	snap = snapshot(t, s.Inspect(context.Background(), source))
	got := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, "link.txt", "index")))
	if got.Kind != "unsupported" || got.CanOpenWorktree || got.Modified.Text != "" {
		t.Fatalf("Git link treated as file: %#v", got)
	}
}

func TestDiffGitlinkSummary(t *testing.T) {
	s, source, dir := fixture(t)
	head := strings.TrimSpace(string(gitFixture(t, dir, "rev-parse", "HEAD")))
	gitFixture(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+head+",module")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	got := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, "module", "index")))
	if got.Kind != "gitlink" || got.CanOpenWorktree || got.Message == "" {
		t.Fatalf("gitlink treated as text: %#v", got)
	}
}

// Catches returning a comparison after the index changes during blob reads.
func TestDiffIndexChangesDuringRead(t *testing.T) {
	s, source, dir := fixture(t)
	writeFixture(t, dir, "tracked.txt", "staged\n")
	gitFixture(t, dir, "add", "tracked.txt")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	native := s.run
	changed := false
	s.run = func(ctx context.Context, dir string, args []string, in []byte, limit int64, emit func(string, string)) CommandResult {
		r := native(ctx, dir, args, in, limit, emit)
		if !changed {
			for _, arg := range args {
				if arg == "cat-file" {
					changed = true
					writeFixture(t, dir, "tracked.txt", "later\n")
					gitFixture(t, dir, "add", "tracked.txt")
					break
				}
			}
		}
		return r
	}
	if got := s.ReadDiff(context.Background(), diffRequest(t, snap, "tracked.txt", "index")); got.Code != "stale_snapshot" || got.Diff != nil {
		t.Fatalf("index race accepted: %#v", got)
	}
}

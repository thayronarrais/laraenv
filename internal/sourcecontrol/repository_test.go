package sourcecontrol

import (
	"bytes"
	"context"
	"lara4ever/internal/workspacefiles"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitFixture(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return out
}
func fixture(t *testing.T) (*Service, workspacefiles.ContextRef, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "init", "-b", "main")
	gitFixture(t, dir, "config", "user.name", "Fixture")
	gitFixture(t, dir, "config", "user.email", "fixture@example.invalid")
	writeFixture(t, dir, "tracked.txt", "base\n")
	gitFixture(t, dir, "add", ".")
	gitFixture(t, dir, "commit", "-m", "base")
	ref := workspacefiles.ContextRef{Kind: "agent", SessionID: "A"}
	ws := workspacefiles.NewService(func(r workspacefiles.ContextRef) (string, string, error) {
		if r != ref {
			return "", "", os.ErrNotExist
		}
		return dir, "A", nil
	})
	return NewService(ws.OpenContext, nil), ref, dir
}
func writeFixture(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func snapshot(t *testing.T, r Result) *Snapshot {
	t.Helper()
	if r.Code != "ok" || r.Snapshot == nil {
		t.Fatalf("result %#v", r)
	}
	return r.Snapshot
}

func TestRepositoryUsesSessionWorktree(t *testing.T) {
	_, _, dir := fixture(t)
	b := filepath.Join(t.TempDir(), "B")
	gitFixture(t, dir, "worktree", "add", "-b", "agent-b", b)
	ws := workspacefiles.NewService(func(r workspacefiles.ContextRef) (string, string, error) {
		if r.SessionID == "A" {
			return dir, "A", nil
		}
		if r.SessionID == "B" {
			return b, "B", nil
		}
		return "", "", os.ErrNotExist
	})
	s := NewService(ws.OpenContext, nil)
	a := snapshot(t, s.Inspect(context.Background(), workspacefiles.ContextRef{Kind: "agent", SessionID: "A"}))
	bb := snapshot(t, s.Inspect(context.Background(), workspacefiles.ContextRef{Kind: "agent", SessionID: "B"}))
	if a.Repository.RootKey == bb.Repository.RootKey || a.Repository.RepositoryKey == bb.Repository.RepositoryKey || bb.Branch != "agent-b" {
		t.Fatalf("worktrees not isolated: %#v %#v", a, bb)
	}
	t.Setenv("GIT_DIR", filepath.Join(dir, ".git"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(dir, ".git", "index"))
	t.Setenv("GIT_WORK_TREE", dir)
	if got := snapshot(t, s.Status(context.Background(), bb.Repository)); got.Branch != "agent-b" {
		t.Fatalf("poison redirected: %#v", got)
	}
	wrong := a.Repository
	wrong.Context = bb.Repository.Context
	if r := s.Status(context.Background(), wrong); r.Code != "repository_changed" {
		t.Fatalf("cross-context authority accepted: %#v", r)
	}
}

func TestRepositoryRequiresCompleteKeys(t *testing.T) {
	s, source, _ := fixture(t)
	for _, ref := range []RepositoryRef{{Context: source}, {Context: source, RootKey: "123", RepositoryKey: "456"}, {Context: source, RootKey: strings.Repeat("a", 64)}} {
		if r := s.Status(context.Background(), ref); r.Code != "invalid_path" {
			t.Fatalf("invalid reference accepted: %#v", r)
		}
	}
}
func TestRepositoryRejectsReplacement(t *testing.T) {
	s, ref, dir := fixture(t)
	old := snapshot(t, s.Inspect(context.Background(), ref))
	if err := os.Rename(dir, dir+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "init")
	if r := s.Status(context.Background(), old.Repository); r.Code != "repository_changed" {
		t.Fatalf("%#v", r)
	}
}
func TestRepositoryGitMarkerChanged(t *testing.T) {
	s, ref, dir := fixture(t)
	old := snapshot(t, s.Inspect(context.Background(), ref))
	if err := os.Rename(filepath.Join(dir, ".git"), filepath.Join(dir, "old-git")); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "init")
	if r := s.Status(context.Background(), old.Repository); r.Code != "repository_changed" {
		t.Fatalf("%#v", r)
	}
}
func TestRepositoryIndexReplacementKeepsKey(t *testing.T) {
	s, ref, dir := fixture(t)
	old := snapshot(t, s.Inspect(context.Background(), ref))
	index := filepath.Join(dir, ".git", "index")
	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(index, index+".old"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(index, data, 0600); err != nil {
		t.Fatal(err)
	}
	got := snapshot(t, s.Status(context.Background(), old.Repository))
	if got.Repository.RepositoryKey != old.Repository.RepositoryKey || got.IndexRevision != old.IndexRevision {
		t.Fatal("index replacement changed identity")
	}
}
func TestRepositoryRejectsAncestor(t *testing.T) {
	_, _, dir := fixture(t)
	nested := filepath.Join(dir, "child")
	os.Mkdir(nested, 0700)
	ws := workspacefiles.NewService(func(workspacefiles.ContextRef) (string, string, error) { return nested, "child", nil })
	s := NewService(ws.OpenContext, nil)
	if r := s.Inspect(context.Background(), workspacefiles.ContextRef{Kind: "project", ProjectName: "child"}); r.Code != "unavailable" {
		t.Fatalf("%#v", r)
	}
}
func TestStatusReadDoesNotRewriteIndex(t *testing.T) {
	s, ref, dir := fixture(t)
	index := filepath.Join(dir, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	stat, _ := os.Stat(index)
	writeFixture(t, dir, "tracked.txt", "changed\n")
	a := snapshot(t, s.Inspect(context.Background(), ref))
	b := snapshot(t, s.Status(context.Background(), a.Repository))
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	now, _ := os.Stat(index)
	if !bytes.Equal(before, after) || !stat.ModTime().Equal(now.ModTime()) {
		t.Fatal("read rewrote index")
	}
	if a.SnapshotRevision != b.SnapshotRevision {
		t.Fatal("unchanged snapshot unstable")
	}
}
func TestStatusV2NamesRenamesAndMM(t *testing.T) {
	s, ref, dir := fixture(t)
	writeFixture(t, dir, "tracked.txt", "staged\n")
	gitFixture(t, dir, "add", "tracked.txt")
	writeFixture(t, dir, "tracked.txt", "disk\n")
	writeFixture(t, dir, "space name.txt", "new")
	a := snapshot(t, s.Inspect(context.Background(), ref))
	var mm bool
	for _, e := range a.Entries {
		if e.RelativePath == "tracked.txt" {
			mm = e.IndexCode == "M" && e.WorktreeCode == "M" && e.CanStage && e.CanUnstage
		}
	}
	if !mm {
		t.Fatalf("missing MM: %#v", a.Entries)
	}
	oid := "0123456789012345678901234567890123456789"
	raw := []byte("# future.header value\x002 R. N... 100644 100644 100644 " + oid + " " + oid + " R100 new\tname\n.txt\x00old name.txt\x00? bad\xff.txt\x00")
	parsed, err := parseStatus(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Entries) != 2 || parsed.Entries[0].RelativePath != "new\tname\n.txt" || parsed.Entries[0].OriginalPath != "old name.txt" {
		t.Fatalf("names lost: %#v", parsed)
	}
	bad := parsed.Entries[1]
	if bad.RelativePath != "" || bad.CanStage || bad.Kind != "blocked" || bad.DisplayPath != "\"bad\\xff.txt\"" {
		t.Fatalf("lossy path exposed: %#v", bad)
	}
}
func TestStatusMalformedAndOverflow(t *testing.T) {
	for _, raw := range []string{"? missing-nul", "1 M. N... broken\x00", "2 R. N... 100644 100644 100644 a b R100 name\x00", "garbage\x00", "? ../escape\x00"} {
		if _, err := parseStatus([]byte(raw)); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	s, ref, _ := fixture(t)
	native := s.run
	s.run = func(ctx context.Context, dir string, args []string, in []byte, limit int64, emit func(string, string)) CommandResult {
		for _, arg := range args {
			if arg == "status" {
				return CommandResult{Overflow: true}
			}
		}
		return native(ctx, dir, args, in, limit, emit)
	}
	if r := s.Inspect(context.Background(), ref); r.Code != "too_large" {
		t.Fatalf("overflow accepted: %#v", r)
	}
}

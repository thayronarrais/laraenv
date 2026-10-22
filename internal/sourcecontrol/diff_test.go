package sourcecontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/workspacefiles"
)

func diffRequest(t *testing.T, snap *Snapshot, path, layer string) DiffRequest {
	t.Helper()
	for _, entry := range snap.Entries {
		if entry.RelativePath == path {
			return DiffRequest{Repository: snap.Repository, Expected: Expectation{SnapshotRevision: snap.SnapshotRevision, IndexRevision: snap.IndexRevision, HeadOID: snap.HeadOID, Branch: snap.Branch}, EntryID: entry.ID, Layer: layer}
		}
	}
	t.Fatalf("missing status entry %q: %#v", path, snap.Entries)
	return DiffRequest{}
}

func comparison(t *testing.T, r Result) *Diff {
	t.Helper()
	if r.Code != "ok" || r.Diff == nil {
		t.Fatalf("comparison result: %#v", r)
	}
	return r.Diff
}

// Catches swapping HEAD/index/disk or using mutable revision expressions.
func TestDiffStagedAndUnstagedMM(t *testing.T) {
	s, source, dir := fixture(t)
	writeFixture(t, dir, "tracked.txt", "staged\n")
	gitFixture(t, dir, "add", "tracked.txt")
	writeFixture(t, dir, "tracked.txt", "\ufeffdisk\r\n")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	index := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, "tracked.txt", "index")))
	work := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, "tracked.txt", "worktree")))
	sum := sha256.Sum256([]byte("\ufeffdisk\r\n"))
	if index.Kind != "text" || index.Original.Text != "base\n" || index.Modified.Text != "staged\n" || index.Original.OID == "" || index.Modified.OID == "" {
		t.Fatalf("wrong staged sides: %#v", index)
	}
	if work.Kind != "text" || work.Original.Text != "staged\n" || work.Modified.Text != "disk\n" || work.Modified.OID != "" || work.Modified.Revision != hex.EncodeToString(sum[:]) || !work.Modified.BOM || work.Modified.EOL != "crlf" || work.Modified.Size != 9 || !work.CanOpenWorktree {
		t.Fatalf("wrong disk side: %#v", work)
	}
	if index.ID == work.ID || index.Repository != snap.Repository || work.RelativePath != "tracked.txt" {
		t.Fatal("comparison identity lost repository or layer")
	}
}

// Status tokens deliberately omit disk bytes. Explicit reads must still return
// a new comparison while leaving the previously returned comparison immutable.
func TestDiffSameStatusFreshDiskReads(t *testing.T) {
	for _, name := range []string{"tracked.txt", "untracked.txt"} {
		t.Run(name, func(t *testing.T) {
			s, source, dir := fixture(t)
			writeFixture(t, dir, name, "first\n")
			initial := snapshot(t, s.Inspect(context.Background(), source))
			request := diffRequest(t, initial, name, "worktree")
			first := comparison(t, s.ReadDiff(context.Background(), request))
			writeFixture(t, dir, name, "later\n")
			current := snapshot(t, s.Status(context.Background(), initial.Repository))
			if current.SnapshotRevision != initial.SnapshotRevision || current.IndexRevision != initial.IndexRevision || current.HeadOID != initial.HeadOID {
				t.Fatal("fixture did not preserve the status/HEAD/index tokens")
			}
			second := comparison(t, s.ReadDiff(context.Background(), request))
			if first.Modified.Text != "first\n" || second.Modified.Text != "later\n" || first.ID == second.ID || first.Modified.Revision == second.Modified.Revision {
				t.Fatalf("disk comparison did not refresh independently: first=%#v second=%#v", first, second)
			}
			identical := comparison(t, s.ReadDiff(context.Background(), request))
			if identical.ID != second.ID || identical.Modified.Revision != second.Modified.Revision {
				t.Fatal("identical disk comparison cannot be reused")
			}
		})
	}
}

// Catches treating legitimate missing versions as errors or reading HEAD on unborn branches.
func TestDiffUnbornAndDeleted(t *testing.T) {
	s, source, dir := fixture(t)
	gitFixture(t, dir, "checkout", "--orphan", "new")
	gitFixture(t, dir, "rm", "-f", "tracked.txt")
	writeFixture(t, dir, "new.txt", "new\n")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	newFile := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, "new.txt", "worktree")))
	if newFile.Original.Text != "" || newFile.Modified.Text != "new\n" || newFile.Kind != "text" {
		t.Fatalf("untracked: %#v", newFile)
	}
	gitFixture(t, dir, "add", "new.txt")
	snap = snapshot(t, s.Inspect(context.Background(), source))
	added := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, "new.txt", "index")))
	if added.Original.Text != "" || added.Modified.Text != "new\n" || added.Kind != "text" {
		t.Fatalf("unborn index: %#v", added)
	}
	gitFixture(t, dir, "commit", "-m", "new")
	if err := os.Remove(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
	snap = snapshot(t, s.Inspect(context.Background(), source))
	deleted := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, "new.txt", "worktree")))
	if deleted.Original.Text != "new\n" || deleted.Modified.Text != "" || deleted.CanOpenWorktree || deleted.Kind != "text" {
		t.Fatalf("disk delete: %#v", deleted)
	}
	gitFixture(t, dir, "add", "-u")
	snap = snapshot(t, s.Inspect(context.Background(), source))
	deleted = comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, "new.txt", "index")))
	if deleted.Original.Text != "new\n" || deleted.Modified.Text != "" || deleted.Kind != "text" {
		t.Fatalf("index delete: %#v", deleted)
	}
}

// Catches false empty text diffs and off-by-one size limits on either side.
func TestDiffBinaryOversizeAndUTF8(t *testing.T) {
	s, source, dir := fixture(t)
	for _, tc := range []struct{ name, data, kind string }{
		{"limit", strings.Repeat("x", 2<<20), "text"},
		{"oversize", strings.Repeat("x", (2<<20)+1), "too_large"},
		{"embedded-nul", "a\x00b", "binary"},
		{"invalid", "a\xffb", "binary"},
		{"cr", "a\rb", "unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFixture(t, dir, tc.name+".txt", tc.data)
			snap := snapshot(t, s.Inspect(context.Background(), source))
			work := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, tc.name+".txt", "worktree")))
			if work.Kind != tc.kind || work.Modified.Size != int64(len(tc.data)) {
				t.Fatalf("disk classification: kind=%s size=%d", work.Kind, work.Modified.Size)
			}
			if tc.kind != "text" && (work.Message == "" || work.Modified.Text != "") {
				t.Fatal("summary lacks explanation or leaks text")
			}
			gitFixture(t, dir, "add", "--", tc.name+".txt")
			snap = snapshot(t, s.Inspect(context.Background(), source))
			index := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, tc.name+".txt", "index")))
			if index.Kind != tc.kind || index.Modified.Size != int64(len(tc.data)) {
				t.Fatalf("blob classification: kind=%s size=%d", index.Kind, index.Modified.Size)
			}
		})
	}
}

// Catches executing configured external diff or textconv when fetching blobs.
func TestDiffDoesNotRunTextconv(t *testing.T) {
	s, source, dir := fixture(t)
	sentinel := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join(t.TempDir(), "convert.sh")
	text := "#!/bin/sh\nprintf ran > '" + filepath.ToSlash(sentinel) + "'\ncat \"$1\"\n"
	if err := os.WriteFile(script, []byte(text), 0700); err != nil {
		t.Fatal(err)
	}
	command := "sh '" + filepath.ToSlash(script) + "'"
	gitFixture(t, dir, "config", "diff.external", command)
	gitFixture(t, dir, "config", "diff.fixture.textconv", command)
	writeFixture(t, dir, ".gitattributes", "tracked.txt diff=fixture\n")
	writeFixture(t, dir, "tracked.txt", "staged\n")
	gitFixture(t, dir, "add", "tracked.txt")
	writeFixture(t, dir, "tracked.txt", "disk\n")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	for _, layer := range []string{"index", "worktree"} {
		if got := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, "tracked.txt", layer))); got.Kind != "text" {
			t.Fatalf("%#v", got)
		}
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("converter executed: %v", err)
	}
	// Positive controls prove both configured sentinels can actually execute.
	gitFixture(t, dir, "diff", "--", "tracked.txt")
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("external diff sentinel was not runnable: %v", err)
	}
	if err := os.Remove(sentinel); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "diff", "--cached", "--no-ext-diff", "--textconv", "--", "tracked.txt")
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("textconv sentinel was not runnable: %v", err)
	}
}

// Catches stale expectations, forged entry membership, or retargeting contexts/roots.
func TestDiffStaleAndRootReplacement(t *testing.T) {
	s, source, dir := fixture(t)
	writeFixture(t, dir, "tracked.txt", "disk\n")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	req := diffRequest(t, snap, "tracked.txt", "worktree")
	for _, modify := range []func(*DiffRequest){
		func(r *DiffRequest) { r.EntryID = strings.Repeat("a", 64) },
		func(r *DiffRequest) { r.Expected.IndexRevision = strings.Repeat("a", 64) },
		func(r *DiffRequest) { r.Expected.SnapshotRevision = "" },
		func(r *DiffRequest) { r.Expected.HeadOID = strings.Repeat("a", 40) },
		func(r *DiffRequest) { r.Expected.Branch = "another" },
	} {
		bad := req
		modify(&bad)
		if got := s.ReadDiff(context.Background(), bad); got.Code != "stale_snapshot" || got.Diff != nil {
			t.Fatalf("stale request accepted: %#v", got)
		}
	}
	bad := req
	bad.Layer = "HEAD:tracked.txt"
	if got := s.ReadDiff(context.Background(), bad); got.Code != "invalid_path" {
		t.Fatalf("rev layer accepted: %#v", got)
	}
	gitFixture(t, dir, "add", "tracked.txt")
	if got := s.ReadDiff(context.Background(), req); got.Code != "stale_snapshot" {
		t.Fatalf("changed index accepted: %#v", got)
	}
	if err := os.Rename(dir, dir+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "init")
	if got := s.ReadDiff(context.Background(), req); got.Code != "repository_changed" {
		t.Fatalf("root replacement accepted: %#v", got)
	}
}

func TestDiffCachedComparisonCannotRetarget(t *testing.T) {
	_, _, dir := fixture(t)
	b := filepath.Join(t.TempDir(), "B")
	gitFixture(t, dir, "worktree", "add", "-b", "b", b)
	ws := workspacefiles.NewService(func(ref workspacefiles.ContextRef) (string, string, error) {
		if ref.SessionID == "A" {
			return dir, "A", nil
		}
		if ref.SessionID == "B" {
			return b, "B", nil
		}
		return "", "", os.ErrNotExist
	})
	s := NewService(ws.OpenContext, nil)
	writeFixture(t, dir, "tracked.txt", "A\n")
	writeFixture(t, b, "tracked.txt", "B\n")
	snap := snapshot(t, s.Inspect(context.Background(), workspacefiles.ContextRef{Kind: "agent", SessionID: "A"}))
	req := diffRequest(t, snap, "tracked.txt", "worktree")
	diff := comparison(t, s.ReadDiff(context.Background(), req))
	req.Repository.Context.SessionID = "B"
	if got := s.ReadDiff(context.Background(), req); got.Code != "repository_changed" {
		t.Fatalf("retargeted: %#v", got)
	}
	if diff.Modified.Text != "A\n" || diff.Repository.Context.SessionID != "A" {
		t.Fatal("returned comparison retargeted")
	}
}

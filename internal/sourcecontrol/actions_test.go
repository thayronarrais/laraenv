package sourcecontrol

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func filesRequest(t *testing.T, snap *Snapshot, paths ...string) FilesRequest {
	t.Helper()
	req := FilesRequest{Repository: snap.Repository, Expected: Expectation{SnapshotRevision: snap.SnapshotRevision, IndexRevision: snap.IndexRevision, HeadOID: snap.HeadOID, Branch: snap.Branch}}
	for _, path := range paths {
		d := diffRequest(t, snap, path, "worktree")
		req.Entries = append(req.Entries, EntrySelection{EntryID: d.EntryID})
	}
	return req
}
func awaitOperation(t *testing.T, s *Service, r Result) *Operation {
	t.Helper()
	if r.Code != "ok" || r.Operation == nil {
		t.Fatalf("start: %#v", r)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		got := s.GetOperation(r.Operation.ID)
		if got.Code != "ok" {
			t.Fatalf("get: %#v", got)
		}
		if got.Operation.FinishedAt != "" {
			return got.Operation
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("operation did not finish")
	return nil
}
func succeeded(t *testing.T, op *Operation) {
	t.Helper()
	if op.State != "succeeded" || op.ResultCode != "ok" || op.AfterSnapshot == nil {
		t.Fatalf("operation: %#v", op)
	}
}

// Catches broad staging, dropping a rename endpoint and worktree writes on unstage.
func TestStageExactPathsRenameAndDeletion(t *testing.T) {
	s, source, dir := fixture(t)
	writeFixture(t, dir, "delete.txt", "delete\n")
	gitFixture(t, dir, "add", "delete.txt")
	gitFixture(t, dir, "commit", "-m", "deletion fixture")
	gitFixture(t, dir, "mv", "tracked.txt", "renamed.txt")
	writeFixture(t, dir, "renamed.txt", "new disk\n")
	writeFixture(t, dir, "other.txt", "leave alone\n")
	if err := os.Remove(filepath.Join(dir, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, s.Inspect(context.Background(), source))
	op := awaitOperation(t, s, s.StartStage(context.Background(), filesRequest(t, snap, "renamed.txt", "delete.txt")))
	succeeded(t, op)
	if !op.MutationObserved {
		t.Fatal("mutation was not observed")
	}
	if got := string(gitFixture(t, dir, "show", ":renamed.txt")); got != "new disk\n" {
		t.Fatalf("index %q", got)
	}
	if got := string(gitFixture(t, dir, "ls-files", "-z")); got != "renamed.txt\x00" {
		t.Fatalf("unselected paths staged: %q", got)
	}
	snap = snapshot(t, s.Status(context.Background(), snap.Repository))
	paths := []string{}
	for _, e := range snap.Entries {
		if e.CanUnstage {
			paths = append(paths, e.RelativePath)
		}
	}
	succeeded(t, awaitOperation(t, s, s.StartUnstage(context.Background(), filesRequest(t, snap, paths...))))
	if got := string(gitFixture(t, dir, "diff", "--cached", "--name-only")); got != "" {
		t.Fatalf("rename endpoints remain: %q", got)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "renamed.txt")); err != nil || string(got) != "new disk\n" {
		t.Fatalf("disk modified: %q %v", got, err)
	}
}

// Catches interpreting filenames as pathspec patterns or permitting nested/directory/ignored paths.
func TestStageLiteralNamesAndNestedRepo(t *testing.T) {
	s, source, dir := fixture(t)
	for _, name := range []string{"-dash.txt", "[abc].txt", "a.txt", "space name.txt"} {
		writeFixture(t, dir, name, name)
	}
	writeFixture(t, dir, ".gitignore", "ignored.txt\n")
	writeFixture(t, dir, "ignored.txt", "ignored")
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, dir, "nested/inside.txt", "old")
	gitFixture(t, dir, "add", "nested/inside.txt")
	gitFixture(t, dir, "commit", "-m", "nested fixture")
	gitFixture(t, filepath.Join(dir, "nested"), "init")
	writeFixture(t, dir, "nested/inside.txt", "changed")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	succeeded(t, awaitOperation(t, s, s.StartStage(context.Background(), filesRequest(t, snap, "-dash.txt", "[abc].txt", "space name.txt"))))
	if got := string(gitFixture(t, dir, "diff", "--cached", "--name-only", "-z")); got != "-dash.txt\x00[abc].txt\x00space name.txt\x00" {
		t.Fatalf("not literal: %q", got)
	}
	for _, path := range []string{"nested/inside.txt", "nested", "ignored.txt"} {
		snap = snapshot(t, s.Status(context.Background(), snap.Repository))
		req := FilesRequest{Repository: snap.Repository, Expected: filesRequest(t, snap).Expected, Entries: []EntrySelection{{EntryID: digest(path, "")}}}
		r := s.StartStage(context.Background(), req)
		if r.Code == "ok" {
			op := awaitOperation(t, s, r)
			if op.State == "succeeded" {
				t.Fatalf("unsafe selection accepted %q", path)
			}
		}
	}
	if got := string(gitFixture(t, dir, "show", ":nested/inside.txt")); got != "old" {
		t.Fatalf("nested file staged: %q", got)
	}
}

// Catches rm without --cached/-f on unborn branches and removing unselected index entries.
func TestUnstageUnbornKeepsNewerDisk(t *testing.T) {
	s, source, dir := fixture(t)
	gitFixture(t, dir, "checkout", "--orphan", "unborn")
	gitFixture(t, dir, "rm", "-f", "tracked.txt")
	writeFixture(t, dir, "new.txt", "stage\n")
	writeFixture(t, dir, "keep.txt", "keep\n")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	succeeded(t, awaitOperation(t, s, s.StartStage(context.Background(), filesRequest(t, snap, "new.txt", "keep.txt"))))
	writeFixture(t, dir, "new.txt", "newer disk\n")
	snap = snapshot(t, s.Status(context.Background(), snap.Repository))
	succeeded(t, awaitOperation(t, s, s.StartUnstage(context.Background(), filesRequest(t, snap, "new.txt"))))
	if got, err := os.ReadFile(filepath.Join(dir, "new.txt")); err != nil || string(got) != "newer disk\n" {
		t.Fatalf("disk changed: %q %v", got, err)
	}
	if got := string(gitFixture(t, dir, "ls-files", "-z")); got != "keep.txt\x00" {
		t.Fatalf("wrong index: %q", got)
	}
}

// Catches stale review dispatch, ignoring optional raw-byte disk revision and deleting external locks.
func TestStageExpectationDiskRevisionAndLock(t *testing.T) {
	s, source, dir := fixture(t)
	writeFixture(t, dir, "tracked.txt", "reviewed\n")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	req := filesRequest(t, snap, "tracked.txt")
	d := comparison(t, s.ReadDiff(context.Background(), diffRequest(t, snap, "tracked.txt", "worktree")))
	req.Entries[0].DiskRevision = d.Modified.Revision
	writeFixture(t, dir, "tracked.txt", "different\n")
	op := awaitOperation(t, s, s.StartStage(context.Background(), req))
	if op.ResultCode != "stale_snapshot" {
		t.Fatalf("disk review ignored: %#v", op)
	}
	snap = snapshot(t, s.Status(context.Background(), snap.Repository))
	req = filesRequest(t, snap, "tracked.txt")
	req.Expected.IndexRevision = "wrong"
	op = awaitOperation(t, s, s.StartStage(context.Background(), req))
	if op.ResultCode != "stale_snapshot" {
		t.Fatalf("expectation ignored: %#v", op)
	}
	writeFixture(t, dir, ".git/index.lock", "external lock")
	req = filesRequest(t, snap, "tracked.txt")
	op = awaitOperation(t, s, s.StartStage(context.Background(), req))
	if op.State != "failed" || op.ResultCode != "git_error" || !op.HasExitCode {
		t.Fatalf("lock ignored: %#v", op)
	}
	if got, err := os.ReadFile(filepath.Join(dir, ".git/index.lock")); err != nil || string(got) != "external lock" {
		t.Fatalf("external lock touched %q %v", got, err)
	}
	if got := string(gitFixture(t, dir, "show", ":tracked.txt")); !strings.Contains(got, "base") {
		t.Fatalf("unexpected mutation %q", got)
	}
}

// Multiple selections of the same entry must not bypass any supplied review.
func TestStageChecksEveryDiskReview(t *testing.T) {
	s, source, dir := fixture(t)
	writeFixture(t, dir, "tracked.txt", "disk\n")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	req := filesRequest(t, snap, "tracked.txt")
	req.Entries = append(req.Entries, EntrySelection{EntryID: req.Entries[0].EntryID, DiskRevision: strings.Repeat("a", 64)})
	op := awaitOperation(t, s, s.StartStage(context.Background(), req))
	if op.State != "failed" || op.ResultCode != "stale_snapshot" {
		t.Fatalf("review bypassed: %#v", op)
	}
	if got := string(gitFixture(t, dir, "show", ":tracked.txt")); got != "base\n" {
		t.Fatalf("index changed: %q", got)
	}
}

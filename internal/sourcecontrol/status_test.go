package sourcecontrol

import (
	"context"
	"fmt"
	"lara4ever/internal/workspacefiles"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusRemoteDestinations(t *testing.T) {
	s, ref, dir := fixture(t)
	gitFixture(t, dir, "remote", "add", "origin", "https://alice:secret@example.invalid/repo.git")
	a := snapshot(t, s.Inspect(context.Background(), ref))
	if len(a.Remotes) != 1 || !a.Remotes[0].CanPush || strings.Contains(a.Remotes[0].DisplayURL, "secret") {
		t.Fatalf("%#v", a.Remotes)
	}
	oldID := a.Remotes[0].ID
	gitFixture(t, dir, "config", "remote.origin.mirror", "yes")
	b := snapshot(t, s.Status(context.Background(), a.Repository))
	if b.Remotes[0].CanPush || oldID == b.Remotes[0].ID {
		t.Fatalf("mirror not disabled: %#v", b.Remotes)
	}
	gitFixture(t, dir, "config", "--unset", "remote.origin.mirror")
	gitFixture(t, dir, "config", "--add", "remote.origin.pushurl", "https://example.invalid/a")
	gitFixture(t, dir, "config", "--add", "remote.origin.pushurl", "https://example.invalid/b")
	c := snapshot(t, s.Status(context.Background(), a.Repository))
	if c.Remotes[0].CanPush || c.Remotes[0].DisabledReason == "" {
		t.Fatalf("multiple destinations enabled: %#v", c.Remotes)
	}
}

func TestStatusRemoteMirrorGitBooleans(t *testing.T) {
	s, ref, dir := fixture(t)
	gitFixture(t, dir, "remote", "add", "origin", "https://example.invalid/repo.git")
	repository := snapshot(t, s.Inspect(context.Background(), ref)).Repository
	for _, tc := range []struct {
		value   string
		canPush bool
	}{
		{"2", false}, {"-2", false}, {"42", false}, {"-1", false},
		{"0", true}, {"true", false}, {"yes", false}, {"on", false}, {"1", false},
		{"TRUE", false}, {"false", true}, {"no", true}, {"off", true}, {"", true},
	} {
		t.Run("value="+tc.value, func(t *testing.T) {
			gitFixture(t, dir, "config", "--replace-all", "remote.origin.mirror", tc.value)
			got := snapshot(t, s.Status(context.Background(), repository))
			if len(got.Remotes) != 1 || got.Remotes[0].CanPush != tc.canPush {
				t.Fatalf("mirror=%q: remotes=%#v, want canPush=%v", tc.value, got.Remotes, tc.canPush)
			}
			if !tc.canPush && got.Remotes[0].DisabledReason == "" {
				t.Fatal("mirror remote has no disabled reason")
			}
		})
	}
	// Git distinguishes a bare key (true) from an explicit empty value (false).
	gitFixture(t, dir, "config", "--unset-all", "remote.origin.mirror")
	configPath := filepath.Join(dir, ".git", "config")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(config, []byte("\n[remote \"origin\"]\n\tmirror\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, s.Status(context.Background(), repository)); got.Remotes[0].CanPush {
		t.Fatal("bare mirror key was enabled for push")
	}
	gitFixture(t, dir, "config", "--replace-all", "remote.origin.mirror", "not-a-boolean")
	if got := s.Status(context.Background(), repository); got.Code != "git_error" || got.Snapshot != nil {
		t.Fatalf("invalid mirror config did not fail closed: %#v", got)
	}
}
func TestStatusRejectsMalformedIndex(t *testing.T) {
	s, ref, _ := fixture(t)
	native := s.run
	s.run = func(ctx context.Context, dir string, args []string, in []byte, limit int64, emit func(string, string)) CommandResult {
		for _, arg := range args {
			if arg == "ls-files" {
				return CommandResult{Stdout: []byte("broken\x00"), HasExitCode: true}
			}
		}
		return native(ctx, dir, args, in, limit, emit)
	}
	if r := s.Inspect(context.Background(), ref); r.Code != "unsupported_format" {
		t.Fatalf("malformed index accepted: %#v", r)
	}
}
func TestStatusConflictsAndUnborn(t *testing.T) {
	oid := strings.Repeat("a", 40)
	p, err := parseStatus([]byte("# branch.oid (initial)\x00# branch.head main\x00# branch.ab +3 -2\x00u UU N... 100644 100644 100644 100644 " + oid + " " + oid + " " + oid + " conflict.txt\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Unborn || p.HeadOID != "" || p.Branch != "main" || !p.HasConflicts || p.Ahead != 3 || p.Behind != 2 || !p.CountsKnown || p.Entries[0].CanStage {
		t.Fatalf("%#v", p)
	}
	s, ref, dir := fixture(t)
	writeFixture(t, filepath.Join(dir, ".git"), "MERGE_HEAD", oid+"\n")
	if p := snapshot(t, s.Inspect(context.Background(), ref)); p.GitOperation != "merge" {
		t.Fatalf("%#v", p)
	}
}
func TestRepositoryLinkedMarkerReplacement(t *testing.T) {
	_, _, dir := fixture(t)
	b := filepath.Join(t.TempDir(), "B")
	gitFixture(t, dir, "worktree", "add", "-b", "linked", b)
	ws := workspacefiles.NewService(func(workspacefiles.ContextRef) (string, string, error) { return b, "B", nil })
	service := NewService(ws.OpenContext, nil)
	source := workspacefiles.ContextRef{Kind: "project", ProjectName: "B"}
	old := snapshot(t, service.Inspect(context.Background(), source))
	marker := filepath.Join(b, ".git")
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(marker, marker+".old"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(marker, data, 0600); err != nil {
		t.Fatal(err)
	}
	if r := service.Status(context.Background(), old.Repository); r.Code != "repository_changed" {
		t.Fatalf("%#v", r)
	}
}

func TestStatusRejectsInvalidRenameAndConflict(t *testing.T) {
	oid := strings.Repeat("a", 40)
	for _, raw := range []string{
		"2 R. N... 100644 100644 100644 " + oid + " " + oid + " R100 name\x00\x00",
		"u MM N... 100644 100644 100644 100644 " + oid + " " + oid + " " + oid + " name\x00",
	} {
		if _, err := parseStatus([]byte(raw)); err == nil {
			t.Errorf("accepted malformed record %q", raw)
		}
	}
}

func TestStatusUnknownHeaderAndBlockedFilename(t *testing.T) {
	p, err := parseStatus([]byte("# future-header\x00? file:name.txt\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Entries) != 1 || p.Entries[0].Kind != "blocked" || p.Entries[0].RelativePath != "" || p.Entries[0].CanStage {
		t.Fatalf("unsafe action path: %#v", p)
	}
}

func TestStatusStructuredResponseLimit(t *testing.T) {
	s, ref, _ := fixture(t)
	var raw strings.Builder
	for i := 0; i < 60000; i++ {
		fmt.Fprintf(&raw, "? item-%06d.txt\x00", i)
	}
	native := s.run
	s.run = func(ctx context.Context, dir string, args []string, in []byte, limit int64, emit func(string, string)) CommandResult {
		for _, arg := range args {
			if arg == "status" {
				return CommandResult{Stdout: []byte(raw.String()), HasExitCode: true}
			}
		}
		return native(ctx, dir, args, in, limit, emit)
	}
	if r := s.Inspect(context.Background(), ref); r.Code != "too_large" {
		t.Fatalf("oversized structured result accepted: code=%s", r.Code)
	}
}

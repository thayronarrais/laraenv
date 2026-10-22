package sourcecontrol

import (
	"context"
	"lara4ever/internal/workspacefiles"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type commitPushService interface {
	StartCommit(context.Context, CommitRequest) Result
	StartPush(context.Context, PushRequest) Result
	TryBeginShutdown() (func(), error)
	Close()
}

func commitAPI(t *testing.T, s *Service) commitPushService {
	t.Helper()
	api, ok := any(s).(commitPushService)
	if !ok {
		t.Fatal("commit/push/shutdown service contract is missing")
	}
	return api
}
func commitRequest(t *testing.T, snap *Snapshot) CommitRequest {
	return CommitRequest{Repository: snap.Repository, Expected: filesRequest(t, snap).Expected, Message: "reviewed commit\n\nbody"}
}
func stageCommitFixture(t *testing.T, s *Service, dir string) {
	t.Helper()
	writeFixture(t, dir, "tracked.txt", "indexed\n")
	gitFixture(t, dir, "add", "tracked.txt")
}
func pushRequest(t *testing.T, snap *Snapshot) PushRequest {
	t.Helper()
	if len(snap.Remotes) != 1 {
		t.Fatal(snap.Remotes)
	}
	return PushRequest{Repository: snap.Repository, Expected: filesRequest(t, snap).Expected, RemoteID: snap.Remotes[0].ID, RemoteBranch: "refs/heads/reviewed", SetUpstream: true}
}
func bareRemote(t *testing.T, dir string) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitFixture(t, dir, "init", "--bare", remote)
	gitFixture(t, dir, "remote", "add", "origin", remote)
	return remote
}

// A commit must consume the reviewed shared index, keeping newer disk bytes.
func TestCommitReviewedIndexRetainsLaterDisk(t *testing.T) {
	s, source, dir := fixture(t)
	api := commitAPI(t, s)
	defer api.Close()
	stageCommitFixture(t, s, dir)
	writeFixture(t, dir, "tracked.txt", "later disk\n")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	op := awaitOperation(t, s, api.StartCommit(context.Background(), commitRequest(t, snap)))
	succeeded(t, op)
	if !op.MutationObserved || op.CommitOID == "" || op.CommitOID == snap.HeadOID {
		t.Fatalf("missing commit observation: %#v", op)
	}
	if got := string(gitFixture(t, dir, "show", "HEAD:tracked.txt")); got != "indexed\n" {
		t.Fatal(got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "tracked.txt")); string(got) != "later disk\n" {
		t.Fatal(string(got))
	}
}

// External index entries are included; normal hooks and signing remain authoritative.
func TestCommitExternalStageAndHooks(t *testing.T) {
	for _, mode := range []string{"external", "hook", "signing", "stale", "empty", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			s, source, dir := fixture(t)
			api := commitAPI(t, s)
			defer api.Close()
			stageCommitFixture(t, s, dir)
			writeFixture(t, dir, "external.txt", "external\n")
			gitFixture(t, dir, "add", "external.txt")
			snap := snapshot(t, s.Inspect(context.Background(), source))
			req := commitRequest(t, snap)
			switch mode {
			case "hook":
				if err := os.WriteFile(filepath.Join(dir, ".git/hooks/pre-commit"), []byte("#!/bin/sh\necho hook-refusal >&2\nexit 1\n"), 0700); err != nil {
					t.Fatal(err)
				}
			case "signing":
				gitFixture(t, dir, "config", "commit.gpgsign", "true")
				gitFixture(t, dir, "config", "gpg.program", filepath.Join(dir, "missing-signer"))
			case "stale":
				writeFixture(t, dir, "external.txt", "new index\n")
				gitFixture(t, dir, "add", "external.txt")
			case "empty":
				req.Message = " \n\t"
			case "conflict":
				writeFixture(t, dir, ".git/MERGE_HEAD", snap.HeadOID+"\n")
				snap = snapshot(t, s.Inspect(context.Background(), source))
				req = commitRequest(t, snap)
			}
			result := api.StartCommit(context.Background(), req)
			var op *Operation
			if result.Code == "ok" {
				op = awaitOperation(t, s, result)
			}
			if mode == "external" {
				succeeded(t, op)
				if got := string(gitFixture(t, dir, "show", "HEAD:external.txt")); got != "external\n" {
					t.Fatal(got)
				}
				return
			}
			if result.Code == "ok" && op.State == "succeeded" {
				t.Fatalf("%s unexpectedly committed", mode)
			}
			if got := strings.TrimSpace(string(gitFixture(t, dir, "rev-parse", "HEAD"))); got != snap.HeadOID {
				t.Fatal("failed commit changed HEAD")
			}
			if got := string(gitFixture(t, dir, "show", ":tracked.txt")); got != "indexed\n" {
				t.Fatal("failed commit lost index")
			}
		})
	}
}

// Configured broad refspecs and followTags cannot extend an explicit push.
func TestPushSingleBranchAndUpstream(t *testing.T) {
	s, source, dir := fixture(t)
	api := commitAPI(t, s)
	defer api.Close()
	remote := bareRemote(t, dir)
	gitFixture(t, dir, "branch", "extra")
	gitFixture(t, dir, "tag", "-a", "release", "-m", "tag")
	gitFixture(t, dir, "config", "push.followTags", "true")
	gitFixture(t, dir, "config", "remote.origin.push", "+refs/heads/*:refs/heads/*")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	op := awaitOperation(t, s, api.StartPush(context.Background(), pushRequest(t, snap)))
	succeeded(t, op)
	if got := string(gitFixture(t, remote, "for-each-ref", "--format=%(refname)")); strings.TrimSpace(got) != "refs/heads/reviewed" {
		t.Fatal(got)
	}
	if got := strings.TrimSpace(string(gitFixture(t, dir, "config", "branch.main.remote"))); got != "origin" {
		t.Fatal(got)
	}
	if got := strings.TrimSpace(string(gitFixture(t, dir, "config", "branch.main.merge"))); got != "refs/heads/reviewed" {
		t.Fatal(got)
	}
	if op.Remote != "origin" || op.RemoteBranch != "refs/heads/reviewed" {
		t.Fatalf("target missing: %#v", op)
	}
}
func TestPushRejectsChangedRemoteDetachedAndForce(t *testing.T) {
	for _, mode := range []string{"remote", "detached", "force", "deletion", "wildcard", "tag", "colon", "multiple", "mirror", "non-fast-forward"} {
		t.Run(mode, func(t *testing.T) {
			s, source, dir := fixture(t)
			api := commitAPI(t, s)
			defer api.Close()
			remote := bareRemote(t, dir)
			if mode == "non-fast-forward" {
				gitFixture(t, dir, "push", "origin", "main:reviewed")
				stageCommitFixture(t, s, dir)
				gitFixture(t, dir, "commit", "-m", "remote ahead")
				gitFixture(t, dir, "push", "origin", "main:reviewed")
				gitFixture(t, dir, "checkout", "-b", "behind", "HEAD~1")
			}
			if mode == "detached" {
				gitFixture(t, dir, "checkout", "--detach")
			}
			if mode == "multiple" {
				gitFixture(t, dir, "remote", "set-url", "--add", "--push", "origin", remote)
				gitFixture(t, dir, "remote", "set-url", "--add", "--push", "origin", remote+"-other")
			}
			if mode == "mirror" {
				gitFixture(t, dir, "config", "remote.origin.mirror", "true")
			}
			snap := snapshot(t, s.Inspect(context.Background(), source))
			req := pushRequest(t, snap)
			if mode == "remote" {
				gitFixture(t, dir, "remote", "set-url", "origin", remote+"-changed")
			}
			switch mode {
			case "deletion":
				req.RemoteBranch = ":refs/heads/reviewed"
			case "wildcard":
				req.RemoteBranch = "refs/heads/*"
			case "tag":
				req.RemoteBranch = "refs/tags/reviewed"
			case "colon":
				req.RemoteBranch = "refs/heads/reviewed:refs/heads/extra"
			}
			if mode == "force" {
				req.RemoteBranch = "+refs/heads/reviewed"
			}
			before := string(gitFixture(t, remote, "for-each-ref", "--format=%(objectname) %(refname)"))
			defer func() {
				if got := string(gitFixture(t, remote, "for-each-ref", "--format=%(objectname) %(refname)")); got != before {
					t.Fatal("rejected push changed remote", got)
				}
			}()
			r := api.StartPush(context.Background(), req)
			if r.Code == "ok" {
				op := awaitOperation(t, s, r)
				if op.State == "succeeded" {
					t.Fatalf("unsafe push accepted: %s", mode)
				}
			}
			if got := string(gitFixture(t, remote, "for-each-ref", "--format=%(objectname) %(refname)")); mode != "non-fast-forward" && got != "" {
				t.Fatal(got)
			}
		})
	}
}

// The actual effect can precede cancellation; never claim it was rolled back.
func TestCancelAfterCommitAndPushEffects(t *testing.T) {
	for _, kind := range []string{"commit", "push"} {
		t.Run(kind, func(t *testing.T) {
			s, source, dir := fixture(t)
			api := commitAPI(t, s)
			defer api.Close()
			if kind == "commit" {
				stageCommitFixture(t, s, dir)
			} else {
				bareRemote(t, dir)
			}
			snap := snapshot(t, s.Inspect(context.Background(), source))
			native := s.run
			effect, release := make(chan struct{}), make(chan struct{})
			s.run = func(ctx context.Context, dir string, args []string, in []byte, limit int64, emit func(string, string)) CommandResult {
				r := native(ctx, dir, args, in, limit, emit)
				if slices.Contains(args, kind) {
					close(effect)
					<-release
					r.Cancelled = true
				}
				return r
			}
			var r Result
			if kind == "commit" {
				r = api.StartCommit(context.Background(), commitRequest(t, snap))
			} else {
				r = api.StartPush(context.Background(), pushRequest(t, snap))
			}
			select {
			case <-effect:
			case <-time.After(10 * time.Second):
				t.Fatal("effect not reached")
			}
			s.CancelOperation(r.Operation.ID)
			close(release)
			op := awaitOperation(t, s, r)
			if kind == "commit" {
				if !op.MutationObserved || op.CommitOID == "" || op.State != "cancelled" {
					t.Fatalf("lost observed commit: %#v", op)
				}
			} else if !op.RemoteUncertain || op.State != "uncertain" {
				t.Fatalf("push certainty fabricated: %#v", op)
			}
		})
	}
}

func TestGitShutdownReservationAndClose(t *testing.T) {
	s, source, dir := fixture(t)
	api := commitAPI(t, s)
	writeFixture(t, dir, "tracked.txt", "changed")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	req := filesRequest(t, snap, "tracked.txt")
	release, err := api.TryBeginShutdown()
	if err != nil {
		t.Fatal(err)
	}
	if r := s.StartStage(context.Background(), req); r.Code != "shutting_down" {
		t.Fatalf("reservation bypassed: %#v", r)
	}
	if _, err := api.TryBeginShutdown(); err == nil {
		t.Fatal("double reservation")
	}
	release()
	release()
	native := s.run
	entered := make(chan struct{})
	drain := make(chan struct{})
	s.run = func(ctx context.Context, dir string, args []string, in []byte, limit int64, emit func(string, string)) CommandResult {
		if slices.Contains(args, "add") {
			close(entered)
			<-ctx.Done()
			<-drain
		}
		return native(ctx, dir, args, in, limit, emit)
	}
	r := s.StartStage(context.Background(), req)
	<-entered
	if _, err := api.TryBeginShutdown(); err == nil {
		t.Fatal("busy shutdown admitted")
	}
	done := make(chan struct{})
	go func() { api.Close(); close(done) }()
	select {
	case <-done:
		t.Fatal("Close returned before drain")
	case <-time.After(50 * time.Millisecond):
	}
	close(drain)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not drain")
	}
	awaitOperation(t, s, r)
	release()
	if _, err := api.TryBeginShutdown(); err == nil {
		t.Fatal("closed service reopened")
	}
	if r := s.StartStage(context.Background(), req); r.Code != "shutting_down" {
		t.Fatal(r)
	}
}

// Discovery has no reservation: a shutdown that wins admission must still block it.
func TestGitShutdownWinsInflightDiscovery(t *testing.T) {
	s, source, dir := fixture(t)
	api := commitAPI(t, s)
	defer api.Close()
	writeFixture(t, dir, "tracked.txt", "disk")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	open := s.open
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.open = func(ref workspacefiles.ContextRef, key string) (*os.Root, *workspacefiles.RootInfo, workspacefiles.Result) {
		once.Do(func() { close(entered); <-resume })
		return open(ref, key)
	}
	done := make(chan Result, 1)
	go func() { done <- s.StartStage(context.Background(), filesRequest(t, snap, "tracked.txt")) }()
	<-entered
	release, err := api.TryBeginShutdown()
	if err != nil {
		close(resume)
		t.Fatal(err)
	}
	close(resume)
	select {
	case r := <-done:
		if r.Code != "shutting_down" {
			t.Fatal("discovery slipped through reservation", r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("discovery stalled")
	}
	release()
	succeeded(t, awaitOperation(t, s, s.StartStage(context.Background(), filesRequest(t, snap, "tracked.txt"))))
}

func TestPushNestedBranchWithoutUpstream(t *testing.T) {
	s, source, dir := fixture(t)
	api := commitAPI(t, s)
	defer api.Close()
	remote := bareRemote(t, dir)
	snap := snapshot(t, s.Inspect(context.Background(), source))
	req := pushRequest(t, snap)
	req.SetUpstream = false
	req.RemoteBranch = "refs/heads/topic/nested"
	succeeded(t, awaitOperation(t, s, api.StartPush(context.Background(), req)))
	if got := strings.TrimSpace(string(gitFixture(t, remote, "for-each-ref", "--format=%(refname)"))); got != "refs/heads/topic/nested" {
		t.Fatal(got)
	}
	if snap := snapshot(t, s.Status(context.Background(), snap.Repository)); snap.Upstream != "" {
		t.Fatal("unrequested upstream configured", snap.Upstream)
	}
}

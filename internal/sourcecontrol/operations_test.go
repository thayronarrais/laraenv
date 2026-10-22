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

// Catches request aliasing, missing registration before events, per-context rather than common-dir locks,
// caller cancellation killing a job, and exposing registry-owned mutable result slices.
func TestOperationEarlyEventAndSharedBusy(t *testing.T) {
	_, _, dir := fixture(t)
	other := filepath.Join(t.TempDir(), "linked")
	gitFixture(t, dir, "worktree", "add", "-b", "linked", other)
	ws := workspacefiles.NewService(func(r workspacefiles.ContextRef) (string, string, error) {
		if r.SessionID == "B" {
			return other, "B", nil
		}
		return dir, "A", nil
	})
	var s *Service
	var mu sync.Mutex
	var events []OperationEvent
	s = NewService(ws.OpenContext, func(e OperationEvent) {
		if r := s.GetOperation(e.OperationID); r.Code != "ok" {
			t.Errorf("event precedes registration: %#v", r)
		}
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	writeFixture(t, dir, "tracked.txt", "changed")
	writeFixture(t, other, "tracked.txt", "other")
	a := snapshot(t, s.Inspect(context.Background(), workspacefiles.ContextRef{Kind: "agent", SessionID: "A"}))
	b := snapshot(t, s.Inspect(context.Background(), workspacefiles.ContextRef{Kind: "agent", SessionID: "B"}))
	entered, release := make(chan struct{}), make(chan struct{})
	native := s.run
	s.run = func(ctx context.Context, dir string, args []string, in []byte, limit int64, emit func(string, string)) CommandResult {
		if slices.Contains(args, "add") {
			close(entered)
			<-release
		}
		return native(ctx, dir, args, in, limit, emit)
	}
	ctx, cancel := context.WithCancel(context.Background())
	req := filesRequest(t, a, "tracked.txt")
	r := s.StartStage(ctx, req)
	cancel()
	req.Entries[0].EntryID = "forged"
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("mutation never dispatched")
	}
	if busy := s.Busy(); !busy.Busy || len(busy.OperationIDs) != 1 || busy.OperationIDs[0] != r.Operation.ID {
		t.Fatalf("busy: %#v", busy)
	}
	if second := s.StartStage(context.Background(), filesRequest(t, b, "tracked.txt")); second.Code != "busy" {
		t.Fatalf("common directory not reserved: %#v", second)
	}
	live := snapshot(t, s.Status(context.Background(), a.Repository))
	if !live.Busy || live.OperationID != r.Operation.ID {
		t.Fatalf("live snapshot hides mutation: %#v", live)
	}
	close(release)
	op := awaitOperation(t, s, r)
	succeeded(t, op)
	if s.Busy().Busy {
		t.Fatal("busy leaked")
	}
	op.AfterSnapshot.Entries = append(op.AfterSnapshot.Entries, Entry{ID: "forged"})
	op.Output = "forged"
	again := s.GetOperation(r.Operation.ID).Operation
	if again.Output == "forged" || slices.ContainsFunc(again.AfterSnapshot.Entries, func(e Entry) bool { return e.ID == "forged" }) {
		t.Fatal("caller mutated retained result")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) < 2 {
		t.Fatalf("missing events: %#v", events)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Sequence <= events[i-1].Sequence {
			t.Fatal("nonmonotonic events")
		}
	}
	if s.GetOperation("missing").Code != "operation_unknown" || s.CancelOperation("missing").Code != "operation_unknown" {
		t.Fatal("unknown operation accepted")
	}
}

// Retention is measured from terminal completion, never from admission. Aging
// records avoids a five-minute wall-clock test while exercising public lookups.
func TestOperationRetentionOnlyTerminal(t *testing.T) {
	s := NewService(nil, nil)
	s.operations["old-running"] = &operationRecord{operation: Operation{ID: "old-running", State: "running", StartedAt: time.Now().Add(-time.Hour).Format(time.RFC3339Nano)}}
	s.operations["recent"] = &operationRecord{operation: Operation{ID: "recent", State: "succeeded"}, finished: time.Now().Add(-4 * time.Minute)}
	s.operations["expired"] = &operationRecord{operation: Operation{ID: "expired", State: "cancelled"}, finished: time.Now().Add(-6 * time.Minute)}
	if s.GetOperation("old-running").Code != "ok" || s.GetOperation("recent").Code != "ok" || s.GetOperation("expired").Code != "operation_unknown" {
		t.Fatal("retention removed active/recent operation or retained expired terminal record")
	}
}

// A real long-lived Git child writes a delayed sentinel if it survives cancellation.
// Recovery is paused at the command boundary to prove Busy lasts through recovery.
func TestOperationCancelDrainsAndRecovers(t *testing.T) {
	s, source, dir := fixture(t)
	writeFixture(t, dir, "tracked.txt", "changed")
	snap := snapshot(t, s.Inspect(context.Background(), source))
	native := s.run
	started, recovering, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var mutation bool
	s.run = func(ctx context.Context, dir string, args []string, in []byte, limit int64, emit func(string, string)) CommandResult {
		if slices.Contains(args, "add") {
			mutation = true
			return native(ctx, dir, []string{"-c", "alias.long=!sh -c '(sleep 3; echo survived > survived.txt) & echo ready; wait'", "long"}, nil, limit, func(stream, chunk string) {
				emit(stream, chunk)
				if strings.Contains(chunk, "ready") {
					close(started)
				}
			})
		}
		if mutation && slices.Contains(args, "status") {
			close(recovering)
			<-release
		}
		return native(ctx, dir, args, in, limit, emit)
	}
	r := s.StartStage(context.Background(), filesRequest(t, snap, "tracked.txt"))
	if r.Operation == nil {
		t.Fatalf("%#v", r)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("child not ready")
	}
	if got := s.CancelOperation(r.Operation.ID); got.Code != "ok" || got.Operation.State != "cancelling" {
		t.Fatalf("cancel: %#v", got)
	}
	select {
	case <-recovering:
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not drain")
	}
	if !s.Busy().Busy {
		t.Fatal("reservation released before recovery")
	}
	close(release)
	op := awaitOperation(t, s, r)
	if op.State != "cancelled" || op.AfterSnapshot == nil || s.Busy().Busy {
		t.Fatalf("recovery: %#v", op)
	}
	time.Sleep(3500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "survived.txt")); !os.IsNotExist(err) {
		t.Fatalf("descendant survived: %v", err)
	}
}

// Catches credentials split between pipe writes and a budget cutoff through userinfo.
func TestRedactChunkedCredentialURL(t *testing.T) {
	var output string
	c := &redactedConsole{remaining: consoleLimit, emit: func(_, chunk string) { output += chunk }}
	stream := &consoleStream{console: c, name: "stderr"}
	for _, chunk := range []string{"fatal https:/", "/alice:sec", "ret@example.invalid/repo ", "done\n"} {
		stream.Write([]byte(chunk))
	}
	stream.flush()
	if strings.Contains(output, "alice") || strings.Contains(output, "secret") || !strings.Contains(output, "done") {
		t.Fatalf("credential leaked: %q", output)
	}
	output = ""
	c = &redactedConsole{remaining: consoleLimit, emit: func(_, chunk string) { output += chunk }}
	stream = &consoleStream{console: c, name: "stderr"}
	stream.Write([]byte(strings.Repeat(" ", consoleLimit-13) + "https://alice:secret@example.invalid/path\n"))
	stream.flush()
	if strings.Contains(output, "alice") || strings.Contains(output, "secret") {
		t.Fatal("credential fragment leaked at truncation")
	}
}

// A real post-index-change hook blocks after Git has written the index. The
// final snapshot must show that partial mutation after killing the hook tree.
func TestOperationCancelNativeHookAfterIndexWrite(t *testing.T) {
	s, source, dir := fixture(t)
	writeFixture(t, dir, "tracked.txt", "staged before hook\n")
	hook := []byte("#!/bin/sh\n(sleep 3; echo survived > hook-survived.txt) &\necho hook-ready\nwait\n")
	if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", "post-index-change"), hook, 0700); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	s.emit = func(e OperationEvent) {
		if strings.Contains(e.Chunk, "hook-ready") {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	}
	snap := snapshot(t, s.Inspect(context.Background(), source))
	r := s.StartStage(context.Background(), filesRequest(t, snap, "tracked.txt"))
	if r.Operation == nil {
		t.Fatalf("%#v", r)
	}
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		s.CancelOperation(r.Operation.ID)
		t.Fatal("native hook did not start")
	}
	s.CancelOperation(r.Operation.ID)
	op := awaitOperation(t, s, r)
	if op.State != "cancelled" || !op.MutationObserved || op.AfterSnapshot == nil {
		t.Fatalf("partial mutation not recovered: %#v", op)
	}
	if got := string(gitFixture(t, dir, "show", ":tracked.txt")); got != "staged before hook\n" {
		t.Fatalf("wrong index %q", got)
	}
	time.Sleep(3500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "hook-survived.txt")); !os.IsNotExist(err) {
		t.Fatalf("native hook child survived: %v", err)
	}
}

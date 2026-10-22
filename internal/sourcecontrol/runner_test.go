package sourcecontrol

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunnerBoundedSeparatedOutput(t *testing.T) {
	_, _, dir := fixture(t)
	r := run(context.Background(), dir, []string{"ls-files", "-z"}, nil, 3, nil)
	if !r.Overflow || len(r.Stdout) != 3 || r.Err != nil || !r.HasExitCode || r.ExitCode != 0 {
		t.Fatalf("%#v", r)
	}
	var console string
	r = run(context.Background(), dir, []string{"remote", "get-url", "https://alice:secret@example.invalid"}, nil, 1024, func(stream, chunk string) { console += stream + ":" + chunk })
	if len(r.Stdout) != 0 || r.Stderr == "" || r.ExitCode == 0 || !r.HasExitCode || strings.Contains(r.Stderr, "secret") || strings.Contains(console, "secret") {
		t.Fatalf("stream or redaction error: %#v %q", r, console)
	}
}

// Catches a background hook/transport escaping cleanup after Git has exited.
func TestRunnerCleansDescendantsAfterParentExit(t *testing.T) {
	_, _, dir := fixture(t)
	r := run(context.Background(), dir, []string{"-c", "alias.background=!sh -c '(sleep 3; echo survived > escaped.txt) >/dev/null 2>&1 & echo ready'", "background"}, nil, 1024, nil)
	if r.Err != nil {
		t.Fatalf("%#v", r)
	}
	time.Sleep(3500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("descendant outlived Git: %v", err)
	}
}

func TestRunnerDrainsTruncatedConsole(t *testing.T) {
	_, _, dir := fixture(t)
	var out strings.Builder
	r := run(context.Background(), dir, []string{"-c", `alias.noisy=!sh -c 'i=0; while [ $i -lt 3000 ]; do printf "https://alice:secret@example.invalid/path diagnostic text\n"; i=$((i+1)); done; echo drained >&2'`, "noisy"}, nil, 1024, func(_, chunk string) { out.WriteString(chunk) })
	if r.Err != nil || !r.ConsoleTruncated || out.Len() > consoleLimit || strings.Contains(out.String(), "secret") || strings.Contains(out.String(), "alice") || !strings.Contains(r.Stderr, "drained") {
		t.Fatalf("console %d, result %#v", out.Len(), r)
	}
}
func TestRunnerCancellationAndTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := run(ctx, t.TempDir(), []string{"status"}, nil, 1024, nil)
	if !r.Cancelled || r.TimedOut {
		t.Fatalf("%#v", r)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	r = run(ctx, t.TempDir(), []string{"status"}, nil, 1024, nil)
	if !r.TimedOut || r.Cancelled {
		t.Fatalf("%#v", r)
	}
}
func TestRunnerRedactsIncompleteCredential(t *testing.T) {
	text := "fatal: https://alice:" + strings.Repeat("secret", 15000)
	if got := redact(text); strings.Contains(got, "secret") || strings.Contains(got, "alice") {
		t.Fatal("unterminated URL credentials exposed")
	}
}

func TestRunnerEmitsWhileRunning(t *testing.T) {
	_, _, dir := fixture(t)
	first := make(chan struct{}, 1)
	done := make(chan CommandResult, 1)
	go func() {
		done <- run(context.Background(), dir, []string{"-c", "alias.progress=!echo first; sleep 1; echo second", "progress"}, nil, 1024, func(stream, chunk string) {
			if stream == "stdout" && strings.Contains(chunk, "first") {
				select {
				case first <- struct{}{}:
				default:
				}
			}
		})
	}()
	select {
	case <-first:
		select {
		case <-done:
			t.Fatal("output was buffered until exit")
		default:
		}
	case result := <-done:
		t.Fatalf("no live output: %#v", result)
	case <-time.After(10 * time.Second):
		t.Fatal("no output")
	}
	select {
	case result := <-done:
		if result.Err != nil {
			t.Fatalf("%#v", result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("command did not finish")
	}
}

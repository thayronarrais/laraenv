package vcruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestCheckContextCancelledDoesNotProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CheckContext(ctx, filepath.Join(t.TempDir(), "missing.exe"), ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context ignored: %v", err)
	}
}

func TestCheckContextCancelsRunningProbe(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "probe-pid")
	t.Setenv("LARAENV_VCRUNTIME_TEST_MARKER", marker)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- CheckContext(ctx, exe, "") }()
	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(marker); err == nil {
			pid, _ = strconv.Atoi(string(data))
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("probe helper did not start")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	probeFinished := false
	t.Cleanup(func() {
		if !probeFinished {
			_ = process.Kill()
		}
		_ = process.Release()
	})
	cancel()
	select {
	case err := <-done:
		probeFinished = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("probe cancellation misclassified: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("probe survived cancellation")
	}
}

package queueworker

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Only this test executable is launched; no PHP, queue jobs or services run.
func TestMain(m *testing.M) {
	if os.Getenv("LARAENV_QUEUE_HELPER") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(m.Run())
}

func workerFixture(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("LARAENV_QUEUE_HELPER", "1")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "artisan"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return dir, exe
}

func killHelper(t *testing.T, pid int) {
	t.Helper()
	t.Cleanup(func() {
		if p, e := os.FindProcess(pid); e == nil {
			_ = p.Kill()
		}
	})
}

func TestConcurrentStartsLaunchOneWorker(t *testing.T) {
	dir, exe := workerFixture(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	m := NewManager(nil, func(string) string { entered <- struct{}{}; <-release; return exe })
	type result struct {
		pid int
		err error
	}
	done := make(chan result, 2)
	start := func() { pid, err := m.Start("demo", dir, nil); done <- result{pid, err} }
	go start()
	<-entered
	go start()
	overlapped := false
	select {
	case <-entered:
		overlapped = true
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	successes := 0
	for i := 0; i < 2; i++ {
		select {
		case got := <-done:
			if got.err == nil {
				successes++
				killHelper(t, got.pid)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("start did not return")
		}
	}
	if successes != 1 || overlapped {
		t.Fatalf("concurrent starts launched %d workers; resolvers overlapped=%v", successes, overlapped)
	}
}

func TestStopWaitsForAnInFlightStart(t *testing.T) {
	dir, _ := workerFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	m := NewManager(nil, func(string) string { close(entered); <-release; return filepath.Join(dir, "missing-php.exe") })
	started := make(chan error, 1)
	go func() { _, err := m.Start("demo", dir, nil); started <- err }()
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- m.Stop("demo") }()
	returnedEarly := false
	select {
	case <-stopped:
		returnedEarly = true
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if err := <-started; err == nil {
		t.Fatal("missing executable launched")
	}
	if !returnedEarly {
		select {
		case err := <-stopped:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("stop did not return")
		}
	}
	if returnedEarly {
		t.Fatal("stop returned before the in-flight launch completed")
	}
}

func TestExitedWorkerDoesNotDeleteItsReplacement(t *testing.T) {
	dir, exe := workerFixture(t)
	events := make(chan struct{}, 4)
	m := NewManager(func(event string, _ ...any) {
		if event == "queueworker:status" {
			events <- struct{}{}
		}
	}, func(string) string { return exe })
	pid, err := m.Start("demo", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	killHelper(t, pid)
	<-events
	m.mu.Lock()
	old := m.procs["demo"]
	replacement := &process{cmd: old.cmd, args: []string{"--replacement"}}
	m.procs["demo"] = replacement
	m.mu.Unlock()
	if err := old.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("exited worker did not notify status")
	}
	m.mu.Lock()
	current := m.procs["demo"]
	m.mu.Unlock()
	if current != replacement {
		t.Fatal("old waiter deleted the replacement worker")
	}
}

func TestStartEmitsStatusAfterReleasingLaunchGate(t *testing.T) {
	dir, exe := workerFixture(t)
	var m *Manager
	var emitted atomic.Bool
	callback := make(chan error, 1)
	m = NewManager(func(event string, _ ...any) {
		if event == "queueworker:status" && !emitted.Swap(true) {
			_, err := m.Start("demo", dir, nil)
			callback <- err
		}
	}, func(string) string { return exe })
	done := make(chan int, 1)
	go func() {
		pid, err := m.Start("demo", dir, nil)
		if err != nil {
			t.Error(err)
		}
		done <- pid
	}()
	select {
	case err := <-callback:
		if err == nil {
			t.Fatal("callback launched a duplicate")
		}
	case <-time.After(5 * time.Second):
		if p := m.Status("demo"); p.Running {
			killHelper(t, p.PID)
		}
		t.Fatal("status callback blocked on the launch gate")
	}
	pid := <-done
	killHelper(t, pid)
}

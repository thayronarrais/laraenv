package devserver

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// The child is this test executable, not npm or any project script.
func TestMain(m *testing.M) {
	if os.Getenv("LARAENV_DEVSERVER_HELPER") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(m.Run())
}

func serverFixture(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("LARAENV_DEVSERVER_HELPER", "1")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"dev":"fixture"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return dir, exe
}
func killServerHelper(t *testing.T, pid int) {
	t.Helper()
	t.Cleanup(func() {
		if p, e := os.FindProcess(pid); e == nil {
			_ = p.Kill()
		}
	})
}

func TestConcurrentStartAndStartInstanceLaunchOneProcess(t *testing.T) {
	dir, exe := serverFixture(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	m := NewManager(nil, func(string, string) (string, string, error) { entered <- struct{}{}; <-release; return exe, "npm", nil }, nil, nil)
	type result struct {
		pid int
		err error
	}
	done := make(chan result, 2)
	go func() { pid, err := m.Start("demo", dir, "", "", 0, ""); done <- result{pid, err} }()
	<-entered
	go func() { pid, err := m.StartInstance("demo", "demo", dir, "", "", 0, ""); done <- result{pid, err} }()
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
				killServerHelper(t, got.pid)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("start did not return")
		}
	}
	if successes != 1 || overlapped {
		t.Fatalf("concurrent calls launched %d dev servers; resolvers overlapped=%v", successes, overlapped)
	}
}

func TestConcurrentInstancesReserveDistinctPorts(t *testing.T) {
	dir, exe := serverFixture(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	m := NewManager(nil, func(string, string) (string, string, error) { entered <- struct{}{}; <-release; return exe, "npm", nil }, nil, nil)
	done := make(chan error, 2)
	go func() {
		pid, err := m.StartInstance("demo", "demo", dir, "", "", 0, "")
		if err == nil {
			killServerHelper(t, pid)
		}
		done <- err
	}()
	<-entered
	go func() {
		pid, err := m.StartInstance("worktree", "demo", dir, "", "", 0, "")
		if err == nil {
			killServerHelper(t, pid)
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("start did not return")
		}
	}
	main, worktree := m.Status("demo"), m.Status("worktree")
	if main.Port == 0 || main.Port == worktree.Port {
		t.Fatalf("instances reused a reserved port: main=%+v worktree=%+v", main, worktree)
	}
}

func TestStopWaitsForPendingDevServerStart(t *testing.T) {
	dir, _ := serverFixture(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	m := NewManager(nil, func(string, string) (string, string, error) {
		close(entered)
		<-release
		return filepath.Join(dir, "missing-npm.exe"), "npm", nil
	}, nil, nil)
	started := make(chan error, 1)
	go func() { _, err := m.Start("demo", dir, "", "", 0, ""); started <- err }()
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
		t.Fatal("stop returned before the pending dev server launch")
	}
}

func TestStartStatusCallbackCanReenterManager(t *testing.T) {
	dir, exe := serverFixture(t)
	var m *Manager
	var emitted atomic.Bool
	callback := make(chan error, 1)
	m = NewManager(func(event string, _ ...any) {
		if event == "devserver:status" && !emitted.Swap(true) {
			_, err := m.Start("demo", dir, "", "", 0, "")
			callback <- err
		}
	}, func(string, string) (string, string, error) { return exe, "npm", nil }, nil, nil)
	done := make(chan int, 1)
	go func() {
		pid, err := m.Start("demo", dir, "", "", 0, "")
		if err != nil {
			t.Error(err)
		}
		done <- pid
	}()
	select {
	case err := <-callback:
		if err == nil {
			t.Fatal("status callback launched a duplicate")
		}
	case <-time.After(5 * time.Second):
		if p := m.Status("demo"); p.Running {
			killServerHelper(t, p.PID)
		}
		t.Fatal("status callback blocked on the launch gate")
	}
	pid := <-done
	killServerHelper(t, pid)
}

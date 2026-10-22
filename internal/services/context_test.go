package services

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStartContextCancelledBehindLifecycleDoesNotInitializeOrSpawn(t *testing.T) {
	m := newTestManager(t)
	marker := filepath.Join(t.TempDir(), "initialized")
	m.RegisterDef(Definition{Name: "queued", Path: filepath.Join(t.TempDir(), "unused.exe"), FirstRunCheck: func() bool { return true }, FirstRunInit: func() error {
		if err := os.WriteFile(marker, []byte("initialized"), 0600); err != nil {
			return err
		}
		return errors.New("initialization should never run")
	}})
	m.lifecycle.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(entered); done <- m.StartContext(ctx, "queued") }()
	<-entered
	cancel()
	m.lifecycle.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued cancellation lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued start did not finish")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("cancelled start initialized the service")
	}
	if got := m.Status("queued"); got != "stopped" {
		t.Fatalf("cancelled start left service %s", got)
	}
}

func TestRestartContextCancelledBehindLifecycleDoesNotStopExistingService(t *testing.T) {
	m := newTestManager(t)
	m.RegisterDef(Definition{Name: "existing"})
	existing := &exec.Cmd{}
	m.procs["existing"] = existing
	m.lifecycle.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.RestartContext(ctx, "existing") }()
	cancel()
	m.lifecycle.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued restart cancellation lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued restart did not finish")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.procs["existing"] != existing || m.stopping["existing"] {
		t.Fatal("cancelled restart changed the existing service")
	}
}

// Stop only the processes owned by this test, never every process with the
// fixture binary's image name.
func cleanupContextService(t *testing.T, m *Manager, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		m.mu.Lock()
		var toKill []*exec.Cmd
		for key, cmd := range m.procs {
			for _, name := range names {
				if logicalName(key) == name {
					m.stopping[key] = true
					if cmd != nil && cmd.Process != nil {
						toKill = append(toKill, cmd)
					}
				}
			}
		}
		m.mu.Unlock()
		for _, cmd := range toKill {
			killTracked(cmd)
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			stopped := true
			for _, name := range names {
				if m.Status(name) != "stopped" {
					stopped = false
				}
			}
			if stopped {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Error("fixture process was not reaped")
	})
}

func contextServiceDefinition(t *testing.T, name string, replicas int) Definition {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Definition{Name: name, Label: name, Path: exe, Args: []string{"-test.run=^$"}, Replicas: replicas, Env: map[string]string{"LARAENV_SERVICES_CONTEXT_HELPER": "1"}}
}

func TestStartContextCancelledDuringReplicasRollsBackOnlyNewServices(t *testing.T) {
	m := newTestManager(t)
	shared := contextServiceDefinition(t, "context-shared", 1)
	m.RegisterDef(shared)
	cleanupContextService(t, m, "context-shared", "context-new", "context-pool")
	if err := m.Start("context-shared"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	sharedProcess := m.procs["context-shared"]
	m.mu.Unlock()
	newDep := shared
	newDep.Name = "context-new"
	m.RegisterDef(newDep)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool := contextServiceDefinition(t, "context-pool", 2)
	pool.Internal = true
	pool.Deps = []string{"context-shared", "context-new"}
	pool.ReplicaArgs = func(i int) []string {
		if i == 1 {
			cancel()
		}
		return []string{"-test.run=^$"}
	}
	m.RegisterDef(pool)
	if err := m.StartContext(ctx, pool.Name); !errors.Is(err, context.Canceled) {
		t.Fatalf("partial start cancellation lost: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.Status("context-new") == "stopped" && m.Status("context-pool") == "stopped" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m.Status("context-new") != "stopped" || m.Status("context-pool") != "stopped" {
		t.Fatal("newly started services survived cancellation")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.procs["context-shared"] != sharedProcess || m.stopping["context-shared"] {
		t.Fatal("cancellation stopped the preexisting shared service")
	}
}

func TestSupervisorRespawnRechecksCancelledOwnershipAfterLifecycleWait(t *testing.T) {
	m := newTestManager(t)
	def := contextServiceDefinition(t, "context-respawn", 1)
	m.RegisterDef(def)
	cleanupContextService(t, m, def.Name)
	m.lifecycle.Lock()
	done := make(chan error, 1)
	entered := make(chan struct{})
	go func() { close(entered); done <- m.respawnProc(def.Name, def, def.Args) }()
	<-entered
	completedEarly := false
	select {
	case <-done:
		completedEarly = true
	case <-time.After(50 * time.Millisecond):
	}
	m.rollbackStartedLocked(map[string]*exec.Cmd{def.Name: nil})
	m.lifecycle.Unlock()
	if !completedEarly {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("supervisor remained blocked")
		}
	}
	if completedEarly || m.Status(def.Name) != "stopped" {
		t.Fatal("supervisor started an owned service after its startup was cancelled")
	}
}

func TestRestartContextCancellationDuringWaitRestoresRunningService(t *testing.T) {
	m := newTestManager(t)
	def := contextServiceDefinition(t, "context-restart", 1)
	m.RegisterDef(def)
	cleanupContextService(t, m, def.Name)
	if err := m.Start(def.Name); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.RestartContext(ctx, def.Name) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.Status(def.Name) == "stopped" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if m.Status(def.Name) != "stopped" {
		t.Fatal("restart never stopped the existing service")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("restart cancellation lost: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("cancelled restart restoration did not finish")
	}
	if m.Status(def.Name) != "running" {
		t.Fatal("cancellation left a preexisting shared service stopped")
	}
}

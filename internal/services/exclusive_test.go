package services

import (
	"strings"
	"testing"
)

// markRunningForTest puts a service into the running state without spawning
// a process: isRunningLocked only checks that the key exists. This lives in
// the test file — the manager gets no test-only method.
//
// newTestManager already exists in manager_test.go (same package), so it is
// reused here rather than redeclared.
func markRunningForTest(t *testing.T, m *Manager, name string) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.procs[name] = nil
}

// Two services in the same exclusive group cannot run at once. The error has
// to name the one already holding the port — "failed to start" sends the user
// looking in the wrong place.
func TestStartRefusesASecondServiceInTheSameExclusiveGroup(t *testing.T) {
	m := newTestManager(t)
	m.RegisterDef(Definition{Name: "nginx", Label: "Nginx", Exclusive: "web"})
	m.RegisterDef(Definition{Name: "caddy", Label: "Caddy", Exclusive: "web"})
	markRunningForTest(t, m, "nginx")

	err := m.Start("caddy")

	if err == nil {
		t.Fatal("started a second web server")
	}
	if !strings.Contains(err.Error(), "Nginx") {
		t.Errorf("error does not name the conflict: %v", err)
	}
}

// Services in another group, or in none, are unaffected.
func TestStartAllowsServicesOutsideTheGroup(t *testing.T) {
	m := newTestManager(t)
	m.RegisterDef(Definition{Name: "nginx", Label: "Nginx", Exclusive: "web"})
	m.RegisterDef(Definition{Name: "mysql", Label: "MySQL"})
	markRunningForTest(t, m, "nginx")

	if err := m.conflictFor("mysql"); err != nil {
		t.Fatalf("unrelated service was blocked: %v", err)
	}
}

// Restarting the one that is already running is not a conflict with itself.
func TestConflictIgnoresTheServiceItself(t *testing.T) {
	m := newTestManager(t)
	m.RegisterDef(Definition{Name: "nginx", Label: "Nginx", Exclusive: "web"})
	markRunningForTest(t, m, "nginx")

	if err := m.conflictFor("nginx"); err != nil {
		t.Fatalf("nginx conflicted with itself: %v", err)
	}
}

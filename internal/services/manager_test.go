package services

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lara4ever/internal/config"
	"lara4ever/internal/i18n"
	"lara4ever/internal/vcruntime"
)

// TestMain loads the real catalogues before running this package's tests, so
// an *i18n.Error's message renders the actual English prose (with vars
// interpolated) instead of degrading to a bare key path — the same nil-Global
// fallback a package would hit in production before LoadCatalogues runs, but
// not what a test asserting on error text wants to see.
func TestMain(m *testing.M) {
	if os.Getenv("LARAENV_SERVICES_CONTEXT_HELPER") == "1" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	set, err := i18n.Load(os.DirFS(filepath.Join("..", "..", "langs")), ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "load langs:", err)
		os.Exit(1)
	}
	i18n.SetGlobal(set)
	os.Exit(m.Run())
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	cfg, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return NewManager(cfg)
}

// pingDef returns a Definition that spawns `replicas` long-lived ping.exe
// processes (a harmless, dependency-free Windows system binary). ReplicaArgs
// varies the count per replica so each is a distinct process.
func pingDef(name string, replicas int) Definition {
	return Definition{
		Name:        name,
		Label:       name,
		Path:        `C:\Windows\System32\ping.exe`,
		Internal:    true,
		Replicas:    replicas,
		ReplicaArgs: func(i int) []string { return []string{"-n", "60", "-w", "1000", "127.0.0.1"} },
	}
}

func TestReplicaHelpers(t *testing.T) {
	if got := replicaKey("php-fpm:8.1", 2); got != "php-fpm:8.1#2" {
		t.Errorf("replicaKey = %q", got)
	}
	if got := logicalName("php-fpm:8.1#2"); got != "php-fpm:8.1" {
		t.Errorf("logicalName replica = %q", got)
	}
	if got := logicalName("nginx"); got != "nginx" {
		t.Errorf("logicalName plain = %q", got)
	}
	if got := replicaCount(Definition{Replicas: 0}); got != 1 {
		t.Errorf("replicaCount(0) = %d, want 1", got)
	}
	if got := replicaCount(Definition{Replicas: 4}); got != 4 {
		t.Errorf("replicaCount(4) = %d, want 4", got)
	}
}

func TestPooledStartStop(t *testing.T) {
	m := newTestManager(t)
	const name = "pool-test"
	m.RegisterDef(pingDef(name, 3))

	if err := m.startOne(name); err != nil {
		t.Fatalf("startOne: %v", err)
	}
	t.Cleanup(func() { _ = m.stopOne(name) })

	// All three replicas tracked, but RunningNames collapses to one logical name.
	m.mu.Lock()
	procCount := len(m.procs)
	m.mu.Unlock()
	if procCount != 3 {
		t.Fatalf("expected 3 tracked procs, got %d", procCount)
	}
	if got := m.Status(name); got != "running" {
		t.Fatalf("Status = %q, want running", got)
	}
	names := m.RunningNames()
	if len(names) != 1 || names[0] != name {
		t.Fatalf("RunningNames = %v, want [%s]", names, name)
	}

	if err := m.stopOne(name); err != nil {
		t.Fatalf("stopOne: %v", err)
	}
	// killTree is async via taskkill; poll briefly for the procs to drain.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m.Status(name) == "stopped" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := m.Status(name); got != "stopped" {
		t.Fatalf("Status after stop = %q, want stopped", got)
	}
}

func TestSingleProcessBackCompat(t *testing.T) {
	m := newTestManager(t)
	const name = "single-test"
	def := pingDef(name, 1) // Replicas<=1 → tracked under the logical name
	m.RegisterDef(def)

	if err := m.startOne(name); err != nil {
		t.Fatalf("startOne: %v", err)
	}
	t.Cleanup(func() { _ = m.stopOne(name) })

	m.mu.Lock()
	_, trackedUnderName := m.procs[name]
	procCount := len(m.procs)
	m.mu.Unlock()
	if !trackedUnderName || procCount != 1 {
		t.Fatalf("single proc should be tracked under logical name; tracked=%v count=%d", trackedUnderName, procCount)
	}
	if got := m.Status(name); got != "running" {
		t.Fatalf("Status = %q, want running", got)
	}
}

// TestEmitCheckErrorSurfacesRedistURL covers the actual logic behind the
// PHP 8.6 fix: a vcruntime.Check failure (*vcruntime.RedistError) must
// reach the emitted ErrorEvent's RedistURL field as typed data, not just
// baked into Message text the frontend would otherwise have to parse.
func TestEmitCheckErrorSurfacesRedistURL(t *testing.T) {
	m := newTestManager(t)
	var got ErrorEvent
	m.SetErrorHandler(func(e ErrorEvent) { got = e })

	redistErr := &vcruntime.RedistError{Binary: "php.exe", RedistURL: vcruntime.RedistURLVS18}
	m.emitCheckError("php-fpm:8.6", redistErr)

	if got.Service != "php-fpm:8.6" {
		t.Errorf("Service = %q, want %q", got.Service, "php-fpm:8.6")
	}
	if got.Message != redistErr.Error() {
		t.Errorf("Message = %q, want %q", got.Message, redistErr.Error())
	}
	if got.RedistURL != vcruntime.RedistURLVS18 {
		t.Errorf("RedistURL = %q, want %q", got.RedistURL, vcruntime.RedistURLVS18)
	}
}

// TestEmitCheckErrorLeavesRedistURLEmptyForOtherErrors guards the other
// side: an error that isn't a *vcruntime.RedistError must not fabricate a
// RedistURL — the frontend treats "" as "no redist link to show", and a
// stray non-empty value here would show a bogus link for an unrelated
// failure.
func TestEmitCheckErrorLeavesRedistURLEmptyForOtherErrors(t *testing.T) {
	m := newTestManager(t)
	var got ErrorEvent
	m.SetErrorHandler(func(e ErrorEvent) { got = e })

	m.emitCheckError("nginx", fmt.Errorf("some unrelated failure"))

	if got.RedistURL != "" {
		t.Errorf("RedistURL = %q, want empty for a non-redist error", got.RedistURL)
	}
}

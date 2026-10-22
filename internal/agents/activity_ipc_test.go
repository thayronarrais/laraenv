package agents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestActivityIPCGuardLimitsAndAuthentication(t *testing.T) {
	_, server, _ := ipcFixture(t)
	for _, tc := range []struct {
		name, body, token string
		status            int
	}{
		{"missing credential", `{\"sessionId\":\"session\"}`, "", http.StatusUnauthorized},
		{"unknown fields", `{"sessionId":"session","prompt":"do not accept"}`, server.Descriptor().Token, http.StatusBadRequest},
		{"large body", strings.Repeat(" ", 64<<10) + `{}`, server.Descriptor().Token, http.StatusBadRequest},
		{"foreign session", `{"sessionId":"foreign"}`, server.Descriptor().Token, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, server.Descriptor().Endpoint+"/get", strings.NewReader(tc.body))
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			recorder := httptest.NewRecorder()
			server.serve(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("HTTP %d, want %d", recorder.Code, tc.status)
			}
		})
	}
}

func TestActivityIPCCloseOnlyOwnServer(t *testing.T) {
	registry, first, _ := ipcFixture(t)
	second, err := StartActivityOwner(context.Background(), registry, &activityIPCBackend{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := registry.ResolveOwner(second.Descriptor().Identity.ID); err != nil || !ok {
		t.Fatalf("other server removed: %v %v", ok, err)
	}
}

type delayedActivityBackend struct {
	activityIPCBackend
	started  chan struct{}
	complete chan struct{}
}

func (b *delayedActivityBackend) Wait(ctx context.Context, id string, _ uint64) (ActivitySnapshot, error) {
	close(b.started)
	select {
	case <-b.complete:
		return b.Get(ctx, id)
	case <-ctx.Done():
		return ActivitySnapshot{}, ctx.Err()
	}
}
func TestActivityIPCRejectsResponseFromReleasedRun(t *testing.T) {
	registry := NewActivityOwnerRegistry(t.TempDir(), nil)
	backend := &delayedActivityBackend{started: make(chan struct{}), complete: make(chan struct{})}
	server, err := StartActivityOwner(context.Background(), registry, backend)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	descriptor := server.Descriptor()
	if err := registry.SetTerminal(descriptor.Identity.ID, "terminal", true); err != nil {
		t.Fatal(err)
	}
	old := ownerBinding(descriptor, "session")
	if err := registry.Bind(old); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, descriptor.Endpoint+"/wait", strings.NewReader(`{"sessionId":"session"}`))
	request.Header.Set("Authorization", "Bearer "+descriptor.Token)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { server.serve(recorder, request); close(done) }()
	<-backend.started
	if err := registry.Release(old.SessionID, old.RunID); err != nil {
		t.Fatal(err)
	}
	if err := registry.Bind(ownerBinding(descriptor, "session")); err != nil {
		t.Fatal(err)
	}
	close(backend.complete)
	<-done
	if recorder.Code != http.StatusConflict {
		t.Fatalf("old run returned HTTP %d, want conflict", recorder.Code)
	}
}

type activityIPCBackend struct {
	mu             sync.Mutex
	interests      map[string]bool
	collectorCount int
	waitStarted    chan struct{}
}

func (b *activityIPCBackend) Get(_ context.Context, id string) (ActivitySnapshot, error) {
	return ActivitySnapshot{SessionID: id, Revision: 7, Nodes: []ActivityNode{}, BackgroundTasks: []ActivityNode{}}, nil
}
func (b *activityIPCBackend) SetInterest(_ context.Context, i ActivityInterest) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.interests == nil {
		b.interests = map[string]bool{}
	}
	if !b.interests[i.SessionID] {
		b.collectorCount++
		b.interests[i.SessionID] = true
	}
	return nil
}
func (b *activityIPCBackend) RemoveInterest(context.Context, string, string) error { return nil }
func (b *activityIPCBackend) Refresh(ctx context.Context, id string) (ActivitySnapshot, error) {
	return b.Get(ctx, id)
}
func (b *activityIPCBackend) Wait(ctx context.Context, _ string, _ uint64) (ActivitySnapshot, error) {
	if b.waitStarted != nil {
		select {
		case b.waitStarted <- struct{}{}:
		default:
		}
	}
	<-ctx.Done()
	return ActivitySnapshot{}, ctx.Err()
}
func ipcFixture(t *testing.T) (*ActivityOwnerRegistry, *ActivityOwnerServer, *activityIPCBackend) {
	t.Helper()
	r := NewActivityOwnerRegistry(t.TempDir(), nil)
	b := &activityIPCBackend{waitStarted: make(chan struct{}, 1)}
	s, err := StartActivityOwner(context.Background(), r, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	d := s.Descriptor()
	if err := r.SetTerminal(d.Identity.ID, "terminal", true); err != nil {
		t.Fatal(err)
	}
	if err := r.Bind(ownerBinding(d, "session")); err != nil {
		t.Fatal(err)
	}
	return r, s, b
}
func TestActivityIPCTwoClientsUseOneBackend(t *testing.T) {
	r, s, b := ipcFixture(t)
	for _, window := range []string{"window-a", "window-b"} {
		c, err := NewActivityClient(r, s.Descriptor().Identity.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if err := c.SetInterest(context.Background(), ActivityInterest{WindowID: window, SessionID: "session", Purpose: ActivityInterestDialog}); err != nil {
			t.Fatal(err)
		}
		got, err := c.Get(context.Background(), "session")
		if err != nil || got.Revision != 7 {
			t.Fatalf("snapshot: %+v %v", got, err)
		}
	}
	b.mu.Lock()
	collectorCount := b.collectorCount
	b.mu.Unlock()
	if collectorCount != 1 {
		t.Fatal(collectorCount)
	}
}
func TestActivityIPCRejectsForeignBinding(t *testing.T) {
	r, s, _ := ipcFixture(t)
	c, err := NewActivityClient(r, s.Descriptor().Identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Get(context.Background(), "foreign-session"); err == nil {
		t.Fatal("foreign snapshot returned")
	}
	if err := c.SetInterest(context.Background(), ActivityInterest{WindowID: "window", SessionID: "foreign-session", Purpose: ActivityInterestCounter}); err == nil {
		t.Fatal("foreign interest accepted")
	}
}
func TestActivityIPCCancelAndOwnerExit(t *testing.T) {
	r, s, b := ipcFixture(t)
	c, err := NewActivityClient(r, s.Descriptor().Identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.Wait(ctx, "session", 7); done <- err }()
	select {
	case <-b.waitStarted:
	case <-time.After(time.Second * 3):
		t.Fatal("wait never reached backend")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second * 3):
		t.Fatal("cancellation stuck")
	}
	go func() { _, err := c.Wait(context.Background(), "session", 7); done <- err }()
	select {
	case <-b.waitStarted:
	case <-time.After(time.Second * 3):
		t.Fatal("second wait missing")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("owner exit returned success")
		}
	case <-time.After(time.Second * 3):
		t.Fatal("owner exit stuck")
	}
	if _, ok, err := NewActivityOwnerRegistry(r.root, nil).ResolveSession("session"); err != nil || ok {
		t.Fatalf("closed owner discoverable %v %v", ok, err)
	}
}

// This test launches a real second process: an in-process mutex cannot satisfy
// the registry claim check or the socket request exercised here.
func TestActivityIPCTwoProcesses(t *testing.T) {
	if os.Getenv("LARA_ACTIVITY_TEST_CHILD") == "1" {
		r := NewActivityOwnerRegistry(os.Getenv("LARA_ACTIVITY_TEST_ROOT"), nil)
		owner, ok, err := r.ResolveSession("session")
		if err != nil || !ok {
			t.Fatalf("resolve parent: %v %v", ok, err)
		}
		binding, ok, err := r.ResolveProvider(AgentClaude, "proven-child")
		if err != nil || !ok || binding.SessionID != "session" || binding.ProviderSessionID != "provider-session" || binding.OwnerID != owner.Identity.ID {
			t.Fatalf("cross process alias did not resolve canonical root: %+v %v %v", binding, ok, err)
		}
		c, err := NewActivityClient(r, owner.Identity.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if got, err := c.Get(context.Background(), "session"); err != nil || got.Revision != 7 {
			t.Fatalf("cross process snapshot: %+v %v", got, err)
		}
		child, err := StartActivityOwner(context.Background(), r, &activityIPCBackend{})
		if err != nil {
			t.Fatal(err)
		}
		defer child.Close()
		if err := r.SetTerminal(child.Descriptor().Identity.ID, "terminal", true); err != nil {
			t.Fatal(err)
		}
		if err := r.Bind(ownerBinding(child.Descriptor(), "session")); !errors.Is(err, ErrActivityOwnerConflict) {
			t.Fatalf("cross process claim: %v", err)
		}
		return
	}
	r, _, _ := ipcFixture(t)
	binding, ok, err := r.ResolveBinding("session")
	if err != nil || !ok {
		t.Fatalf("resolve fixture: %v %v", ok, err)
	}
	if err := r.SetProviderAliases(binding.SessionID, binding.RunID, []string{"proven-child"}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestActivityIPCTwoProcesses$", "-test.v")
	cmd.Env = append(os.Environ(), "LARA_ACTIVITY_TEST_CHILD=1", "LARA_ACTIVITY_TEST_ROOT="+r.root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if _, ok, err := r.ResolveSession("session"); err != nil || !ok {
		t.Fatalf("child cleanup removed parent: %v %v", ok, err)
	}
}

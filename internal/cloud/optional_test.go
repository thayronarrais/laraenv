package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"lara4ever/internal/config"
)

func optionalClient(t *testing.T, endpoint string) (*Client, *config.Store) {
	t.Helper()
	store, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	cl := store.Get().Cloud
	cl.BaseURL, cl.Token, cl.Plan, cl.SyncKey, cl.SyncEnabled = endpoint, "saved-token", "pro", EncodeKey(make([]byte, 32)), true
	cl.UserID, cl.Email, cl.Name, cl.GithubID, cl.KdfSalt = 7, "saved@example.invalid", "Saved", "123", "saved-salt"
	if err := store.SetCloud(cl); err != nil {
		t.Fatal(err)
	}
	return NewClient(store), store
}

func setOptionalEnabled(t *testing.T, client *Client, enabled bool) {
	t.Helper()
	setter, ok := any(client).(interface{ SetEnabled(bool) error })
	if !ok {
		t.Fatal("cloud must expose an explicit opt-in")
	}
	if err := setter.SetEnabled(enabled); err != nil {
		t.Fatal(err)
	}
}

func TestDisabledCloudRefusesAllHTTPFamilies(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count.Add(1); _, _ = w.Write([]byte(`{}`)) }))
	defer server.Close()
	client, _ := optionalClient(t, server.URL)
	ctx := context.Background()
	calls := map[string]func() error{
		"health":          func() error { return client.Health(ctx) },
		"login":           func() error { _, err := client.Login(ctx, "a", "b"); return err },
		"register":        func() error { _, err := client.Register(ctx, "a", "b", "c"); return err },
		"me":              func() error { _, err := client.Me(ctx); return err },
		"resources":       func() error { _, err := client.PullResources(ctx, ""); return err },
		"resource upload": func() error { return client.PutResource(ctx, KindSSHHost, "host", []byte(`{}`), 0) },
		"resource delete": func() error { return client.DeleteResource(ctx, KindSSHHost, "host") },
		"deployments":     func() error { _, err := client.ListDeployments(ctx); return err },
		"checkout":        func() error { return client.StartCheckout(ctx) },
		"portal":          func() error { return client.StartPortal(ctx) },
		"paypal":          func() error { return client.StartPayPalCheckout(ctx) },
		"github":          func() error { _, err := client.LoginWithGithub(ctx); return err },
		"logout":          func() error { return client.Logout(ctx) },
		"prime sync key":  func() error { return client.PrimeKey("unused-password") },
		"quote":           func() error { _, err := client.RandomQuote(ctx); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, ErrDisabled) {
				t.Errorf("disabled cloud returned %v, want ErrDisabled", err)
			}
		})
	}
	if count.Load() != 0 {
		t.Fatalf("disabled profile sent %d requests", count.Load())
	}
	raw, _ := json.Marshal(client.Status())
	var status map[string]any
	_ = json.Unmarshal(raw, &status)
	if status["enabled"] != false {
		t.Fatalf("status must expose local disabled state: %s", raw)
	}
	if client.SyncStatusSnapshot().Enabled {
		t.Fatal("disabled cloud reported active sync")
	}
}

func TestDisableCancelsInflightRequestAndPreservesProfile(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) > 1 {
			_, _ = w.Write([]byte(`{"ok":true,"service":"laraenv-server"}`))
			return
		}
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	client, store := optionalClient(t, server.URL)
	setOptionalEnabled(t, client, true)
	before := store.Get().Cloud
	done := make(chan error, 1)
	go func() { _, err := client.Me(context.Background()); done <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	setOptionalEnabled(t, client, false)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("disabled in-flight request succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("disable did not cancel request")
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("transport did not receive cancellation")
	}
	setOptionalEnabled(t, client, true)
	if after := store.Get().Cloud; !reflect.DeepEqual(after, before) {
		t.Fatalf("disable changed saved cloud profile: before=%+v after=%+v", before, after)
	}
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("re-enabled cloud inherited canceled request context: %v", err)
	}
}

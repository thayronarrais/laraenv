package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"lara4ever/internal/cloud"
	"lara4ever/internal/config"
	pkgcron "lara4ever/internal/cron"
)

func TestCloudSyncPreservesRemoteSSHPasswordWhenLocalProtectionIsUnavailable(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/resources" {
			_, _ = fmt.Fprint(w, `{"resources":[]}`)
			return
		}
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/ssh_host/") {
			writes.Add(1)
			_, _ = fmt.Fprint(w, `{"kind":"ssh_host","external_id":"locked","version":1}`)
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "config.json")
	// This deliberately invalid envelope represents a portable copy from another account.
	data := map[string]any{"projects": map[string]any{"sshHosts": map[string]any{
		"locked": map[string]any{"name": "locked", "address": "fixture.invalid", "user": "fixture", "authMode": "password", "passwordProtected": "dpapi:v1:unavailable"},
	}}}
	raw, _ := json.Marshal(data)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	hosts := store.ListSSHHosts()
	if len(hosts) != 1 || !hosts[0].PasswordUnavailable {
		t.Fatalf("fixture did not load a locked host: %+v", hosts)
	}
	settings := store.Get().Cloud
	settings.Enabled = true
	settings.BaseURL, settings.Token, settings.Plan, settings.SyncEnabled = server.URL, "fixture-token", "pro", true
	settings.SyncKey = cloud.EncodeKey(make([]byte, 32))
	if err := store.SetCloud(settings); err != nil {
		t.Fatal(err)
	}
	crons := pkgcron.NewManager(nil, nil, nil)
	defer crons.Stop()
	app := &App{cfg: store, cloud: cloud.NewClient(store), crons: crons, ctx: context.Background()}
	summary, err := app.CloudSyncNow()
	if err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 0 {
		t.Fatal("sync replaced a remote SSH password with an unavailable local value")
	}
	if !strings.Contains(strings.ToLower(summary), "password") {
		t.Fatalf("missing actionable password warning: %s", summary)
	}
}

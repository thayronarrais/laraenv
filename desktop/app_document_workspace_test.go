package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"lara4ever/internal/cloud"
	"lara4ever/internal/config"
	pkgcron "lara4ever/internal/cron"
)

const documentLayout = `{"version":1,"tabs":[{"references":[{"context":{"kind":"project","projectName":"missing-project"},"rootKey":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","relativePath":"local-document-marker.md"}],"mode":"preview"}],"activeTabIndex":0,"visible":true,"panelPercent":45}`

func documentTestStore(t *testing.T) *config.Store {
	t.Helper()
	cfg, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDocumentWorkspaceStrictSchema(t *testing.T) {
	a := &App{cfg: documentTestStore(t)}
	if a.LoadDocumentWorkspace() != "" {
		t.Fatal("old config should have no layout")
	}
	for _, valid := range []string{documentLayout, `{"version":1,"tabs":[],"activeTabIndex":-1,"visible":false,"panelPercent":20}`, strings.ReplaceAll(documentLayout, `"panelPercent":45`, `"panelPercent":80`), strings.ReplaceAll(documentLayout, `"kind":"project","projectName":"missing-project"`, `"kind":"agent","sessionId":"missing-session"`)} {
		if err := a.SaveDocumentWorkspace(valid); err != nil {
			t.Fatal(valid, err)
		}
	}
	invalid := []string{
		``, `null`, `{}`, documentLayout + `{}`, strings.Repeat(" ", 1024*1024) + documentLayout,
		strings.ReplaceAll(documentLayout, `"version":1`, `"version":2`),
		strings.ReplaceAll(documentLayout, `"version":1`, `"Version":1`),
		strings.ReplaceAll(documentLayout, `"version":1`, `"version":1,"version":1`),
		strings.ReplaceAll(documentLayout, `"visible":true,`, ``),
		strings.ReplaceAll(documentLayout, `"visible":true`, `"visible":null`),
		strings.ReplaceAll(documentLayout, `"version":1`, `"version":1,"content":"secret"`),
		strings.ReplaceAll(documentLayout, `"mode":"preview"`, `"mode":"preview","revision":"secret"`),
		strings.ReplaceAll(documentLayout, `"relativePath":"local-document-marker.md"`, `"relativePath":"local-document-marker.md","text":"secret"`),
		strings.ReplaceAll(documentLayout, `"projectName":"missing-project"`, `"projectName":"missing-project","sessionId":"other"`),
		strings.ReplaceAll(documentLayout, `"projectName":"missing-project"`, `"projectName":"missing-project","credentials":"secret"`),
		strings.ReplaceAll(documentLayout, `"mode":"preview"`, `"mode":"other"`),
		strings.ReplaceAll(documentLayout, `"activeTabIndex":0`, `"activeTabIndex":-1`),
		strings.ReplaceAll(documentLayout, `"activeTabIndex":0`, `"activeTabIndex":1`),
		strings.ReplaceAll(documentLayout, `"panelPercent":45`, `"panelPercent":19.9`),
		strings.ReplaceAll(documentLayout, `"panelPercent":45`, `"panelPercent":80.1`),
		strings.ReplaceAll(documentLayout, strings.Repeat("a", 64), "invalid"),
		strings.ReplaceAll(documentLayout, `local-document-marker.md`, `../outside.md`),
		strings.ReplaceAll(documentLayout, `local-document-marker.md`, `C:/outside.md`),
		`{"version":1,"tabs":[],"activeTabIndex":0,"visible":false,"panelPercent":45}`,
		`{"version":1,"tabs":null,"activeTabIndex":-1,"visible":false,"panelPercent":45}`,
		`{"version":1,"tabs":[{"references":[],"mode":"source"}],"activeTabIndex":0,"visible":true,"panelPercent":45}`,
	}
	if err := a.SaveDocumentWorkspace(documentLayout); err != nil {
		t.Fatal(err)
	}
	for i, raw := range invalid {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if err := a.SaveDocumentWorkspace(raw); err == nil {
				t.Fatal("accepted invalid layout", raw[:min(len(raw), 300)])
			}
			if a.LoadDocumentWorkspace() != documentLayout {
				t.Fatal("invalid write changed saved state")
			}
		})
	}
}

func TestDocumentWorkspaceIsLocalAcrossRealCloudSync(t *testing.T) {
	cfg := documentTestStore(t)
	var mu sync.Mutex
	payloads := map[string][][]byte{}
	var gets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/resources" {
			gets = append(gets, r.URL.Query().Get("kind"))
			_, _ = io.WriteString(w, `{"resources":[]}`)
			return
		}
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/resources/") {
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/resources/"), "/")
			if len(parts) != 2 {
				t.Error(r.URL.Path)
				w.WriteHeader(400)
				return
			}
			b, _ := io.ReadAll(r.Body)
			payloads[parts[0]] = append(payloads[parts[0]], b)
			_, _ = fmt.Fprintf(w, `{"kind":%q,"external_id":%q,"version":1}`, parts[0], parts[1])
			return
		}
		t.Errorf("unexpected cloud request: %s %s", r.Method, r.URL)
		w.WriteHeader(400)
	}))
	defer server.Close()
	key := bytes.Repeat([]byte{7}, 32)
	cl := cfg.Get().Cloud
	cl.Enabled = true
	cl.BaseURL = server.URL
	cl.Token = "test-token"
	cl.Plan = "pro"
	cl.SyncEnabled = true
	cl.SyncKey = cloud.EncodeKey(key)
	if err := cfg.SetCloud(cl); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetSSHHost(config.SSHHost{Name: "ssh-positive-marker", Address: "example.invalid", User: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetDeployPreset(config.DeployPreset{ID: "preset-id", Name: "preset-positive-marker", Script: "echo fixture"}); err != nil {
		t.Fatal(err)
	}
	crons := pkgcron.NewManager(nil, nil, nil)
	crons.Start([]pkgcron.Job{{ID: "cron-id", Name: "cron-positive-marker", Enabled: false}})
	defer crons.Stop()
	a := &App{cfg: cfg, cloud: cloud.NewClient(cfg), crons: crons, ctx: context.Background()}
	if err := a.SaveDocumentWorkspace(documentLayout); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CloudSyncNow(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 3 || len(gets) != 3 {
		t.Fatal("expected exactly three resource kinds", payloads, gets)
	}
	for kind, marker := range map[string]string{"ssh_host": "ssh-positive-marker", "cron": "cron-positive-marker", "deploy_preset": "preset-positive-marker"} {
		bodies := payloads[kind]
		if len(bodies) != 1 {
			t.Fatalf("%s: expected one PUT, got %d", kind, len(bodies))
		}
		var body struct {
			Ciphertext string `json:"ciphertext"`
		}
		if err := json.Unmarshal(bodies[0], &body); err != nil {
			t.Fatal(err)
		}
		blob, err := base64.StdEncoding.DecodeString(body.Ciphertext)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := cloud.Decrypt(key, blob)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(plain, []byte(marker)) {
			t.Fatalf("%s missing positive marker: %s", kind, plain)
		}
		if bytes.Contains(plain, []byte("local-document-marker")) || bytes.Contains(plain, []byte("documentWorkspace")) {
			t.Fatalf("document state leaked in %s: %s", kind, plain)
		}
	}
}

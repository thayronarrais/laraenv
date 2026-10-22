package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lara4ever/internal/cloud"
	"lara4ever/internal/config"
	"lara4ever/internal/deploy"
	"lara4ever/internal/deploycoord"
	"lara4ever/internal/deployments"
	"lara4ever/internal/i18n"
)

type fixtureDeploymentProtector struct{}

func (fixtureDeploymentProtector) Protect(s string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}
func (fixtureDeploymentProtector) Unprotect(s string) (string, error) {
	b, e := base64.StdEncoding.DecodeString(s)
	return string(b), e
}

type fixtureDeploymentExecutor struct {
	mu           sync.Mutex
	calls        int
	host         config.SSHHost
	steps        []deploy.Step
	entered      chan struct{}
	resume       chan struct{}
	err          error
	log          string
	activityPath string
}

func (f *fixtureDeploymentExecutor) Apply(h config.SSHHost, steps []deploy.Step, line func(string), step func(int, int, string, string)) (string, error) {
	f.mu.Lock()
	f.calls++
	f.host = h
	f.steps = steps
	f.mu.Unlock()
	if f.entered != nil {
		close(f.entered)
		<-f.resume
	}
	line(f.log)
	return f.log, f.err
}
func (f *fixtureDeploymentExecutor) Detect(h config.SSHHost, line func(string)) (*deploy.DetectionResult, error) {
	f.mu.Lock()
	f.calls++
	f.host = h
	f.mu.Unlock()
	return &deploy.DetectionResult{OSFamily: "ubuntu", OSVersion: "24.04", Installed: map[string]string{}, Log: "fixture detection"}, f.err
}
func (f *fixtureDeploymentExecutor) Activity(h config.SSHHost, path string) (string, error) {
	f.activityPath = path
	return "[laraenv-autopull] 2026-10-19T12:00:00Z aaaa -> bbbb\n", f.err
}

type fixtureDeploymentEvents struct {
	name  string
	value any
}

func newOfflineFixture(t *testing.T) (*App, *deployments.Store, *fixtureDeploymentExecutor, chan fixtureDeploymentEvents) {
	t.Helper()
	previousCatalog, previousLanguage := i18n.Global(), i18n.Active()
	set, err := i18n.Load(os.DirFS("langs"), ".")
	if err != nil {
		t.Fatal(err)
	}
	i18n.SetGlobal(set)
	i18n.SetActive("en")
	t.Cleanup(func() { i18n.SetGlobal(previousCatalog); i18n.SetActive(previousLanguage) })
	cfg, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.SetSSHHost(config.SSHHost{Name: "fixture", Address: "192.0.2.10", Port: 22, User: "fixture", AuthMode: "key"}); err != nil {
		t.Fatal(err)
	}
	store, err := deployments.NewStore(filepath.Join(t.TempDir(), "deployments"), fixtureDeploymentProtector{})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixtureDeploymentExecutor{}
	events := make(chan fixtureDeploymentEvents, 100)
	a := &App{ctx: context.Background(), cfg: cfg, cloud: cloud.NewClient(cfg), localDeployments: store, deploymentExecutor: f,
		deploymentEmit: func(name string, value any) { events <- fixtureDeploymentEvents{name, value} }}
	return a, store, f, events
}
func waitDeploymentEvent(t *testing.T, events chan fixtureDeploymentEvents, suffix string) fixtureDeploymentEvents {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-events:
			if strings.Contains(e.name, suffix) {
				return e
			}
		case <-timer.C:
			t.Fatal("timed out waiting for deployment event " + suffix)
			return fixtureDeploymentEvents{}
		}
	}
}
func fixtureApply() deploy.ApplyConfig {
	return deploy.ApplyConfig{Name: "remote-name", ProjectKind: "static", Domain: "fixture.invalid", GitURL: "https://example.invalid/repo.git", GitPAT: "fixture-secret-token", WebServer: "nginx", BuildScript: "echo fixture"}
}

func TestOfflineDeploymentApplyDoesNotRequireAccountAndSerializesHostedAlias(t *testing.T) {
	a, store, f, events := newOfflineFixture(t)
	d, err := a.CreateDeployment("display label", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	f.entered = make(chan struct{})
	f.resume = make(chan struct{})
	f.log = "git https://fixture-secret-token@example.invalid/repo.git fixture-secret-token"
	cfg := fixtureApply()
	if err = a.ApplyDeployment(d.ID, cfg); err != nil {
		t.Fatal(err)
	}
	<-f.entered
	if err = a.CloudDeploymentDeploy(123); !errors.Is(err, deploycoord.ErrBusy) {
		t.Fatalf("hosted alias bypassed coordinator: %v", err)
	}
	_ = a.cfg.SetSSHHost(config.SSHHost{Name: "fixture", Address: "192.0.2.99", Port: 22, User: "changed"})
	cfg.DatabaseAllowedIPs = []string{"changed"}
	close(f.resume)
	waitDeploymentEvent(t, events, ":apply:done:")
	f.mu.Lock()
	if f.calls != 1 || f.host.Address != "192.0.2.10" {
		t.Errorf("execution snapshot changed: %#v", f)
	}
	f.mu.Unlock()
	saved, err := store.Get(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	ap := saved.Config["apply"].(map[string]any)
	if strings.Contains(ap["git_url"].(string), "fixture-secret-token") {
		t.Fatal("PAT persisted in URL")
	}
	if len(saved.Runs) != 1 || saved.Runs[0].Status != "ok" {
		t.Fatalf("run not persisted: %#v", saved.Runs)
	}
	log, err := a.ReadDeploymentRunLog(d.ID, saved.Runs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log, "fixture-secret-token") {
		t.Fatal("known secret leaked in log")
	}
}

func TestOfflineDetectPreservesApplyAndActivityUsesRemoteName(t *testing.T) {
	a, store, f, events := newOfflineFixture(t)
	d, _ := a.CreateDeployment("display label", "fixture")
	_, err := store.Patch(d.ID, map[string]any{"config": map[string]any{"apply": fixtureApply()}})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.DetectDeployment(d.ID); err != nil {
		t.Fatal(err)
	}
	waitDeploymentEvent(t, events, ":detect:done:")
	saved, _ := store.Get(d.ID)
	if saved.Config["apply"] == nil || saved.OSFamily != "ubuntu" {
		t.Fatalf("detection lost saved config: %#v", saved)
	}
	entries, err := a.DeploymentActivity(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.activityPath != "/tmp/laraenv-remote-name.log" || len(entries) != 1 {
		t.Fatalf("wrong activity target: %s %#v", f.activityPath, entries)
	}
}

type finishFailRepository struct{ deploymentRepository }

func (f finishFailRepository) FinishRun(string, string, string, string, string) error {
	return errors.New("fixture disk full")
}
func TestOfflineRemoteSuccessWithSaveFailureIsNotRetried(t *testing.T) {
	a, store, f, events := newOfflineFixture(t)
	d, _ := a.CreateDeployment("fixture", "fixture")
	a.localDeployments = finishFailRepository{store}
	if err := a.ApplyDeployment(d.ID, fixtureApply()); err != nil {
		t.Fatal(err)
	}
	e := waitDeploymentEvent(t, events, ":apply:error:")
	if !strings.Contains(e.value.(string), "remote operation completed") || !strings.Contains(e.value.(string), "fixture disk full") {
		t.Fatalf("remote result lost: %v", e.value)
	}
	if f.calls != 1 {
		t.Fatalf("remote command retried %d times", f.calls)
	}
	if _, err := store.Get(d.ID); err != nil {
		t.Fatal("record was lost", err)
	}
}

func TestOfflineFailedDestroyKeepsRegistration(t *testing.T) {
	a, store, f, events := newOfflineFixture(t)
	d, _ := a.CreateDeployment("fixture", "fixture")
	_, _ = store.Patch(d.ID, map[string]any{"config": map[string]any{"apply": fixtureApply()}})
	f.err = errors.New("fixture remote partial failure")
	if err := a.DestroyDeployment(d.ID); err != nil {
		t.Fatal(err)
	}
	waitDeploymentEvent(t, events, ":destroy:error:")
	saved, err := store.Get(d.ID)
	if err != nil || len(saved.Runs) != 1 || saved.Runs[0].Status != "error" {
		t.Fatalf("failed destroy lost history/record: %#v %v", saved, err)
	}
}

func TestOfflineImportRequiresExistingHostAndNeverExecutesSSH(t *testing.T) {
	a, _, f, _ := newOfflineFixture(t)
	bundle := deployments.ImportBundle{Version: 1, Source: deployments.ImportSource{Namespace: "fixture-export"}, Deployments: []deployments.Deployment{{ID: "42", Name: "imported", SSHHostExternalID: "old-host", Config: map[string]any{"apply": fixtureApply()}}}, HostMappings: map[string]string{"old-host": "missing"}}
	raw, _ := json.Marshal(bundle)
	if _, err := a.ImportDeploymentsJSON(string(raw)); err == nil {
		t.Fatal("accepted mapping to missing local host")
	}
	bundle.HostMappings["old-host"] = "fixture"
	raw, _ = json.Marshal(bundle)
	r, err := a.ImportDeploymentsJSON(string(raw))
	if err != nil || len(r.Inserted) != 1 {
		t.Fatalf("import failed: %#v %v", r, err)
	}
	r, err = a.ImportDeploymentsJSON(string(raw))
	if err != nil || len(r.Skipped) != 1 {
		t.Fatalf("import not idempotent: %#v %v", r, err)
	}
	if f.calls != 0 {
		t.Fatal("import executed SSH")
	}
}

func TestOfflineUnavailableStoreDoesNotPanicAndCreateValidatesHost(t *testing.T) {
	a, _, _, _ := newOfflineFixture(t)
	if _, err := a.CreateDeployment("fixture", "missing"); err == nil {
		t.Fatal("accepted nonexistent host")
	}
	a.localDeployments = nil
	a.localDeploymentsErr = errors.New("fixture locked index")
	if _, err := a.ListDeployments(); err == nil {
		t.Fatal("missing store did not report recovery error")
	}
}

type startFailRepository struct{ deploymentRepository }

func (f startFailRepository) StartRun(string, string) (*deployments.RunSummary, error) {
	return nil, errors.New("fixture start save failed")
}
func TestOfflineFailedStartSaveNeverOpensSSHAndReleasesCoordinator(t *testing.T) {
	a, store, f, _ := newOfflineFixture(t)
	d, _ := a.CreateDeployment("fixture", "fixture")
	a.localDeployments = startFailRepository{store}
	if err := a.ApplyDeployment(d.ID, fixtureApply()); err == nil {
		t.Fatal("run started despite failed durable save")
	}
	if f.calls != 0 {
		t.Fatal("SSH opened before durable start")
	}
	release, err := a.deploymentCoordinator.Acquire()
	if err != nil {
		t.Fatal("validation error leaked coordinator", err)
	}
	release()
}

func TestOfflineLocalDataSurvivesCloudOptInAndOptOut(t *testing.T) {
	a, store, _, _ := newOfflineFixture(t)
	d, _ := a.CreateDeployment("fixture", "fixture")
	_, _ = store.Patch(d.ID, map[string]any{"config": map[string]any{"apply": fixtureApply()}})
	cl := a.cfg.Get().Cloud
	cl.Token = "saved-session"
	cl.SyncKey = "saved-sync-key"
	cl.Plan = "free"
	cl.SyncEnabled = true
	if err := a.cfg.SetCloud(cl); err != nil {
		t.Fatal(err)
	}
	if a.syncShouldRun() {
		t.Fatal("legacy sync flag bypassed opt-in")
	}
	if err := a.CloudSetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := a.CloudSetEnabled(false); err != nil {
		t.Fatal(err)
	}
	saved, err := a.GetDeployment(d.ID)
	if err != nil || saved.Config["apply"] == nil {
		t.Fatalf("cloud toggle lost local data: %#v %v", saved, err)
	}
	after := a.cfg.Get().Cloud
	if after.Token != cl.Token || after.SyncKey != cl.SyncKey || after.SyncEnabled != cl.SyncEnabled {
		t.Fatal("cloud opt-out erased saved profile")
	}
}

func TestOfflineExplicitCloudImportPreservesSourceAndSanitizesLegacyCredentialURL(t *testing.T) {
	a, store, f, _ := newOfflineFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/deployments/42" {
			t.Errorf("unexpected hosted mutation: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":42,"name":"hosted fixture","ssh_host_external_id":"old-host","config":{"apply":{"name":"original-remote","git_url":"https://legacy-token@example.invalid/repo.git"}},"runs":[{"id":8,"kind":"apply","status":"ok","error_message":"legacy-token was rejected by git","started_at":"2026-10-19T12:00:00Z","has_log":true}]}`))
	}))
	defer server.Close()
	cl := a.cfg.Get().Cloud
	cl.Enabled = true
	cl.UserID = 12
	cl.Token = "fixture-token"
	cl.BaseURL = server.URL
	if err := a.cfg.SetCloud(cl); err != nil {
		t.Fatal(err)
	}
	result, err := a.ImportCloudDeployments([]int{42}, map[string]string{"old-host": "fixture"})
	if err != nil || len(result.Inserted) != 1 {
		t.Fatalf("cloud import: %#v %v", result, err)
	}
	d, err := store.Get(result.Inserted[0])
	if err != nil {
		t.Fatal(err)
	}
	if d.Origin.ID != "42" || d.Origin.Source.AccountID != "12" || d.Origin.Source.ServiceURL != server.URL {
		t.Fatalf("wrong import scope: %#v", d.Origin)
	}
	ap := d.Config["apply"].(map[string]any)
	if strings.Contains(ap["git_url"].(string), "legacy-token") || ap["git_pat"] != "legacy-token" {
		t.Fatal("legacy credential lost or persisted in URL")
	}
	if len(d.Runs) != 1 || d.Runs[0].HasLog {
		t.Fatal("import invented unavailable historical log body")
	}
	if strings.Contains(d.Runs[0].ErrorMessage, "legacy-token") || !strings.Contains(d.Runs[0].ErrorMessage, "[REDACTED]") {
		t.Fatal("known credential leaked in imported history error")
	}
	if f.calls != 0 {
		t.Fatal("cloud import executed SSH")
	}
}

func TestOfflineCloudImportStopsWhenAccountChanges(t *testing.T) {
	a, store, f, _ := newOfflineFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cl := a.cfg.Get().Cloud
		cl.UserID = 99
		_ = a.cfg.SetCloud(cl)
		_, _ = w.Write([]byte(`{"id":42,"name":"fixture","ssh_host_external_id":"old-host"}`))
	}))
	defer server.Close()
	cl := a.cfg.Get().Cloud
	cl.Enabled = true
	cl.UserID = 12
	cl.Token = "fixture-token"
	cl.BaseURL = server.URL
	if err := a.cfg.SetCloud(cl); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ImportCloudDeployments([]int{42}, map[string]string{"old-host": "fixture"}); err == nil {
		t.Fatal("import accepted changed account scope")
	}
	list, _ := store.List()
	if len(list) != 0 || f.calls != 0 {
		t.Fatal("changed profile imported or executed remote commands")
	}
}

func TestOfflineSyncValidatesTheCapturedProfile(t *testing.T) {
	a, _, _, _ := newOfflineFixture(t)
	cl := a.cfg.Get().Cloud
	cl.Enabled = true
	cl.Token = "old-token"
	cl.UserID = 1
	cl.SyncKey = "saved-key"
	cl.SyncEnabled = true
	cl.Plan = "pro"
	if err := a.cfg.SetCloud(cl); err != nil {
		t.Fatal(err)
	}
	if !a.syncShouldRun() {
		t.Fatal("fixture old profile was not sync-enabled")
	}
	// Models an account change after the fast local hook check and before
	// queueing its background operation. The captured profile authorizes it.
	cl.Token = "new-token"
	cl.UserID = 2
	cl.Plan = "free"
	cl.SyncEnabled = false
	if err := a.cfg.SetCloud(cl); err != nil {
		t.Fatal(err)
	}
	if _, cancel, err := a.beginCloudSyncSession(); err == nil {
		cancel()
		t.Fatal("old profile authorized sync under a different disabled profile")
	}
}

func TestOfflineCancelledHostedOriginCannotEmitIntoNextAccount(t *testing.T) {
	a, _, _, events := newOfflineFixture(t)
	if err := a.CloudSetEnabled(true); err != nil {
		t.Fatal(err)
	}
	oldCtx, cancel, _, err := a.cloud.BeginSession(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	a.emitHostedDeployment(oldCtx, "deployment:apply:log:17", "old before change")
	<-events
	if err := a.CloudSetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := a.CloudSetEnabled(true); err != nil {
		t.Fatal(err)
	}
	a.emitHostedDeployment(oldCtx, "deployment:apply:done:17", "old after change")
	select {
	case event := <-events:
		t.Fatalf("stale origin fed a new listener: %#v", event)
	default:
	}
	newCtx, newCancel, _, err := a.cloud.BeginSession(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer newCancel()
	a.emitHostedDeployment(newCtx, "deployment:apply:done:17", "current origin")
	if event := <-events; event.value != "current origin" {
		t.Fatalf("new origin event missing: %#v", event)
	}
}

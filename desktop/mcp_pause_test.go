package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"lara4ever/internal/config"
	"lara4ever/internal/paths"
	"lara4ever/internal/projectroutes"
	"lara4ever/internal/projects"
	"lara4ever/internal/services"
	"lara4ever/internal/vhost"
)

const mcpPauseFixtureRootEnv = "LARAENV_MCP_PAUSE_FIXTURE_ROOT"

// paths resolves its root during package initialization. Run the same Go test
// in an isolated fixture process so certificate paths cannot reach an actual
// installation. This process invokes App methods directly; no GUI or service
// binary is launched.
func mcpPauseFixtureProcess(t *testing.T) bool {
	t.Helper()
	if root := os.Getenv(mcpPauseFixtureRootEnv); root != "" {
		if !strings.EqualFold(filepath.Clean(root), filepath.Clean(paths.Root())) {
			t.Fatal("pause fixture install root was not isolated before package initialization")
		}
		return false
	}
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.v")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.EqualFold(key, paths.EnvRoot) || strings.EqualFold(key, mcpPauseFixtureRootEnv) {
			continue
		}
		command.Env = append(command.Env, value)
	}
	command.Env = append(command.Env, paths.EnvRoot+"="+root, mcpPauseFixtureRootEnv+"="+root)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated pause fixture failed: %v\n%s", err, output)
	}
	return true
}

type mcpPauseFixture struct {
	app           *App
	store         *projectroutes.Store
	storePath     string
	project       projectroutes.Project
	before, after []projectroutes.Route
	certificates  map[string][]byte
	unrelated     []string
}

func mcpPauseWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func mcpNewPauseFixture(t *testing.T) mcpPauseFixture {
	t.Helper()
	root := paths.Root()
	cfg, err := config.NewStore(filepath.Join(root, "fixture-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Get().Projects
	settings.Root, settings.TLD = filepath.Join(root, "www"), "test"
	settings.Paused = map[string]bool{"demo": true}
	settings.SSL = map[string]bool{"demo": true}
	if err := cfg.SetProjects(settings); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settings.Root, 0700); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, "state", "fixture-project-routes.json")
	store, err := projectroutes.NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(settings.Root, "demo")
	missingTarget := filepath.Join(root, "missing-worktree")
	canonical := projectroutes.Route{ID: "demo-canonical", ProjectName: "demo", Hostname: "demo.test", Canonical: true, TargetKind: projectroutes.TargetMainCheckout, TargetPath: mainPath, SSL: true, ServeMode: projectroutes.ServeDocRoot}
	oldPreview := projectroutes.Route{ID: "old-preview", ProjectName: "demo", Hostname: "old.demo.test", TargetKind: projectroutes.TargetWorktree, TargetPath: missingTarget, WorktreePath: missingTarget, SSL: true, ServeMode: projectroutes.ServeDocRoot}
	newPreview := oldPreview
	newPreview.ID, newPreview.Hostname = "new-preview", "new.demo.test"
	before := []projectroutes.Route{canonical, oldPreview}
	// Missing targets make a regression fail before certificate generation or
	// PHP resolution. A paused apply must bypass both rendering and validation.
	afterCanonical := canonical
	afterCanonical.TargetPath = missingTarget
	after := []projectroutes.Route{afterCanonical, newPreview}
	if err := store.ReplaceProject("demo", before); err != nil {
		t.Fatal(err)
	}
	unrelated := projectroutes.Route{ID: "other-canonical", ProjectName: "other", Hostname: "other.test", Canonical: true, TargetKind: projectroutes.TargetMainCheckout, TargetPath: filepath.Join(settings.Root, "other"), SSL: true}
	if err := store.ReplaceProject("other", []projectroutes.Route{unrelated}); err != nil {
		t.Fatal(err)
	}
	hostsPath := filepath.Join(root, "fixture-hosts")
	mcpPauseWrite(t, hostsPath, []byte("127.0.0.1 localhost\n# preserve this unmanaged note\n"))
	app := &App{
		cfg: cfg, projects: projects.NewScanner(cfg), services: services.NewManager(cfg),
		nginx:  vhost.NewNginxConfigurator(paths.NginxSites()),
		apache: vhost.NewApacheConfigurator(paths.ApacheSites()),
		caddy:  vhost.NewCaddyConfigurator(paths.CaddySites()), hosts: vhost.NewHostsEditorAt(hostsPath),
	}
	// Resolver callbacks and the PHP manager deliberately remain nil. The
	// pause guards need only stored metadata and cannot enter normal generation.
	app.routes = projectroutes.NewManager(store, nil, nil, nil)
	fixture := mcpPauseFixture{app: app, store: store, storePath: storePath, project: projectroutes.Project{Name: "demo", MainPath: mainPath, TLD: "test", SSL: true}, before: before, after: after, certificates: map[string][]byte{}}
	all := append(append([]projectroutes.Route{}, before...), newPreview, unrelated)
	for _, route := range all {
		folder := routeFolder(route.ProjectName, route)
		for _, path := range []string{app.nginx.VhostPath(folder), app.apache.VhostPath(folder), app.caddy.VhostPath(folder)} {
			mcpPauseWrite(t, path, []byte("fixture vhost for "+route.Hostname))
			if route.ProjectName == "other" {
				fixture.unrelated = append(fixture.unrelated, path)
			}
		}
		if err := app.hosts.AddEntry("127.0.0.1", route.Hostname); err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{".crt", ".key"} {
			path := filepath.Join(paths.NginxCerts(), route.Hostname+suffix)
			data := []byte("fixture certificate material for " + route.Hostname + suffix)
			mcpPauseWrite(t, path, data)
			fixture.certificates[path] = data
		}
	}
	return fixture
}

func (fixture mcpPauseFixture) assertArtifactsRemoved(t *testing.T, routes []projectroutes.Route) {
	t.Helper()
	for _, route := range routes {
		folder := routeFolder("demo", route)
		for _, path := range []string{fixture.app.nginx.VhostPath(folder), fixture.app.apache.VhostPath(folder), fixture.app.caddy.VhostPath(folder)} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("paused route vhost remains or was recreated: %s (%v)", path, err)
			}
		}
	}
	hosts, err := fixture.app.hosts.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range routes {
		if strings.Contains(string(hosts), route.Hostname) {
			t.Errorf("paused route hosts entry remains or was recreated: %s", route.Hostname)
		}
	}
	if !strings.Contains(string(hosts), "other.test") || !strings.Contains(string(hosts), "localhost") || !strings.Contains(string(hosts), "preserve this unmanaged note") {
		t.Fatalf("pause removed unrelated hosts content: %s", hosts)
	}
	for _, path := range fixture.unrelated {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "fixture vhost for other.test" {
			t.Errorf("pause changed an unrelated site: %s (%v)", path, err)
		}
	}
	for path, expected := range fixture.certificates {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Errorf("pause changed or deleted retained certificate: %s (%v)", path, err)
		}
	}
	if !reflect.DeepEqual(fixture.store.List("demo"), fixture.before) {
		t.Errorf("pause changed saved route definitions: %#v", fixture.store.List("demo"))
	}
	if !fixture.app.cfg.Get().Projects.Paused["demo"] {
		t.Error("apply reset the project's persisted pause preference")
	}
	if running := fixture.app.services.RunningNames(); len(running) != 0 {
		t.Errorf("paused apply started services: %v", running)
	}
}

func TestMCPPauseRouteApplyCannotRecreateBeforeOrAfterArtifacts(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := mcpNewPauseFixture(t)
	savedBytes, err := os.ReadFile(fixture.storePath)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := fixture.app.applyProjectRouteSet(context.Background(), fixture.project, fixture.before, fixture.after); err != nil {
			t.Fatalf("paused route apply entered rendering/validation: %v", err)
		}
		fixture.assertArtifactsRemoved(t, append(append([]projectroutes.Route{}, fixture.before...), fixture.after...))
	}
	actualBytes, err := os.ReadFile(fixture.storePath)
	if err != nil || !bytes.Equal(actualBytes, savedBytes) {
		t.Fatalf("paused route apply changed persisted route bytes: %v", err)
	}
}

func TestMCPPauseApplyVhostBypassesGenerationAndKeepsSavedRoutes(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := mcpNewPauseFixture(t)
	// This helper reads stored routes; remove the uncommitted preview fixture
	// so this test asserts precisely the saved route set plus the main site.
	newRoute := fixture.after[1]
	for _, remove := range []func(string) error{fixture.app.nginx.RemoveVhost, fixture.app.apache.RemoveVhost, fixture.app.caddy.RemoveVhost} {
		if err := remove(routeFolder("demo", newRoute)); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.app.hosts.RemoveEntry(newRoute.Hostname); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := fixture.app.applyVhost("demo", true, true); err != nil {
			t.Fatalf("paused apply generated vhosts or resolved PHP: %v", err)
		}
		fixture.assertArtifactsRemoved(t, fixture.before)
	}
}

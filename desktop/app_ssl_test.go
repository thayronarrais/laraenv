//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"lara4ever/internal/config"
	"lara4ever/internal/paths"
	"lara4ever/internal/projectroutes"
	"lara4ever/internal/projects"
	"lara4ever/internal/services"
	"lara4ever/internal/vhost"
)

type sslFixture struct {
	app        *App
	configPath string
	hostsPath  string
	files      map[string][]byte
}

func newSSLFixture(t *testing.T, enabled, missingPHP bool) sslFixture {
	t.Helper()
	configPath := filepath.Join(paths.Root(), "ssl-fixture.json")
	cfg, err := config.NewStore(configPath)
	if err != nil {
		t.Fatal(err)
	}
	prefs := cfg.Get().Projects
	prefs.Root, prefs.TLD = filepath.Join(paths.Root(), "www"), "test"
	prefs.SSL = map[string]bool{"demo": enabled, "other": true}
	if missingPHP {
		prefs.PhpVersions = map[string]string{"demo": "9.9"}
	}
	if err := cfg.SetProjects(prefs); err != nil {
		t.Fatal(err)
	}
	hostsPath := filepath.Join(paths.Root(), "fixture-hosts")
	app := &App{
		cfg: cfg, projects: projects.NewScanner(cfg), services: services.NewManager(cfg),
		nginx:  vhost.NewNginxConfigurator(paths.NginxSites()),
		apache: vhost.NewApacheConfigurator(paths.ApacheSites()),
		caddy:  vhost.NewCaddyConfigurator(paths.CaddySites()), hosts: vhost.NewHostsEditorAt(hostsPath),
	}
	fixture := sslFixture{app: app, configPath: configPath, hostsPath: hostsPath, files: map[string][]byte{}}
	for _, path := range []string{
		app.nginx.VhostPath("demo"), app.apache.VhostPath("demo"), app.caddy.VhostPath("demo"),
		filepath.Join(paths.NginxCerts(), "demo.test.crt"), filepath.Join(paths.NginxCerts(), "demo.test.key"),
		vhost.LocalCACertPath(paths.NginxCerts()), filepath.Join(paths.NginxCerts(), "lara4ever-rootCA.key"),
		filepath.Join(prefs.Root, "demo", "index.html"), hostsPath,
	} {
		data := []byte("preserve SSL fixture: " + filepath.Base(path) + "\n")
		mcpPauseWrite(t, path, data)
		fixture.files[path] = data
	}
	return fixture
}

func (fixture sslFixture) assertPreserved(t *testing.T, enabled bool) {
	t.Helper()
	for path, want := range fixture.files {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("failed SSL action changed %s: %v; got %q", filepath.Base(path), err, got)
		}
	}
	if got := fixture.app.cfg.Get().Projects.SSL["demo"]; got != enabled {
		t.Errorf("in-memory SSL = %v after failure, want %v", got, enabled)
	}
	reloaded, err := config.NewStore(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get().Projects.SSL["demo"]; got != enabled {
		t.Errorf("persisted SSL = %v after failure, want %v", got, enabled)
	}
	if !reloaded.Get().Projects.SSL["other"] {
		t.Error("SSL rollback changed an unrelated project")
	}
}

func TestSetProjectSSLFailedEnablePreservesPreference(t *testing.T) {
	// The existing subprocess fixture resolves paths.Root to a fresh temp
	// directory before package initialization. No real hosts/cert files are used.
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := newSSLFixture(t, false, true)
	err := fixture.app.SetProjectSSL("demo", true)
	if err == nil || !strings.Contains(err.Error(), "runtime unavailable") {
		t.Fatalf("want missing-runtime failure, got %v", err)
	}
	fixture.assertPreserved(t, false)
}

func TestSetProjectSSLFailedDisablePreservesCertificateAndPreference(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := newSSLFixture(t, true, true)
	err := fixture.app.SetProjectSSL("demo", false)
	if err == nil || !strings.Contains(err.Error(), "runtime unavailable") {
		t.Fatalf("want missing-runtime failure, got %v", err)
	}
	fixture.assertPreserved(t, true)
}

func TestSetProjectSSLFailedApplyRestoresWrittenVhosts(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := newSSLFixture(t, true, false)
	path, err := windows.UTF16PtrFromString(fixture.hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	// Readers still work, so apply reaches the vhost write; Windows then
	// denies hosts modification. Lock only this temporary fixture file.
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	err = fixture.app.SetProjectSSL("demo", false)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "hosts") {
		t.Fatalf("want hosts-write failure, got %v", err)
	}
	fixture.assertPreserved(t, true)
}

func TestSetProjectSSLFailedConfigSaveRestoresMemory(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := newSSLFixture(t, false, true)
	path, err := windows.UTF16PtrFromString(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := fixture.app.SetProjectSSL("demo", true); err == nil {
		t.Fatal("want config replacement failure")
	}
	fixture.assertPreserved(t, false)
}

func TestApplyProjectSSLSuccessfulDisableRemovesOnlyLeafCertificate(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := newSSLFixture(t, true, false)
	if err := fixture.app.applyProjectSSL("demo", false); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".crt", ".key"} {
		if _, err := os.Stat(filepath.Join(paths.NginxCerts(), "demo.test"+suffix)); !os.IsNotExist(err) {
			t.Errorf("successful disable left leaf certificate %s: %v", suffix, err)
		}
	}
	for _, path := range []string{vhost.LocalCACertPath(paths.NginxCerts()), filepath.Join(paths.NginxCerts(), "lara4ever-rootCA.key")} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, fixture.files[path]) {
			t.Errorf("successful disable changed root CA %s: %v", filepath.Base(path), err)
		}
	}
	data, err := os.ReadFile(fixture.app.nginx.VhostPath("demo"))
	if err != nil || !bytes.Contains(data, []byte("listen 80;")) || bytes.Contains(data, []byte("listen 443")) {
		t.Errorf("successful disable did not apply HTTP vhost: %v; %s", err, data)
	}
	reloaded, err := config.NewStore(fixture.configPath)
	if err != nil || reloaded.Get().Projects.SSL["demo"] || !reloaded.Get().Projects.SSL["other"] {
		t.Errorf("successful disable did not preserve per-project preference: %v", err)
	}
}

func TestSetProjectSSLFailedApplyPreservesUnrelatedHostsChanges(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := newSSLFixture(t, true, false)
	store, err := projectroutes.NewStore(filepath.Join(paths.Root(), "fixture-routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	resolverFailure := errors.New("fixture route resolver failed")
	fixture.app.routes = projectroutes.NewManager(store, func(context.Context, string) (projectroutes.Project, error) {
		// Interleave an independent writer after SSL takes its snapshot, then
		// fail before the SSL operation has touched any hosts entries itself.
		if err := fixture.app.hosts.AddEntry("127.0.0.2", "other.test"); err != nil {
			t.Fatal(err)
		}
		current, err := os.ReadFile(fixture.hostsPath)
		if err != nil {
			t.Fatal(err)
		}
		current = append(current, []byte("10.0.0.8 personal.test # unmanaged change\r\n")...)
		if err := os.WriteFile(fixture.hostsPath, current, 0644); err != nil {
			t.Fatal(err)
		}
		fixture.files[fixture.hostsPath] = current
		return projectroutes.Project{}, resolverFailure
	}, nil, nil)
	if err := fixture.app.SetProjectSSL("demo", false); !errors.Is(err, resolverFailure) {
		t.Fatalf("want interleaved resolver failure, got %v", err)
	}
	fixture.assertPreserved(t, true)
}

type sslInterleavedContext struct {
	context.Context
	beforeErr func()
}

func (ctx sslInterleavedContext) Err() error {
	ctx.beforeErr()
	return ctx.Context.Err()
}

func TestSSLManagedRouteRollbackPreservesUnrelatedHostsChanges(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := newSSLFixture(t, true, false)
	project := projectroutes.Project{Name: "demo", MainPath: filepath.Join(paths.Root(), "www", "demo"), TLD: "test", SSL: false}
	routes := []projectroutes.Route{
		{ID: "canonical", ProjectName: "demo", Canonical: true, Hostname: "demo.test", TargetPath: project.MainPath, TargetKind: projectroutes.TargetMainCheckout},
		{ID: "preview", ProjectName: "demo", Hostname: "preview.demo.test", TargetPath: project.MainPath, TargetKind: projectroutes.TargetWorktree},
	}
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	interleaved := false
	ctx := sslInterleavedContext{Context: base, beforeErr: func() {
		if interleaved {
			return
		}
		// Cancel only after a real route-vhost write, and commit B's independent
		// hosts change immediately before cancellation reaches the rollback.
		for _, path := range []string{fixture.app.nginx.VhostPath("demo"), fixture.app.apache.VhostPath("demo"), fixture.app.caddy.VhostPath("demo")} {
			data, err := os.ReadFile(path)
			if err != nil || bytes.Equal(data, fixture.files[path]) {
				continue
			}
			interleaved = true
			if err := fixture.app.hosts.AddEntry("127.0.0.2", "other.test"); err != nil {
				t.Fatal(err)
			}
			current, err := os.ReadFile(fixture.hostsPath)
			if err != nil {
				t.Fatal(err)
			}
			fixture.files[fixture.hostsPath] = current
			cancel()
			return
		}
	}}
	if err := fixture.app.applyProjectRouteSet(ctx, project, routes, routes); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancelled route apply, got %v", err)
	}
	if !interleaved {
		t.Fatal("route cancellation fixture did not reach the vhost write boundary")
	}
	fixture.assertPreserved(t, true)
}

func TestSSLManagedRouteRollbackRestoresOwnHostsAndPreservesOtherProject(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := newSSLFixture(t, true, false)
	project := projectroutes.Project{Name: "demo", MainPath: filepath.Join(paths.Root(), "www", "demo"), TLD: "test", SSL: false}
	routes := []projectroutes.Route{
		{ID: "canonical", ProjectName: "demo", Canonical: true, Hostname: "demo.test", TargetPath: project.MainPath, TargetKind: projectroutes.TargetMainCheckout},
		{ID: "preview", ProjectName: "demo", Hostname: "preview.demo.test", TargetPath: project.MainPath, TargetKind: projectroutes.TargetWorktree},
	}
	originalHosts := append([]byte{}, fixture.files[fixture.hostsPath]...)
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	interleaved := false
	ctx := sslInterleavedContext{Context: base, beforeErr: func() {
		if interleaved {
			return
		}
		current, err := os.ReadFile(fixture.hostsPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(current, []byte(" demo.test ")) || !bytes.Contains(current, []byte(" preview.demo.test ")) {
			return
		}
		interleaved = true
		if err := fixture.app.hosts.AddEntry("127.0.0.2", "other.test"); err != nil {
			t.Fatal(err)
		}
		fixture.files[fixture.hostsPath] = append(originalHosts, []byte("127.0.0.2 other.test # lara4ever\n")...)
		cancel()
	}}
	if err := fixture.app.applyProjectRouteSet(ctx, project, routes, routes); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancelled route apply, got %v", err)
	}
	if !interleaved {
		t.Fatal("fixture did not reach canonical and preview hosts writes")
	}
	fixture.assertPreserved(t, true)
}

// Run a real, uniquely named fixture process, never an installed web server.
// Its original configuration serves a loopback socket. The replacement either
// fails to bind another reserved fixture socket or exits before serving it.
func TestSSLWebServerProcess(t *testing.T) {
	if os.Getenv("LARAENV_SSL_WEB_HELPER") != "1" {
		return
	}
	config, err := os.ReadFile(os.Getenv("LARAENV_SSL_WEB_CONFIG"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if !bytes.Contains(config, []byte("original SSL fixture")) {
		switch os.Getenv("LARAENV_SSL_WEB_FAILURE") {
		case "bind":
			// Leave enough time for cmd.Start to report success. The port is
			// held by this test, not another user or system service.
			time.Sleep(150 * time.Millisecond)
			listener, err := net.Listen("tcp", os.Getenv("LARAENV_SSL_WEB_BLOCKED"))
			if err != nil {
				fmt.Fprintln(os.Stderr, "fixture bind failed:", err)
				os.Exit(3)
			}
			listener.Close()
		case "exit":
			time.Sleep(150 * time.Millisecond)
			fmt.Fprintln(os.Stderr, "fixture web server exited before readiness")
			os.Exit(4)
		}
	}
	listener, err := net.Listen("tcp", os.Getenv("LARAENV_SSL_WEB_LISTEN"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(5)
	}
	defer listener.Close()
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		_, _ = connection.Write([]byte("SSL fixture is serving\n"))
		connection.Close()
	}
}

func startSSLWebServerFixture(t *testing.T, fixture sslFixture, failure string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	// Manager.Stop has an image-name fallback for an already stopped service.
	// Give this copy a unique name so cleanup can never target the test runner
	// or an actual nginx process, even on a failed restart.
	helper := filepath.Join(paths.Root(), "ssl-web-"+rand.Text()+".exe")
	if err := os.WriteFile(helper, data, 0700); err != nil {
		t.Fatal(err)
	}
	primary, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := primary.Addr().String()
	primary.Close()
	blocked, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { blocked.Close() })
	configPath := fixture.app.nginx.VhostPath("demo")
	before := []byte("# original SSL fixture\n")
	mcpPauseWrite(t, configPath, before)
	fixture.files[configPath] = before
	starts := 0
	fixture.app.services.RegisterDef(services.Definition{
		Name: "nginx", Label: "isolated SSL fixture", Path: helper,
		Args: []string{"-test.run=^TestSSLWebServerProcess$"},
		Env: map[string]string{
			"LARAENV_SSL_WEB_HELPER": "1", "LARAENV_SSL_WEB_CONFIG": configPath,
			"LARAENV_SSL_WEB_LISTEN": address, "LARAENV_SSL_WEB_BLOCKED": blocked.Addr().String(),
			"LARAENV_SSL_WEB_FAILURE": failure,
		},
		FirstRunCheck: func() bool { return true },
		FirstRunInit: func() error {
			starts++
			if failure == "start" && starts == 2 {
				return errors.New("fixture synchronous restart failure")
			}
			return nil
		},
	})
	t.Cleanup(func() {
		if err := fixture.app.services.Stop("nginx"); err != nil {
			t.Error(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for fixture.app.services.Status("nginx") == "running" && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if fixture.app.services.Status("nginx") != "stopped" {
			t.Error("fixture service was not reaped")
		}
	})
	if err := fixture.app.services.Start("nginx"); err != nil {
		t.Fatal(err)
	}
	assertSSLWebServerServing(t, address)
	return address
}

func assertSSLWebServerServing(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.SetReadDeadline(time.Now().Add(time.Second))
			response, readErr := io.ReadAll(connection)
			connection.Close()
			if readErr == nil && string(response) == "SSL fixture is serving\n" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("previously active fixture web server is not serving after SSL rollback")
}

func TestSetProjectSSLRestoresPreviouslyActiveServerAfterFailedApply(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	for _, failure := range []string{"bind", "exit", "start"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newSSLFixture(t, true, false)
			address := startSSLWebServerFixture(t, fixture, failure)
			if err := fixture.app.applyProjectSSL("demo", false); err == nil {
				t.Error("SSL transaction reported success before the replacement web server was serving")
			}
			fixture.assertPreserved(t, true)
			if fixture.app.services.Status("nginx") != "running" {
				t.Error("SSL rollback left the previously active web server stopped")
			}
			assertSSLWebServerServing(t, address)
		})
	}
}

func TestSetProjectSSLSuccessfulDisableKeepsActiveServerServing(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := newSSLFixture(t, true, false)
	address := startSSLWebServerFixture(t, fixture, "")
	if err := fixture.app.applyProjectSSL("demo", false); err != nil {
		t.Fatal(err)
	}
	assertSSLWebServerServing(t, address)
	if fixture.app.cfg.Get().Projects.SSL["demo"] {
		t.Fatal("successful disable did not commit its preference")
	}
	if fixture.app.services.Status("apache") != "stopped" || fixture.app.services.Status("caddy") != "stopped" {
		t.Fatal("SSL change started a previously stopped web server")
	}
	for _, suffix := range []string{".crt", ".key"} {
		if _, err := os.Stat(filepath.Join(paths.NginxCerts(), "demo.test"+suffix)); !os.IsNotExist(err) {
			t.Fatalf("successful disable did not remove leaf %s: %v", suffix, err)
		}
	}
}

func TestSSLReadinessRejectsAnotherProcessesListener(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := newSSLFixture(t, true, false)
	address := startSSLWebServerFixture(t, fixture, "")
	unrelated, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer unrelated.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// Only the test parent owns this port. A TCP connect alone would accept it
	// and conceal the same bind collision on HTTPS's port 443.
	err = fixture.app.waitSSLWebServers(ctx, []sslWebServerSnapshot{{name: "nginx", ports: []int{unrelated.Addr().(*net.TCPAddr).Port}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("another process's listener was accepted as SSL readiness: %v", err)
	}
	assertSSLWebServerServing(t, address)
	fixture.assertPreserved(t, true)
}

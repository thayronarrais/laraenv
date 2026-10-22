package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"lara4ever/internal/config"
	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/projects"
)

func mcpDiagnosticFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMCPDoctorChecksFixturesWithoutReadingSecrets(t *testing.T) {
	root := t.TempDir()
	mcpDiagnosticFile(t, filepath.Join(root, "php", "8.4", "php.exe"), "fixture")
	mcpDiagnosticFile(t, filepath.Join(root, "php", "8.4", "php-cgi.exe"), "fixture")
	mcpDiagnosticFile(t, filepath.Join(root, "nodejs", "22.1", "node.exe"), "fixture")
	mcpDiagnosticFile(t, filepath.Join(root, "nginx", "nginx.exe"), "fixture")
	path := filepath.Join(root, "www", "demo")
	if err := os.MkdirAll(filepath.Join(path, "public"), 0700); err != nil {
		t.Fatal(err)
	}
	mcpDiagnosticFile(t, filepath.Join(path, ".env"), "DB_PASSWORD=must-never-appear\nAPP_KEY=secret-key\n")
	project := projects.Project{Name: "demo", URL: "http://demo.test", Path: path, DocRoot: filepath.Join(path, "public"), Kind: "laravel", PhpVersion: "8.4", NodeVersion: "22.1"}
	report, err := mcpDoctor(context.Background(), root, []projects.Project{project}, map[string]string{"nginx": "running"})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Projects) != 1 || !report.Projects[0].DirectoryExists || !report.Projects[0].DocumentRootExists {
		t.Fatalf("invalid doctor result: %#v", report)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "must-never-appear") || strings.Contains(string(data), "secret-key") || strings.Contains(string(data), "DB_PASSWORD") {
		t.Fatal("doctor exposed secret configuration")
	}
	if len(report.Services) != 1 || report.Services[0].Status != "running" {
		t.Fatal(report.Services)
	}
}

func TestMCPDoctorReportsMissingProjectAndPinnedRuntime(t *testing.T) {
	root := t.TempDir()
	report, err := mcpDoctor(context.Background(), root, []projects.Project{{Name: "missing", Path: filepath.Join(root, "www", "missing"), DocRoot: filepath.Join(root, "www", "missing", "public"), PhpVersion: "8.4"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Projects[0].DirectoryExists || report.Projects[0].PhpAvailable || len(report.Projects[0].Issues) == 0 {
		t.Fatalf("missing checks not reported: %#v", report)
	}
}

func TestMCPDoctorUsesDetectedRuntimeAndReportsRequirementProblems(t *testing.T) {
	root := t.TempDir()
	mcpDiagnosticFile(t, filepath.Join(root, "php", "8.3", "php.exe"), "fixture")
	mcpDiagnosticFile(t, filepath.Join(root, "php", "8.3", "php-cgi.exe"), "fixture")
	mcpDiagnosticFile(t, filepath.Join(root, "nodejs", "22.1", "node.exe"), "fixture")
	path := filepath.Join(root, "www", "demo")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	project := projects.Project{Name: "demo", Path: path, PhpVersion: "8.3.15", PhpRuntime: projects.RuntimeResolution{Version: "8.3", FullVersion: "8.3.15", Source: "manual", Status: "incompatible", Constraint: "^8.4"}, NodeRuntime: projects.RuntimeResolution{Source: ".nvmrc", Status: "missing", Constraint: "99", Problem: "missing_runtime"}}
	report, err := mcpDoctor(context.Background(), root, []projects.Project{project}, nil)
	if err != nil {
		t.Fatal(err)
	}
	check := report.Projects[0]
	if !check.PhpAvailable || !check.PhpFpmAvailable || check.NodeAvailable || report.OK {
		t.Fatalf("doctor ignored effective runtimes: %+v", report)
	}
	if !strings.Contains(strings.Join(check.Issues, ";"), "incompatible") || !strings.Contains(strings.Join(check.Issues, ";"), "Node") {
		t.Fatalf("requirement diagnostics missing: %+v", check.Issues)
	}
}

func mcpTimingFixture(server *httptest.Server, record *string) mcpTimingDependencies {
	return mcpTimingDependencies{dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		if record != nil {
			*record = address
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
}

func TestMCPTimingDialsLoopbackWithProjectHostAndBoundsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "demo.test" || r.URL.Path != "/" {
			t.Errorf("wrong virtual host request: %s %s", r.Host, r.URL.String())
		}
		_, _ = w.Write([]byte(strings.Repeat("secret-response", 8192)))
	}))
	defer server.Close()
	var address string
	result, err := mcpTimeProject(context.Background(), projects.Project{Name: "demo", URL: "http://demo.test"}, mcpTimingFixture(server, &address))
	if err != nil {
		t.Fatal(err)
	}
	if address != "127.0.0.1:80" || result.StatusCode != 200 || !result.BodyTruncated || result.BodyBytesRead > 65537 {
		t.Fatalf("unbounded or non-local request: %s %#v", address, result)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), "secret-response") {
		t.Fatal("timing result included response data")
	}
}

func TestMCPTimingDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1); w.WriteHeader(200) }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer server.Close()
	_, err := mcpTimeProject(context.Background(), projects.Project{Name: "demo", URL: "http://demo.test"}, mcpTimingFixture(server, nil))
	if err == nil || redirected.Load() != 0 {
		t.Fatalf("redirect was not blocked: %v requests=%d", err, redirected.Load())
	}
}

func TestMCPTimingPreservesTLSHostnameVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" {
			t.Errorf("wrong TLS Host: %s", r.Host)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	deps := mcpTimingFixture(server, nil)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	deps.tlsConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	result, err := mcpTimeProject(context.Background(), projects.Project{Name: "demo", URL: "https://example.com"}, deps)
	if err != nil || !result.TLSVerified || result.StatusCode != 204 {
		t.Fatalf("TLS hostname verification failed: %#v %v", result, err)
	}
	if _, err := mcpTimeProject(context.Background(), projects.Project{Name: "demo", URL: "https://wrong.test"}, deps); err == nil {
		t.Fatal("TLS hostname mismatch accepted")
	}
}

func TestMCPTimingRejectsUnsafeCatalogURLsBeforeDial(t *testing.T) {
	for _, url := range []string{"file:///secret", "http://user:secret@demo.test", "http://demo.test/private?token=secret", "http://demo.test/#secret", "http://demo.test:70000"} {
		t.Run(url, func(t *testing.T) {
			deps := mcpTimingDependencies{dial: func(context.Context, string, string) (net.Conn, error) {
				t.Fatal("invalid URL reached network")
				return nil, errors.New("network forbidden")
			}}
			if _, err := mcpTimeProject(context.Background(), projects.Project{Name: "demo", URL: url}, deps); err == nil {
				t.Fatal("unsafe project URL accepted")
			}
		})
	}
}

func TestMCPTimingHonorsCancellationBeforeDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := mcpTimingDependencies{dial: func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("canceled request reached network")
		return nil, nil
	}}
	if _, err := mcpTimeProject(ctx, projects.Project{Name: "demo", URL: "http://demo.test"}, deps); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestMCPDoctorReportsMissingEnabledServiceAndPythonMetadata(t *testing.T) {
	root := t.TempDir()
	mcpDiagnosticFile(t, filepath.Join(root, "python", "3.13", "python.exe"), "fixture")
	report, err := mcpDoctor(context.Background(), root, nil, map[string]string{"mysql": "running"})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || len(report.Services) != 1 || report.Services[0].Installed {
		t.Fatalf("missing enabled binary was not reported: %#v", report)
	}
	for _, runtime := range report.Runtimes {
		if runtime.Name == "python" && runtime.Installed && len(runtime.Versions) == 1 && runtime.Versions[0] == "3.13" {
			return
		}
	}
	t.Fatal("installed Python runtime metadata was not reported")
}

func TestMCPDoctorReportsMissingPHPCGIForWebProject(t *testing.T) {
	root := t.TempDir()
	mcpDiagnosticFile(t, filepath.Join(root, "php", "8.4", "php.exe"), "fixture")
	path := filepath.Join(root, "www", "demo")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	report, err := mcpDoctor(context.Background(), root, []projects.Project{{Name: "demo", Path: path, DocRoot: path, Kind: "wordpress", PhpVersion: "8.4"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || len(report.Projects[0].Issues) == 0 {
		t.Fatalf("missing PHP CGI binary was not reported: %#v", report)
	}
}

func TestMCPDoctorHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mcpDoctor(ctx, t.TempDir(), nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("doctor ignored cancellation: %v", err)
	}
}

func TestMCPTimingUsesExplicitVhostPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "demo.test:8080" {
			t.Errorf("wrong Host header: %s", r.Host)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	var address string
	result, err := mcpTimeProject(context.Background(), projects.Project{Name: "demo", URL: "http://demo.test:8080"}, mcpTimingFixture(server, &address))
	if err != nil || address != "127.0.0.1:8080" || result.StatusCode != 204 {
		t.Fatalf("wrong explicit local port: %s %#v %v", address, result, err)
	}
}

func TestMCPTimingHonorsCancellationDuringRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cancel(); <-r.Context().Done() }))
	defer server.Close()
	if _, err := mcpTimeProject(ctx, projects.Project{Name: "demo", URL: "http://demo.test"}, mcpTimingFixture(server, nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight cancellation lost: %v", err)
	}
}

func TestMCPTimingBoundsResponseHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Large", strings.Repeat("secret", 8192))
		w.WriteHeader(200)
	}))
	defer server.Close()
	_, err := mcpTimeProject(context.Background(), projects.Project{Name: "demo", URL: "http://demo.test"}, mcpTimingFixture(server, nil))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unbounded or unredacted headers accepted: %v", err)
	}
}

func TestMCPDoctorDoesNotCreateMissingProjectsDirectory(t *testing.T) {
	root := t.TempDir()
	store, err := config.NewStore(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings := store.Get().Projects
	settings.Root = filepath.Join(root, "missing-projects")
	if err := store.SetProjects(settings); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: store, projects: projects.NewScanner(store)}
	if _, err := app.mcpDiagnostics(context.Background(), mcpbridge.Args{Action: "doctor"}, nil); err == nil {
		t.Fatal("missing projects directory accepted")
	}
	if _, err := os.Stat(settings.Root); !os.IsNotExist(err) {
		t.Fatalf("doctor created the projects directory: %v", err)
	}
}

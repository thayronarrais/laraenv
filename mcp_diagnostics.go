package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/paths"
	"lara4ever/internal/projects"
)

func (a *App) mcpDiagnostics(ctx context.Context, input mcpbridge.Args, progress func(string)) (any, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if a.projects == nil {
		return nil, errors.New("project catalogue is unavailable")
	}
	// Scanner.List creates an absent projects root. A diagnostic request must
	// report that condition without causing the scanner's repair side effect.
	if !mcpDiagnosticDirectory(a.projects.Root()) {
		return nil, errors.New("configured projects directory is unavailable")
	}
	switch input.Action {
	case "doctor":
		var list []projects.Project
		if input.Project != "" {
			project, err := a.mcpProject(input.Project)
			if err != nil {
				return nil, err
			}
			list = []projects.Project{project}
		} else {
			var err error
			list, err = a.ListProjects()
			if err != nil {
				return nil, errors.New("could not inspect project catalogue")
			}
		}
		if a.services == nil {
			return nil, errors.New("local service manager is unavailable")
		}
		statuses := map[string]string{}
		for _, service := range a.ListServices() {
			if input.Service == "" || input.Service == service.Name {
				statuses[service.Name] = a.ServiceStatus(service.Name)
			}
		}
		if input.Service != "" && len(statuses) == 0 {
			return nil, errors.New("service is not enabled in LaraEnv")
		}
		if progress != nil {
			progress("checking installed runtimes, services and projects")
		}
		return mcpDoctor(ctx, paths.Root(), list, statuses)
	case "request_timing", "timing":
		project, err := a.mcpProject(input.Project)
		if err != nil {
			return nil, err
		}
		if progress != nil {
			progress("timing the local project virtual host")
		}
		return mcpTimeProject(ctx, project, mcpTimingDependencies{})
	default:
		return nil, errors.New("diagnostic action must be doctor or request_timing")
	}
}

type mcpDoctorProject struct {
	Name               string                     `json:"name"`
	DirectoryExists    bool                       `json:"directoryExists"`
	DocumentRootExists bool                       `json:"documentRootExists"`
	PhpAvailable       bool                       `json:"phpAvailable"`
	PhpFpmAvailable    bool                       `json:"phpFpmAvailable"`
	NodeAvailable      bool                       `json:"nodeAvailable"`
	PhpRuntime         projects.RuntimeResolution `json:"phpRuntime"`
	NodeRuntime        projects.RuntimeResolution `json:"nodeRuntime"`
	Issues             []string                   `json:"issues"`
}
type mcpDoctorService struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Installed bool   `json:"installed"`
}
type mcpDoctorRuntime struct {
	Name      string   `json:"name"`
	Installed bool     `json:"installed"`
	Versions  []string `json:"versions"`
}
type mcpDoctorReport struct {
	OK       bool               `json:"ok"`
	Projects []mcpDoctorProject `json:"projects"`
	Services []mcpDoctorService `json:"services"`
	Runtimes []mcpDoctorRuntime `json:"runtimes"`
}
type mcpTimingDependencies struct {
	dial      func(context.Context, string, string) (net.Conn, error)
	tlsConfig *tls.Config
}
type mcpRequestTiming struct {
	Project       string  `json:"project"`
	URL           string  `json:"url"`
	StatusCode    int     `json:"statusCode"`
	TotalMS       float64 `json:"totalMs"`
	ConnectMS     float64 `json:"connectMs"`
	TLSMS         float64 `json:"tlsMs"`
	FirstByteMS   float64 `json:"firstByteMs"`
	BodyBytesRead int64   `json:"bodyBytesRead"`
	BodyTruncated bool    `json:"bodyTruncated"`
	TLSVerified   bool    `json:"tlsVerified"`
}

var mcpRuntimeVersion = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+){0,3}$`)

func mcpDiagnosticRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func mcpDiagnosticDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func mcpDiagnosticVersions(root, executable string) []string {
	versions := []string{}
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if mcpRuntimeVersion.MatchString(entry.Name()) && mcpDiagnosticRegularFile(filepath.Join(root, entry.Name(), executable)) {
			versions = append(versions, entry.Name())
		}
	}
	sort.Strings(versions)
	return versions
}

func mcpDiagnosticVersionAvailable(version string, versions []string) bool {
	if version == "" {
		return len(versions) > 0
	}
	for _, installed := range versions {
		if version == installed {
			return true
		}
	}
	return false
}

func mcpDiagnosticRuntimeAvailable(runtime projects.RuntimeResolution, legacyVersion string, versions []string) bool {
	if runtime.Status == "" {
		return mcpDiagnosticVersionAvailable(legacyVersion, versions)
	}
	// An unresolved declaration cannot use an unrelated installed default. A
	// manual override may still have an effective version alongside a warning.
	return runtime.Version != "" && mcpDiagnosticVersionAvailable(runtime.Version, versions)
}

func mcpDiagnosticRuntimeIssue(name string, runtime projects.RuntimeResolution) string {
	switch runtime.Status {
	case "missing", "invalid", "incompatible":
		issue := fmt.Sprintf("%s runtime %s (source: %q, constraint: %q", name, runtime.Status, runtime.Source, runtime.Constraint)
		if runtime.Problem != "" {
			issue += fmt.Sprintf(", problem: %q", runtime.Problem)
		}
		return issue + ")"
	default:
		return ""
	}
}

// Doctor inspects file metadata only. In particular, PHP resolution helpers may
// repair configuration or php.ini, so diagnostics never call those helpers.
func mcpDoctor(ctx context.Context, root string, list []projects.Project, statuses map[string]string) (mcpDoctorReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return mcpDoctorReport{}, err
	}
	php := mcpDiagnosticVersions(filepath.Join(root, "php"), "php.exe")
	phpCGI := mcpDiagnosticVersions(filepath.Join(root, "php"), "php-cgi.exe")
	node := mcpDiagnosticVersions(filepath.Join(root, "nodejs"), "node.exe")
	python := mcpDiagnosticVersions(filepath.Join(root, "python"), "python.exe")
	installed := map[string]bool{
		"php":      len(php) > 0,
		"node":     len(node) > 0,
		"python":   len(python) > 0,
		"nginx":    mcpDiagnosticRegularFile(filepath.Join(root, "nginx", "nginx.exe")),
		"apache":   mcpDiagnosticRegularFile(filepath.Join(root, "apache", "bin", "httpd.exe")),
		"caddy":    mcpDiagnosticRegularFile(filepath.Join(root, "caddy", "caddy.exe")),
		"mysql":    mcpDiagnosticRegularFile(filepath.Join(root, "mysql", "bin", "mysqld.exe")),
		"postgres": mcpDiagnosticRegularFile(filepath.Join(root, "postgres", "bin", "postgres.exe")),
		"redis":    mcpDiagnosticRegularFile(filepath.Join(root, "redis", "redis-server.exe")),
		"mailpit":  mcpDiagnosticRegularFile(filepath.Join(root, "mailpit", "mailpit.exe")),
		"composer": mcpDiagnosticRegularFile(filepath.Join(root, "composer", "composer.phar")),
	}
	report := mcpDoctorReport{OK: true, Projects: []mcpDoctorProject{}, Services: []mcpDoctorService{}, Runtimes: []mcpDoctorRuntime{}}
	for _, name := range []string{"php", "node", "python", "composer", "nginx", "apache", "caddy", "mysql", "postgres", "redis", "mailpit"} {
		versions := []string{}
		if name == "php" {
			versions = php
		}
		if name == "node" {
			versions = node
		}
		if name == "python" {
			versions = python
		}
		report.Runtimes = append(report.Runtimes, mcpDoctorRuntime{Name: name, Installed: installed[name], Versions: versions})
	}
	for name, status := range statuses {
		if err := ctx.Err(); err != nil {
			return mcpDoctorReport{}, err
		}
		available := installed[name]
		if strings.HasPrefix(name, "php-fpm:") {
			version := strings.TrimPrefix(name, "php-fpm:")
			available = mcpRuntimeVersion.MatchString(version) && mcpDiagnosticRegularFile(filepath.Join(root, "php", version, "php-cgi.exe"))
		}
		if status != "running" {
			status = "stopped"
		}
		report.Services = append(report.Services, mcpDoctorService{Name: name, Status: status, Installed: available})
		if !available {
			report.OK = false
		}
	}
	sort.Slice(report.Services, func(i, j int) bool { return report.Services[i].Name < report.Services[j].Name })
	for _, project := range list {
		if err := ctx.Err(); err != nil {
			return mcpDoctorReport{}, err
		}
		check := mcpDoctorProject{
			Name: project.Name, DirectoryExists: mcpDiagnosticDirectory(project.Path),
			PhpAvailable:    mcpDiagnosticRuntimeAvailable(project.PhpRuntime, project.PhpVersion, php),
			PhpFpmAvailable: mcpDiagnosticRuntimeAvailable(project.PhpRuntime, project.PhpVersion, phpCGI),
			NodeAvailable:   mcpDiagnosticRuntimeAvailable(project.NodeRuntime, project.NodeVersion, node),
			PhpRuntime:      project.PhpRuntime, NodeRuntime: project.NodeRuntime, Issues: []string{},
		}
		docRoot := project.DocRoot
		if docRoot == "" {
			docRoot = project.Path
		}
		check.DocumentRootExists = mcpDiagnosticDirectory(docRoot)
		if !check.DirectoryExists {
			check.Issues = append(check.Issues, "project directory is missing")
		}
		if !check.DocumentRootExists {
			check.Issues = append(check.Issues, "document root is missing")
		}
		phpIssue := mcpDiagnosticRuntimeIssue("PHP", project.PhpRuntime)
		nodeIssue := mcpDiagnosticRuntimeIssue("Node", project.NodeRuntime)
		if phpIssue != "" {
			check.Issues = append(check.Issues, phpIssue)
		}
		if nodeIssue != "" {
			check.Issues = append(check.Issues, nodeIssue)
		}
		if !check.PhpAvailable && phpIssue == "" && (project.PhpRuntime.Version != "" || project.PhpVersion != "" || project.Kind == "laravel" || project.Kind == "wordpress") {
			check.Issues = append(check.Issues, "required PHP runtime is missing")
		}
		if check.PhpAvailable && !check.PhpFpmAvailable && (project.Kind == "laravel" || project.Kind == "wordpress") {
			check.Issues = append(check.Issues, "required PHP CGI binary is missing")
		}
		if !check.NodeAvailable && nodeIssue == "" && (project.NodeRuntime.Version != "" || project.NodeVersion != "" || project.JSFramework != "" || project.PackageManager != "") {
			check.Issues = append(check.Issues, "required Node runtime is missing")
		}
		if len(check.Issues) > 0 {
			report.OK = false
		}
		report.Projects = append(report.Projects, check)
	}
	sort.Slice(report.Projects, func(i, j int) bool { return report.Projects[i].Name < report.Projects[j].Name })
	return report, nil
}

const mcpTimingBodyLimit = 64 * 1024

// The URL must come from mcpProject, never from a request parameter. The socket
// destination is fixed loopback while HTTP Host and TLS SNI retain the vhost.
func mcpTimeProject(ctx context.Context, project projects.Project, deps mcpTimingDependencies) (mcpRequestTiming, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return mcpRequestTiming{}, err
	}
	target, err := url.Parse(project.URL)
	if err != nil || target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" || target.User != nil || target.RawQuery != "" || target.ForceQuery || target.Fragment != "" || (target.Path != "" && target.Path != "/") || target.RawPath != "" {
		return mcpRequestTiming{}, errors.New("project URL must be an HTTP or HTTPS virtual host without credentials, query or fragment")
	}
	port := target.Port()
	if port == "" {
		port = "80"
		if target.Scheme == "https" {
			port = "443"
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return mcpRequestTiming{}, errors.New("project URL has an invalid port")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	dial := deps.dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: 3 * time.Second}).DialContext
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if deps.tlsConfig != nil {
		tlsConfig = deps.tlsConfig.Clone()
	}
	// Never allow an injected trust store to turn off certificate/hostname checks.
	tlsConfig.InsecureSkipVerify = false
	tlsConfig.ServerName = target.Hostname()
	transport := &http.Transport{
		Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 32 * 1024,
		ResponseHeaderTimeout: 10 * time.Second, TLSHandshakeTimeout: 5 * time.Second, TLSClientConfig: tlsConfig,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dial(ctx, network, net.JoinHostPort("127.0.0.1", port))
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("project redirects are not followed") }}
	start := time.Now()
	var mu sync.Mutex
	var connectStart, tlsStart time.Time
	var connectDuration, tlsDuration, firstByteDuration time.Duration
	trace := &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) { mu.Lock(); connectStart = time.Now(); mu.Unlock() },
		ConnectDone: func(_, _ string, _ error) {
			mu.Lock()
			if !connectStart.IsZero() {
				connectDuration = time.Since(connectStart)
			}
			mu.Unlock()
		},
		TLSHandshakeStart: func() { mu.Lock(); tlsStart = time.Now(); mu.Unlock() },
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			mu.Lock()
			if !tlsStart.IsZero() {
				tlsDuration = time.Since(tlsStart)
			}
			mu.Unlock()
		},
		GotFirstResponseByte: func() { mu.Lock(); firstByteDuration = time.Since(start); mu.Unlock() },
	}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, target.String(), nil)
	if err != nil {
		return mcpRequestTiming{}, errors.New("could not prepare project timing request")
	}
	request.Header.Set("User-Agent", "LaraEnv-MCP/1")
	response, err := client.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if ctx.Err() != nil {
			return mcpRequestTiming{}, ctx.Err()
		}
		return mcpRequestTiming{}, errors.New("local project request failed; verify its web server and TLS certificate")
	}
	defer response.Body.Close()
	read, err := io.Copy(io.Discard, io.LimitReader(response.Body, mcpTimingBodyLimit+1))
	if err != nil {
		if ctx.Err() != nil {
			return mcpRequestTiming{}, ctx.Err()
		}
		return mcpRequestTiming{}, errors.New("could not read local project response")
	}
	if ctx.Err() != nil {
		return mcpRequestTiming{}, ctx.Err()
	}
	mu.Lock()
	result := mcpRequestTiming{Project: project.Name, URL: target.String(), StatusCode: response.StatusCode, TotalMS: float64(time.Since(start)) / float64(time.Millisecond), ConnectMS: float64(connectDuration) / float64(time.Millisecond), TLSMS: float64(tlsDuration) / float64(time.Millisecond), FirstByteMS: float64(firstByteDuration) / float64(time.Millisecond), BodyBytesRead: read, BodyTruncated: read > mcpTimingBodyLimit, TLSVerified: response.TLS != nil && len(response.TLS.VerifiedChains) > 0}
	mu.Unlock()
	return result, nil
}

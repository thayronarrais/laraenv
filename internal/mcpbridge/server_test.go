package mcpbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMain(m *testing.M) {
	if path := os.Getenv("LARAENV_MCP_TEST_CONNECTION"); path != "" {
		if err := RunProxy(context.Background(), path, "test"); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type backendFunc func(context.Context, string, Args, func(string)) (any, error)

func (f backendFunc) Call(ctx context.Context, group string, args Args, progress func(string)) (any, error) {
	return f(ctx, group, args, progress)
}

func startTestServer(t *testing.T, backend Backend, audit func(AuditEvent)) *Server {
	t.Helper()
	s, err := NewServer(Options{Backend: backend, DescriptorPath: filepath.Join(t.TempDir(), "connection.json"), Version: "test", Audit: audit})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAuthenticatedProtocolAndAudit(t *testing.T) {
	var mu sync.Mutex
	var events []AuditEvent
	s := startTestServer(t, backendFunc(func(_ context.Context, group string, args Args, progress func(string)) (any, error) {
		if group != "project" || args.Action != "detail" || args.Project != "shop" {
			t.Errorf("unexpected call %s %+v", group, args)
		}
		progress("Resolved project")
		return map[string]any{"name": "shop", "running": true}, nil
	}), func(event AuditEvent) { mu.Lock(); events = append(events, event); mu.Unlock() })
	d, err := ReadDescriptor(s.Status().DescriptorPath)
	if err != nil {
		t.Fatal(err)
	}
	progress := make(chan string, 1)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, &mcp.ClientOptions{ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) { progress <- req.Params.Message }})
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: d.URL, HTTPClient: descriptorClient(d)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 8 {
		t.Fatalf("tools = %+v, error %v", tools, err)
	}
	params := &mcp.CallToolParams{Name: "laraenv_project", Arguments: Args{Action: "detail", Project: "shop"}}
	params.SetProgressToken("test-progress")
	result, err := session.CallTool(t.Context(), params)
	if err != nil || result.IsError {
		t.Fatalf("result = %+v, error %v", result, err)
	}
	if !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "shop") {
		t.Fatalf("unexpected content: %+v", result.Content)
	}
	select {
	case message := <-progress:
		if message != "Resolved project" {
			t.Fatal(message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing progress")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || events[0].Group != "project" || !events[0].Success || events[0].ID == "" {
		t.Fatalf("audit: %+v", events)
	}
}

func TestHTTPGuardsRejectBeforeBackend(t *testing.T) {
	s := startTestServer(t, backendFunc(func(context.Context, string, Args, func(string)) (any, error) {
		t.Error("unauthorized backend call")
		return nil, nil
	}), nil)
	d, _ := ReadDescriptor(s.Status().DescriptorPath)
	for _, tc := range []struct {
		name, token, host, origin, body string
		status                          int
	}{
		{"missing auth", "", "", "", `{}`, http.StatusUnauthorized},
		{"wrong auth", "wrong", "", "", `{}`, http.StatusUnauthorized},
		{"host", d.Token, "attacker.test", "", `{}`, http.StatusForbidden},
		{"origin", d.Token, "", "https://attacker.test", `{}`, http.StatusForbidden},
		{"body limit", d.Token, "", "", strings.Repeat("x", maxRequestBytes+1), http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, d.URL, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+tc.token)
			req.Header.Set("Content-Type", "application/json")
			if tc.host != "" {
				req.Host = tc.host
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != tc.status {
				t.Fatalf("status %d, want %d: %s", res.StatusCode, tc.status, body)
			}
			if strings.Contains(string(body), d.Token) {
				t.Fatal("token leaked")
			}
		})
	}
}

func TestDescriptorRejectsRemoteEndpointsAndCleanupPreservesNewOwner(t *testing.T) {
	s := startTestServer(t, backendFunc(func(context.Context, string, Args, func(string)) (any, error) { return nil, nil }), nil)
	path := s.Status().DescriptorPath
	d, _ := ReadDescriptor(path)
	for _, endpoint := range []string{"https://example.com/mcp", "http://localhost:1234/mcp", "http://127.0.0.1:1234/mcp?token=x", "http://127.0.0.1:1234/other", "http://user:pass@127.0.0.1:1234/mcp"} {
		d.URL = endpoint
		if err := d.validate(); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	d, _ = ReadDescriptor(path)
	d.Token = strings.Repeat("a", 43)
	b, _ := json.Marshal(d)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("removed a descriptor owned by a newer server: %v", err)
	}
}

func TestProxyForwardsResultsProgressAndCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	s := startTestServer(t, backendFunc(func(ctx context.Context, _ string, args Args, progress func(string)) (any, error) {
		progress("Started")
		if args.Project == "wait" {
			started <- struct{}{}
			<-ctx.Done()
			cancelled <- struct{}{}
			return nil, ctx.Err()
		}
		return map[string]string{"name": args.Project}, nil
	}), nil)
	proxy, closeProxy, err := newProxy(t.Context(), s.Status().DescriptorPath, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := proxy.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	progress := make(chan string, 8)
	client := mcp.NewClient(&mcp.Implementation{Name: "proxy-test", Version: "1"}, &mcp.ClientOptions{ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) { progress <- req.Params.Message }})
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	p := &mcp.CallToolParams{Name: "laraenv_project", Arguments: Args{Action: "detail", Project: "shop"}}
	p.SetProgressToken("outer")
	result, err := session.CallTool(t.Context(), p)
	if err != nil || result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "shop") {
		t.Fatalf("result %+v, err %v", result, err)
	}
	select {
	case <-progress:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy lost progress")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "laraenv_project", Arguments: Args{Action: "detail", Project: "wait"}})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("backend not started")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("call not cancelled")
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("backend not cancelled")
	}
}

func TestSchemaRejectsDestructiveMissingConfirmation(t *testing.T) {
	s := startTestServer(t, backendFunc(func(context.Context, string, Args, func(string)) (any, error) {
		t.Error("unconfirmed destructive backend call")
		return nil, nil
	}), nil)
	d, _ := ReadDescriptor(s.Status().DescriptorPath)
	client := mcp.NewClient(&mcp.Implementation{Name: "schema-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: d.URL, HTTPClient: descriptorClient(d)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, p := range []*mcp.CallToolParams{
		{Name: "laraenv_db", Arguments: Args{Action: "drop", Engine: "mysql", Database: "shop"}},
		{Name: "laraenv_worker", Arguments: Args{Action: "run", Kind: "cron", ID: "job"}},
		{Name: "laraenv_worker", Arguments: Args{Action: "start", Kind: "cron", ID: "job"}},
		{Name: "laraenv_worker", Arguments: Args{Action: "start", Kind: "devserver", Project: "shop"}},
		{Name: "laraenv_worker", Arguments: Args{Action: "restart", Kind: "devserver", Project: "shop"}},
		{Name: "laraenv_worker", Arguments: Args{Action: "start", Kind: "queue", Project: "shop"}},
		{Name: "laraenv_worker", Arguments: Args{Action: "restart", Kind: "queue", Project: "shop"}},
		{Name: "laraenv_worker", Arguments: Args{Action: "start", Kind: "queue", Project: "shop", Confirm: true, Args: []string{"--queue=emails"}}},
		{Name: "laraenv_exec", Arguments: Args{Action: "artisan", Project: "shop", Command: "evil"}},
	} {
		res, err := session.CallTool(t.Context(), p)
		if err != nil || !res.IsError {
			t.Fatalf("expected tool error: %+v %v", res, err)
		}
	}
}

func TestProxyPreservesExplicitEmptyOverrides(t *testing.T) {
	calls := make(chan Args, 2)
	s := startTestServer(t, backendFunc(func(_ context.Context, _ string, args Args, _ func(string)) (any, error) {
		calls <- args
		return map[string]bool{"ok": true}, nil
	}), nil)
	proxy, closeProxy, err := newProxy(t.Context(), s.Status().DescriptorPath, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	a, b := mcp.NewInMemoryTransports()
	ss, err := proxy.Connect(t.Context(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "clear-test", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, args := range []map[string]any{
		{"action": "set_php", "project": "shop", "version": ""},
		{"action": "set_proxy", "project": "shop", "port": 0},
	} {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "laraenv_project", Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("clear: %+v %v", res, err)
		}
	}
	if len(calls) != 2 {
		t.Fatal("clear overrides were not delivered")
	}
}

func TestRunProxyUsesOnlyStdioProtocol(t *testing.T) {
	s := startTestServer(t, backendFunc(func(_ context.Context, _ string, args Args, _ func(string)) (any, error) {
		return map[string]string{"name": args.Project}, nil
	}), nil)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "LARAENV_MCP_TEST_CONNECTION="+s.Status().DescriptorPath)
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "laraenv_project", Arguments: Args{Action: "detail", Project: "shop"}})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "shop") {
		t.Fatalf("stdio result %+v, err %v", res, err)
	}
}

func TestFailedCallIsAuditedWithoutOutputOrCommand(t *testing.T) {
	audit := make(chan AuditEvent, 1)
	s := startTestServer(t, backendFunc(func(context.Context, string, Args, func(string)) (any, error) {
		return nil, errors.New("operation failed")
	}), func(e AuditEvent) { audit <- e })
	d, _ := ReadDescriptor(s.Status().DescriptorPath)
	client := mcp.NewClient(&mcp.Implementation{Name: "audit-test", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: d.URL, HTTPClient: descriptorClient(d)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "laraenv_exec", Arguments: Args{Action: "npm", Project: "shop", Args: []string{"secret-value"}}})
	if err != nil || !res.IsError {
		t.Fatalf("expected error: %+v %v", res, err)
	}
	e := <-audit
	b, _ := json.Marshal(e)
	if e.Success || strings.Contains(string(b), "secret-value") || strings.Contains(string(b), "operation failed") {
		t.Fatalf("unsafe audit: %s", b)
	}
}

func TestFailedCommandPreservesOutput(t *testing.T) {
	s := startTestServer(t, backendFunc(func(context.Context, string, Args, func(string)) (any, error) {
		return map[string]any{"output": "syntax error line 5", "exitCode": 1}, errors.New("command exited with status 1")
	}), nil)
	d, _ := ReadDescriptor(s.Status().DescriptorPath)
	client := mcp.NewClient(&mcp.Implementation{Name: "failed-output", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: d.URL, HTTPClient: descriptorClient(d)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "laraenv_exec", Arguments: Args{Action: "npm", Project: "shop", Args: []string{"run", "build"}}})
	if err != nil || !res.IsError || res.StructuredContent == nil {
		t.Fatalf("failure output lost: %+v %v", res, err)
	}
	if !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "syntax error line 5") {
		t.Fatal("failure output missing")
	}
}

func TestInvalidCallsAreAuditedWithoutUntrustedFields(t *testing.T) {
	events := make(chan AuditEvent, 2)
	s := startTestServer(t, backendFunc(func(context.Context, string, Args, func(string)) (any, error) {
		t.Error("invalid request reached backend")
		return nil, nil
	}), func(e AuditEvent) { events <- e })
	d, _ := ReadDescriptor(s.Status().DescriptorPath)
	client := mcp.NewClient(&mcp.Implementation{Name: "invalid-audit", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: d.URL, HTTPClient: descriptorClient(d)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "laraenv_project", Arguments: map[string]any{"action": "secret-token", "project": "another-secret"}})
	if err != nil || !res.IsError {
		t.Fatalf("expected validation error %+v %v", res, err)
	}
	select {
	case e := <-events:
		b, _ := json.Marshal(e)
		if e.Success || strings.Contains(string(b), "secret") {
			t.Fatalf("unsafe rejected-call audit: %s", b)
		}
	case <-time.After(time.Second):
		t.Fatal("rejected call not audited")
	}
}

func TestWorkerStatusAcceptsGlobalScope(t *testing.T) {
	calls := make(chan Args, 3)
	s := startTestServer(t, backendFunc(func(_ context.Context, _ string, args Args, _ func(string)) (any, error) {
		calls <- args
		return []any{}, nil
	}), nil)
	d, _ := ReadDescriptor(s.Status().DescriptorPath)
	client := mcp.NewClient(&mcp.Implementation{Name: "global-workers", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: d.URL, HTTPClient: descriptorClient(d)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, kind := range []string{"devserver", "queue", "cron"} {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "laraenv_worker", Arguments: Args{Action: "status", Kind: kind}})
		if err != nil || res.IsError {
			t.Fatalf("global status %+v %v", res, err)
		}
	}
	if len(calls) != 3 {
		t.Fatal("global statuses not delivered")
	}
}

func TestHTTPChunkedBodyLimit(t *testing.T) {
	s := startTestServer(t, backendFunc(func(context.Context, string, Args, func(string)) (any, error) {
		t.Error("oversized backend call")
		return nil, nil
	}), nil)
	d, _ := ReadDescriptor(s.Status().DescriptorPath)
	// A reader without a known length forces Transfer-Encoding: chunked.
	body := io.LimitReader(strings.NewReader(strings.Repeat(" ", maxRequestBytes+1)), maxRequestBytes+1)
	req, _ := http.NewRequest(http.MethodPost, d.URL, body)
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		data, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d: %s", res.StatusCode, data)
	}
}

func TestProxySendsRejectedInputsToCentralAudit(t *testing.T) {
	events := make(chan AuditEvent, 1)
	s := startTestServer(t, backendFunc(func(context.Context, string, Args, func(string)) (any, error) {
		t.Error("invalid request reached backend")
		return nil, nil
	}), func(e AuditEvent) { events <- e })
	proxy, closeProxy, err := newProxy(t.Context(), s.Status().DescriptorPath, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	a, b := mcp.NewInMemoryTransports()
	ss, err := proxy.Connect(t.Context(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "rejected-proxy", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "laraenv_project", Arguments: map[string]any{"action": "invalid-secret"}})
	if err != nil || !res.IsError {
		t.Fatalf("expected error: %+v %v", res, err)
	}
	select {
	case e := <-events:
		if e.Success || e.Action != "invalid" {
			t.Fatalf("audit %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy rejected input was not centrally audited")
	}
}

// Opt-in smoke of the actual packaged executable. A fresh installation root
// proves that the CLI branch does not initialize GUI/config/service managers.
func TestReleaseBinaryStdioSmoke(t *testing.T) {
	binary := os.Getenv("LARAENV_MCP_SMOKE_BINARY")
	if binary == "" {
		t.Skip("set LARAENV_MCP_SMOKE_BINARY to an absolute release executable")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("release smoke executable must be absolute")
	}
	progress := make(chan string, 4)
	s := startTestServer(t, backendFunc(func(_ context.Context, group string, args Args, emit func(string)) (any, error) {
		if group == "project" && args.Action == "list" {
			return []map[string]string{{"name": "mcp-smoke-fixture"}}, nil
		}
		if group == "exec" && args.Action == "npm" {
			emit("smoke-progress")
			return map[string]any{"output": "fake-runtime-output", "exitCode": 0}, nil
		}
		t.Errorf("unexpected release smoke call %s %+v", group, args)
		return nil, errors.New("unexpected smoke call")
	}), nil)
	freshRoot := t.TempDir()
	cmd := exec.Command(binary, "mcp", "--connection", s.Status().DescriptorPath)
	cmd.Env = append(os.Environ(), "LARAENV_ROOT="+freshRoot)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "release-smoke", Version: "1"}, &mcp.ClientOptions{ProgressNotificationHandler: func(_ context.Context, r *mcp.ProgressNotificationClientRequest) { progress <- r.Params.Message }})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("release CLI did not initialize MCP: %v; stderr: %s", err, stderr.String())
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 8 {
		t.Fatalf("release tools %+v %v", tools, err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "laraenv_project", Arguments: Args{Action: "list"}})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "mcp-smoke-fixture") {
		t.Fatalf("release result %+v %v", res, err)
	}
	p := &mcp.CallToolParams{Name: "laraenv_exec", Arguments: Args{Action: "npm", Project: "mcp-smoke-fixture", Args: []string{"--version"}}}
	p.SetProgressToken("release-smoke-progress")
	res, err = cs.CallTool(ctx, p)
	if err != nil || res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "fake-runtime-output") {
		t.Fatalf("release exec result %+v %v", res, err)
	}
	select {
	case message := <-progress:
		if message != "smoke-progress" {
			t.Fatal(message)
		}
	case <-ctx.Done():
		t.Fatal("release progress was lost")
	}
	if err := cs.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(freshRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("MCP CLI initialized application files in fresh root: %+v", entries)
	}
}

func TestAuditIncludesValidatedDatabaseAndCronTargets(t *testing.T) {
	events := make(chan AuditEvent, 3)
	s := startTestServer(t, backendFunc(func(context.Context, string, Args, func(string)) (any, error) {
		return map[string]bool{"ok": true}, nil
	}), func(e AuditEvent) { events <- e })
	d, _ := ReadDescriptor(s.Status().DescriptorPath)
	client := mcp.NewClient(&mcp.Implementation{Name: "target-audit", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: d.URL, HTTPClient: descriptorClient(d)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, p := range []*mcp.CallToolParams{
		{Name: "laraenv_db", Arguments: Args{Action: "create", Engine: "mysql", Database: "shop"}},
		{Name: "laraenv_worker", Arguments: Args{Action: "status", Kind: "cron", ID: "job-123"}},
	} {
		res, err := cs.CallTool(t.Context(), p)
		if err != nil || res.IsError {
			t.Fatalf("call: %+v %v", res, err)
		}
	}
	db, cron := <-events, <-events
	if db.Database != "shop" || db.Engine != "mysql" || db.JobID != "" {
		t.Fatalf("database audit %+v", db)
	}
	if cron.JobID != "job-123" || cron.Database != "" || cron.Engine != "" {
		t.Fatalf("cron audit %+v", cron)
	}
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "laraenv_db", Arguments: map[string]any{"action": "create", "engine": "secret-token", "database": "rejected-secret"}})
	if err != nil || !res.IsError {
		t.Fatalf("expected schema error %+v %v", res, err)
	}
	rejected := <-events
	if rejected.Database != "" || rejected.Engine != "" || rejected.JobID != "" {
		t.Fatalf("rejected input leaked into audit %+v", rejected)
	}
}

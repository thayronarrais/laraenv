package main

import (
	"context"
	"errors"
	"lara4ever/internal/config"
	"lara4ever/internal/i18n"
	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/projects"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func assertSameFilesystemPath(t *testing.T, actual, expected string) {
	t.Helper()
	actualInfo, err := os.Stat(actual)
	if err != nil {
		t.Fatalf("actual path %q: %v", actual, err)
	}
	expectedInfo, err := os.Stat(expected)
	if err != nil {
		t.Fatalf("expected path %q: %v", expected, err)
	}
	if !os.SameFile(actualInfo, expectedInfo) {
		t.Fatalf("paths identify different files or directories: actual=%q expected=%q", actual, expected)
	}
}

func TestMCPProjectScopeRejectsTraversalAndUnknownProjects(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "demo"), 0700)
	store, e := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if e != nil {
		t.Fatal(e)
	}
	p := store.Get().Projects
	p.Root = dir
	store.SetProjects(p)
	a := &App{cfg: store, projects: projects.NewScanner(store)}
	for _, name := range []string{"../demo", `..\demo`, dir, "missing", "demo/child"} {
		if _, e = a.mcpProject(name); e == nil {
			t.Errorf("accepted %q", name)
		}
	}
	got, e := a.mcpProject("demo")
	if e != nil {
		t.Fatal(e)
	}
	assertSameFilesystemPath(t, got.Path, filepath.Join(dir, "demo"))
	if _, e = (appMCPBackend{a}).Call(context.Background(), "unknown", mcpbridge.Args{Action: "list"}, nil); e == nil {
		t.Fatal("unknown group accepted")
	}
}

func TestMCPExecConfirmationAndLiteralArguments(t *testing.T) {
	for _, args := range [][]string{{"show", `-dC:\outside`}, {"show", `-vdC:\outside`}, {"show", "--working-d=outside"}} {
		if e := validateMCPExec("composer", args, false); e == nil {
			t.Fatalf("accepted scope escape %v", args)
		}
	}
	for _, args := range [][]string{{"list", "-Coutside"}, {"list", "--location=global"}} {
		if e := validateMCPExec("npm", args, false); e == nil {
			t.Fatalf("accepted npm scope escape %v", args)
		}
	}
	for _, tc := range []struct {
		kind string
		args []string
	}{{"artisan", []string{"migrate:fresh"}}, {"artisan", []string{"db:wipe"}}, {"artisan", []string{"migrate:rollback"}}, {"npm", []string{"run", "reset"}}, {"composer", []string{"run-script", "reset"}}} {
		if err := validateMCPExec(tc.kind, tc.args, false); err == nil {
			t.Errorf("missing confirmation for %v", tc)
		}
		if err := validateMCPExec(tc.kind, tc.args, true); err != nil {
			t.Errorf("confirmed rejected %v: %v", tc, err)
		}
	}
	if err := validateMCPExec("artisan", []string{"route:list"}, false); err != nil {
		t.Fatal(err)
	}
	if err := validateMCPExec("powershell", []string{"echo"}, true); err == nil {
		t.Fatal("arbitrary executable accepted")
	}
	if err := validateMCPExec("npm", []string{"run", "build", "--", "a & b"}, true); err != nil {
		t.Fatal("literal argv rejected", err)
	}
	if err := validateMCPExec("artisan", []string{"list\x00bad"}, true); err == nil {
		t.Fatal("NUL accepted")
	}
}

func TestMCPLogTailBoundsAndRejectsEscapingLog(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "storage", "logs"), 0700)
	path := filepath.Join(root, "storage", "logs", "laravel.log")
	os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0600)
	out, e := mcpAppLog(root, 2)
	if e != nil || strings.Join(out, "|") != "two|three" {
		t.Fatalf("%v %v", out, e)
	}
	out = mcpTail([]string{strings.Repeat("x", 20000), "last"}, 1000)
	if len(out) != 2 || len(out[0]) > 8192 {
		t.Fatal("log output unbounded")
	}
	if _, e = mcpAppLog(root, -1); e == nil {
		t.Fatal("negative line count accepted")
	}
}

func TestMCPCLIValidatesConnectionArguments(t *testing.T) {
	if _, e := mcpCLIConnection([]string{"app", "mcp", "--connection", "relative"}); e == nil {
		t.Fatal("relative connection accepted")
	}
	path := filepath.Join(t.TempDir(), "connection.json")
	got, e := mcpCLIConnection([]string{"app", "mcp", "--connection", path})
	if e != nil || got != path {
		t.Fatal(got, e)
	}
}

func TestPausedProjectRejectsWorkerStartsBeforeManagers(t *testing.T) {
	store, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetProjectPaused("demo", true); err != nil {
		t.Fatal(err)
	}
	// Managers and scanner intentionally stay nil: a paused project must be
	// rejected before any runtime lookup, process start or queue-args write.
	a := &App{cfg: store}
	for _, tc := range []struct {
		name string
		call func() (int, error)
	}{
		{"devserver", func() (int, error) { return a.StartDevServer("demo") }},
		{"queue-with-args", func() (int, error) { return a.StartQueueWorkerWithArgs("demo", "--queue=emails") }},
		{"queue-restart", func() (int, error) { return a.RestartQueueWorker("demo") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pid, err := tc.call()
			var localized *i18n.Error
			if pid != 0 || !errors.As(err, &localized) || localized.Key != "errors.app.projectPaused" {
				t.Fatalf("paused start: pid=%d error=%v", pid, err)
			}
		})
	}
	if got := store.Get().Projects.QueueArgs["demo"]; got != "" {
		t.Fatalf("paused start persisted queue args: %q", got)
	}
}

func TestMCPWorkerStartRequiresConfirmationBeforeManagers(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "demo"), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := config.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := store.Get().Projects
	p.Root = root
	if err = store.SetProjects(p); err != nil {
		t.Fatal(err)
	}
	a := &App{cfg: store, projects: projects.NewScanner(store)}
	for _, kind := range []string{"queue", "devserver"} {
		for _, action := range []string{"start", "restart"} {
			t.Run(kind+"/"+action, func(t *testing.T) {
				_, err := a.mcpWorkers(mcpbridge.Args{Action: action, Kind: kind, Project: "demo"})
				if err == nil || !strings.Contains(err.Error(), "confirm:true") {
					t.Fatalf("unconfirmed worker call: %v", err)
				}
			})
		}
	}
}

func TestMCPMutationGateWaitRespectsCancellation(t *testing.T) {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	a := &App{mcpGate: gate}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (appMCPBackend{a}).Call(ctx, "service", mcpbridge.Args{Action: "start", Service: "nginx"}, nil)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("occupied mutation gate did not wait: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("gate cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mutation gate ignored cancellation")
	}
	if len(gate) != 1 {
		t.Fatal("cancelled waiter released another call's gate slot")
	}
}

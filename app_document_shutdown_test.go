package main

import (
	"context"
	"errors"
	"fmt"
	"lara4ever/internal/sourcecontrol"
	"lara4ever/internal/workspacefiles"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"lara4ever/internal/documentshutdown"
	"lara4ever/internal/i18n"
)

func TestUpdateIsGatedBeforeApply(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failure], func(t *testing.T) {
			calls, quits, shown := 0, 0, 0
			var requests []documentshutdown.RequestEvent
			var notices []string
			a := &App{documentEffects: documentShutdownEffects{
				emit: func(e documentshutdown.RequestEvent) { requests = append(requests, e) },
				show: func() { shown++ }, quit: func() { quits++ }, hide: func(context.Context) {},
				apply: func(path string) error {
					calls++
					if path != "test.msi" {
						t.Error(path)
					}
					if failure {
						return errors.New("fixture apply error")
					}
					return nil
				},
				afterApply: func(fn func()) { fn() }, notice: func(s string) { notices = append(notices, s) },
			}}
			if err := a.InstallUpdate("test.msi"); err != nil {
				t.Fatal(err)
			}
			if calls != 0 || quits != 0 || len(requests) != 0 {
				t.Fatal("acted before ready")
			}
			if err := a.SetDocumentWorkspaceState(DocumentWorkspaceState{Ready: true, DirtyCount: 1, StateVersion: 1}); err != nil {
				t.Fatal(err)
			}
			if len(requests) != 1 || shown != 1 {
				t.Fatal(requests, shown)
			}
			if _, err := a.ResolveDocumentShutdown(DocumentShutdownReply{RequestID: requests[0].RequestID, Decision: "cancel", StateVersion: 1}); err != nil {
				t.Fatal(err)
			}
			if calls != 0 || quits != 0 {
				t.Fatal("canceled update applied")
			}
			_ = a.SetDocumentWorkspaceState(DocumentWorkspaceState{Ready: true, StateVersion: 2})
			if err := a.InstallUpdate("test.msi"); err != nil {
				t.Fatal(err)
			}
			a.QuitApp() // repeated/cross-intent retains update
			if calls != 0 || len(requests) != 2 {
				t.Fatal("bypassed clean ACK")
			}
			outcome, err := a.ResolveDocumentShutdown(DocumentShutdownReply{RequestID: requests[1].RequestID, Decision: "allow", StateVersion: 2})
			if calls != 1 {
				t.Fatal(calls)
			}
			if failure {
				want := i18n.T("updates.flash.installFailed", map[string]any{"error": "fixture apply error"})
				if err != nil || !outcome.Released || quits != 0 || len(notices) != 1 || notices[0] != want {
					t.Fatal(err, quits, notices)
				}
				if a.quitting.Load() {
					t.Fatal("failed apply permits close")
				}
			} else if err != nil || outcome.Released || quits != 1 || len(notices) != 0 {
				t.Fatal(err, quits, notices)
			}
		})
	}
}

func TestMainCloseStillHides(t *testing.T) {
	hidden, quits := 0, 0
	var request documentshutdown.RequestEvent
	a := &App{documentEffects: documentShutdownEffects{hide: func(context.Context) { hidden++ }, quit: func() { quits++ }, show: func() {}, emit: func(e documentshutdown.RequestEvent) { request = e }}}
	if !a.beforeClose(context.Background()) || hidden != 1 {
		t.Fatal("main X must hide")
	}
	a.QuitApp()
	if quits != 0 || !a.beforeClose(context.Background()) {
		t.Fatal("pending quit changed X")
	}
	_ = a.SetDocumentWorkspaceState(DocumentWorkspaceState{Ready: true, StateVersion: 1})
	if request.Intent != "quit" || quits != 0 {
		t.Fatal(request, quits)
	}
	if _, err := a.ResolveDocumentShutdown(DocumentShutdownReply{RequestID: request.RequestID, Decision: "allow", StateVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if quits != 1 || a.beforeClose(context.Background()) {
		t.Fatal("authorized quit must close")
	}
	for _, role := range []string{"detached", "commandbar"} {
		a := &App{detached: role == "detached", commandBar: role == "commandbar", documentEffects: documentShutdownEffects{quit: func() { quits++ }}}
		if a.beforeClose(context.Background()) {
			t.Fatal(role)
		}
		a.QuitApp()
	}
	if quits != 3 {
		t.Fatal(quits)
	}
}

// Real temporary Git work makes the App gate observable without replacing service admission.
func appGitFixture(t *testing.T, emit sourcecontrol.OperationEmitter) (*sourcecontrol.Service, sourcecontrol.FilesRequest) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	git("init", "-b", "main")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("disk\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ref := workspacefiles.ContextRef{Kind: "agent", SessionID: "shutdown"}
	ws := workspacefiles.NewService(func(workspacefiles.ContextRef) (string, string, error) { return dir, "shutdown", nil })
	s := sourcecontrol.NewService(ws.OpenContext, emit)
	t.Cleanup(s.Close)
	r := s.Inspect(context.Background(), ref)
	if r.Code != "ok" {
		t.Fatal(r)
	}
	snap := r.Snapshot
	req := sourcecontrol.FilesRequest{Repository: snap.Repository, Expected: sourcecontrol.Expectation{SnapshotRevision: snap.SnapshotRevision, IndexRevision: snap.IndexRevision, HeadOID: snap.HeadOID, Branch: snap.Branch}, Entries: []sourcecontrol.EntrySelection{{EntryID: snap.Entries[0].ID}}}
	return s, req
}
func blockedAppGit(t *testing.T) (*sourcecontrol.Service, func()) {
	t.Helper()
	ready, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s, req := appGitFixture(t, func(e sourcecontrol.OperationEvent) {
		if e.State == "running" && e.Stream == "" {
			once.Do(func() { close(ready); <-release })
		}
	})
	done := make(chan sourcecontrol.Result, 1)
	go func() { done <- s.StartStage(context.Background(), req) }()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("operation did not register")
	}
	var finish sync.Once
	cleanup := func() {
		finish.Do(func() {
			close(release)
			r := <-done
			if r.Code != "ok" {
				t.Error(r)
			}
			deadline := time.Now().Add(10 * time.Second)
			for s.Busy().Busy && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if s.Busy().Busy {
				t.Error("job did not drain")
			}
		})
	}
	t.Cleanup(cleanup)
	return s, cleanup
}

// Early refusal must neither emit a document request nor call installer/quit.
func TestGitBusyRejectsQuitAndUpdate(t *testing.T) {
	s, _ := blockedAppGit(t)
	effects, requests, notices, hidden := 0, 0, 0, 0
	a := &App{sourceControl: s, documentEffects: documentShutdownEffects{quit: func() { effects++ }, apply: func(string) error { effects++; return nil }, show: func() {}, emit: func(documentshutdown.RequestEvent) { requests++ }, notice: func(string) { notices++ }, hide: func(context.Context) { hidden++ }}}
	_ = a.SetDocumentWorkspaceState(DocumentWorkspaceState{Ready: true, StateVersion: 1})
	a.QuitApp()
	err := a.InstallUpdate("fixture.msi")
	if err == nil || effects != 0 || requests != 0 || notices != 2 || a.quitting.Load() {
		t.Fatalf("busy bypass: %v %d %d %d", err, effects, requests, notices)
	}
	if !a.beforeClose(context.Background()) || hidden != 1 || !s.Busy().Busy {
		t.Fatal("tray cancelled work")
	}
}

// A job can register after the clean ACK request, before its continuation runs.
func TestGitStartsBetweenDocumentAckAndContinuation(t *testing.T) {
	for _, intent := range []string{"quit", "update"} {
		t.Run(intent, func(t *testing.T) {
			effects, notices := 0, 0
			var request documentshutdown.RequestEvent
			a := &App{documentEffects: documentShutdownEffects{quit: func() { effects++ }, apply: func(string) error { effects++; return nil }, show: func() {}, emit: func(e documentshutdown.RequestEvent) { request = e }, notice: func(string) { notices++ }}}
			_ = a.SetDocumentWorkspaceState(DocumentWorkspaceState{Ready: true, StateVersion: 1})
			if intent == "quit" {
				a.QuitApp()
			} else {
				if err := a.InstallUpdate("fixture.msi"); err != nil {
					t.Fatal(err)
				}
			}
			s, _ := blockedAppGit(t)
			a.sourceControl = s
			outcome, err := a.ResolveDocumentShutdown(DocumentShutdownReply{RequestID: request.RequestID, Decision: "allow", StateVersion: 1})
			if err != nil || !outcome.Released || effects != 0 || notices != 1 || a.quitting.Load() {
				t.Fatalf("late busy bypass: %#v %v effects=%d notices=%d", outcome, err, effects, notices)
			}
		})
	}
}

func TestGitUpdateRetainsReservationAndFailureReleases(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			s, req := appGitFixture(t, nil)
			var timer func()
			quits := 0
			a := &App{sourceControl: s, commandBar: true}
			a.documentEffects = documentShutdownEffects{notice: func(string) {}, apply: func(string) error {
				if r := s.StartStage(context.Background(), req); r.Code != "shutting_down" {
					t.Errorf("start during installer: %#v", r)
				}
				if failure {
					return errors.New("launch failed")
				}
				return nil
			}, afterApply: func(fn func()) { timer = fn }, quit: func() {
				quits++
				if r := s.StartStage(context.Background(), req); r.Code != "shutting_down" {
					t.Errorf("start during quit: %#v", r)
				}
			}}
			err := a.InstallUpdate("fixture.msi")
			if failure {
				if err == nil || timer != nil || a.quitting.Load() {
					t.Fatal("failed launch retained effect")
				}
				r := s.StartStage(context.Background(), req)
				if r.Code != "ok" {
					t.Fatalf("failed launch leaked reservation: %#v", r)
				}
				deadline := time.Now().Add(10 * time.Second)
				for s.Busy().Busy && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
				return
			}
			if err != nil || timer == nil {
				t.Fatal(err)
			}
			if r := s.StartStage(context.Background(), req); r.Code != "shutting_down" {
				t.Fatal("handoff gap", r)
			}
			timer()
			if quits != 1 || !a.quitting.Load() {
				t.Fatal("timer reacquired reservation instead of quitting")
			}
		})
	}
}

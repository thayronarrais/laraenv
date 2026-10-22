package main

import (
	"context"
	"encoding/json"
	"lara4ever/internal/sourcecontrol"
	"lara4ever/internal/workspacefiles"
	"os"
	"strings"
	"testing"
)

func TestSourceControlContextFacade(t *testing.T) {
	var received workspacefiles.ContextRef
	ws := workspacefiles.NewService(func(ref workspacefiles.ContextRef) (string, string, error) {
		received = ref
		return "", "", os.ErrNotExist
	})
	a := &App{ctx: context.Background(), sourceControl: sourcecontrol.NewService(ws.OpenContext, nil)}
	ref := workspacefiles.ContextRef{Kind: "agent", SessionID: "session-A"}
	r := a.InspectSourceControl(ref)
	if r.Code != "unavailable" || received != ref {
		t.Fatalf("%#v %#v", r, received)
	}
	encoded, err := json.Marshal(sourcecontrol.RepositoryRef{Context: ref, RootKey: "root", RepositoryKey: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"context":{"kind":"agent","sessionId":"session-A","projectName":""},"rootKey":"root","repositoryKey":"repo"}` {
		t.Fatalf("wrong facade DTO: %s", encoded)
	}
}

func TestSourceControlDiffFacade(t *testing.T) {
	if got := (&App{}).ReadSourceControlDiff(sourcecontrol.DiffRequest{}); got.Code != "unavailable" {
		t.Fatalf("%#v", got)
	}
	var received workspacefiles.ContextRef
	ws := workspacefiles.NewService(func(ref workspacefiles.ContextRef) (string, string, error) {
		received = ref
		return "", "", os.ErrNotExist
	})
	a := &App{ctx: context.Background(), sourceControl: sourcecontrol.NewService(ws.OpenContext, nil)}
	ref := workspacefiles.ContextRef{Kind: "agent", SessionID: "session-diff"}
	got := a.ReadSourceControlDiff(sourcecontrol.DiffRequest{Repository: sourcecontrol.RepositoryRef{Context: ref, RootKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RepositoryKey: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, Layer: "worktree"})
	if got.Code != "repository_changed" || received != ref {
		t.Fatalf("wrong authority: %#v %#v", got, received)
	}
	data, err := json.Marshal(sourcecontrol.Diff{Repository: sourcecontrol.RepositoryRef{Context: ref}, EntryID: "entry", CanOpenWorktree: true, Modified: sourcecontrol.DiffSide{Revision: "disk", OID: "blob", EOL: "lf", BOM: true}})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if string(wire["entryId"]) != `"entry"` || string(wire["canOpenWorktree"]) != "true" || wire["repository"] == nil {
		t.Fatalf("diff wire fields: %s", data)
	}
}

func TestSourceControlOperationFacade(t *testing.T) {
	a := &App{}
	if a.StartSourceControlStage(sourcecontrol.FilesRequest{}).Code != "unavailable" || a.StartSourceControlUnstage(sourcecontrol.FilesRequest{}).Code != "unavailable" || a.GetSourceControlOperation("id").Code != "unavailable" || a.CancelSourceControlOperation("id").Code != "unavailable" {
		t.Fatal("unavailable facade did not return a structured problem")
	}
	if busy := a.GetSourceControlBusy(); busy.Busy || busy.OperationIDs == nil {
		t.Fatalf("invalid empty busy state: %#v", busy)
	}
	var received workspacefiles.ContextRef
	ws := workspacefiles.NewService(func(ref workspacefiles.ContextRef) (string, string, error) {
		received = ref
		return "", "", os.ErrNotExist
	})
	a.sourceControl = sourcecontrol.NewService(ws.OpenContext, nil)
	req := sourcecontrol.FilesRequest{Repository: sourcecontrol.RepositoryRef{Context: workspacefiles.ContextRef{Kind: "agent", SessionID: "operations"}, RootKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RepositoryKey: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, Entries: []sourcecontrol.EntrySelection{{EntryID: "entry"}}}
	for _, start := range []func(sourcecontrol.FilesRequest) sourcecontrol.Result{a.StartSourceControlStage, a.StartSourceControlUnstage} {
		received = workspacefiles.ContextRef{}
		if result := start(req); result.Code != "repository_changed" || received != req.Repository.Context {
			t.Fatalf("authority changed: %#v %#v", result, received)
		}
	}
	if a.GetSourceControlOperation("missing").Code != "operation_unknown" || a.CancelSourceControlOperation("missing").Code != "operation_unknown" {
		t.Fatal("registry result lost")
	}
}

func TestSourceControlSatelliteMutationRejected(t *testing.T) {
	for _, role := range []string{"detached", "commandbar"} {
		opened := false
		ws := workspacefiles.NewService(func(workspacefiles.ContextRef) (string, string, error) { opened = true; return "", "", os.ErrNotExist })
		a := &App{sourceControl: sourcecontrol.NewService(ws.OpenContext, nil), detached: role == "detached", commandBar: role == "commandbar"}
		req := sourcecontrol.FilesRequest{Repository: sourcecontrol.RepositoryRef{RootKey: strings.Repeat("a", 64), RepositoryKey: strings.Repeat("b", 64)}, Entries: []sourcecontrol.EntrySelection{{EntryID: "entry"}}}
		for _, start := range []func(sourcecontrol.FilesRequest) sourcecontrol.Result{a.StartSourceControlStage, a.StartSourceControlUnstage} {
			if r := start(req); r.Code != "unavailable" || opened {
				t.Fatalf("%s mutates: %#v opened=%v", role, r, opened)
			}
		}
		api, ok := any(a).(interface {
			StartSourceControlCommit(sourcecontrol.CommitRequest) sourcecontrol.Result
			StartSourceControlPush(sourcecontrol.PushRequest) sourcecontrol.Result
		})
		if !ok {
			t.Fatal("commit/push facade missing")
		}
		if api.StartSourceControlCommit(sourcecontrol.CommitRequest{Repository: req.Repository, Message: "msg"}).Code != "unavailable" || api.StartSourceControlPush(sourcecontrol.PushRequest{Repository: req.Repository}).Code != "unavailable" || opened {
			t.Fatal("satellite admitted commit/push")
		}
	}
}

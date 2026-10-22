package main

import (
	"context"
	"lara4ever/internal/sourcecontrol"
	"lara4ever/internal/workspacefiles"
)

func (a *App) sourceControlContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}
func (a *App) InspectSourceControl(source workspacefiles.ContextRef) sourcecontrol.Result {
	if a.sourceControl == nil {
		return sourcecontrol.Result{Code: "unavailable", Message: "Source control unavailable"}
	}
	return a.sourceControl.Inspect(a.sourceControlContext(), source)
}
func (a *App) GetSourceControlStatus(ref sourcecontrol.RepositoryRef) sourcecontrol.Result {
	if a.sourceControl == nil {
		return sourcecontrol.Result{Code: "unavailable", Message: "Source control unavailable"}
	}
	return a.sourceControl.Status(a.sourceControlContext(), ref)
}

func (a *App) ReadSourceControlDiff(req sourcecontrol.DiffRequest) sourcecontrol.Result {
	if a.sourceControl == nil {
		return sourcecontrol.Result{Code: "unavailable", Message: "Source control unavailable"}
	}
	return a.sourceControl.ReadDiff(a.sourceControlContext(), req)
}

func (a *App) StartSourceControlStage(req sourcecontrol.FilesRequest) sourcecontrol.Result {
	if a.detached || a.commandBar {
		return sourcecontrol.Result{Code: "unavailable", Message: "Source control mutations require the main window"}
	}
	if a.sourceControl == nil {
		return sourcecontrol.Result{Code: "unavailable", Message: "Source control unavailable"}
	}
	return a.sourceControl.StartStage(a.sourceControlContext(), req)
}
func (a *App) StartSourceControlUnstage(req sourcecontrol.FilesRequest) sourcecontrol.Result {
	if a.detached || a.commandBar {
		return sourcecontrol.Result{Code: "unavailable", Message: "Source control mutations require the main window"}
	}
	if a.sourceControl == nil {
		return sourcecontrol.Result{Code: "unavailable", Message: "Source control unavailable"}
	}
	return a.sourceControl.StartUnstage(a.sourceControlContext(), req)
}
func (a *App) GetSourceControlOperation(id string) sourcecontrol.Result {
	if a.sourceControl == nil {
		return sourcecontrol.Result{Code: "unavailable", Message: "Source control unavailable"}
	}
	return a.sourceControl.GetOperation(id)
}
func (a *App) CancelSourceControlOperation(id string) sourcecontrol.Result {
	if a.sourceControl == nil {
		return sourcecontrol.Result{Code: "unavailable", Message: "Source control unavailable"}
	}
	return a.sourceControl.CancelOperation(id)
}
func (a *App) GetSourceControlBusy() sourcecontrol.BusyState {
	if a.sourceControl == nil {
		return sourcecontrol.BusyState{OperationIDs: []string{}}
	}
	return a.sourceControl.Busy()
}

func (a *App) StartSourceControlCommit(req sourcecontrol.CommitRequest) sourcecontrol.Result {
	if a.detached || a.commandBar || a.sourceControl == nil {
		return sourcecontrol.Result{Code: "unavailable", Message: "Source control mutations require the main window"}
	}
	return a.sourceControl.StartCommit(a.sourceControlContext(), req)
}
func (a *App) StartSourceControlPush(req sourcecontrol.PushRequest) sourcecontrol.Result {
	if a.detached || a.commandBar || a.sourceControl == nil {
		return sourcecontrol.Result{Code: "unavailable", Message: "Source control mutations require the main window"}
	}
	return a.sourceControl.StartPush(a.sourceControlContext(), req)
}
func (a *App) reserveSourceControlShutdown() (func(), error) {
	if a.sourceControl == nil {
		return func() {}, nil
	}
	release, err := a.sourceControl.TryBeginShutdown()
	if err != nil {
		a.sourceControlBusyNotice()
	}
	return release, err
}

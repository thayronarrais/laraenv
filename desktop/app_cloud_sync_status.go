package main

import (
	"context"
	"fmt"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"lara4ever/internal/i18n"
)

func (a *App) beginCloudSyncSession() (context.Context, context.CancelFunc, error) {
	ctx, cancel, cl, err := a.cloud.BeginSession(a.ctx)
	if err != nil {
		return nil, nil, err
	}
	var reason error
	switch {
	case cl.Token == "":
		reason = i18n.Errorf("errors.app.notLoggedIn", nil)
	case cl.Plan != "pro":
		reason = i18n.Errorf("errors.app.syncRequiresProPlan", nil)
	case cl.SyncKey == "":
		reason = i18n.Errorf("errors.app.noSyncKeySignInAgain", nil)
	case !cl.SyncEnabled:
		reason = i18n.Errorf("errors.app.syncDisabled", nil)
	}
	if reason != nil {
		cancel()
		return nil, nil, reason
	}
	return ctx, cancel, nil
}

func (a *App) emitHostedDeployment(ctx context.Context, event string, value any) {
	// A numeric ID may exist in several accounts. A cancelled origin must never
	// feed a listener newly installed for the same ID in another profile.
	if ctx.Err() != nil {
		return
	}
	if a.deploymentEmit != nil {
		a.deploymentEmit(event, value)
	} else if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, event, value)
	}
}

// Report cached sync state without making another HTTP request. Optional cloud
// UI subscribes only while enabled; local saves never inherit remote failures.
func (a *App) reportCloudSyncStatus() {
	if a.cloudSyncEmit != nil && a.cloud != nil {
		a.cloudSyncEmit(a.cloud.SyncStatusSnapshot())
	}
}

func (a *App) maybeSyncDeleteResource(kind, id string) {
	if !a.syncShouldRun() {
		return
	}
	ctx, cancel, err := a.beginCloudSyncSession()
	if err != nil {
		return
	}
	go func() {
		defer cancel()
		defer a.reportCloudSyncStatus()
		if err := a.cloud.DeleteResource(ctx, kind, id); err != nil {
			fmt.Printf("[cloud] delete %s %s: %v\n", kind, id, err)
		}
	}()
}

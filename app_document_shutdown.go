package main

import (
	"context"
	"errors"
	"time"

	"lara4ever/internal/documentshutdown"
	"lara4ever/internal/i18n"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Concrete App DTOs keep the generated Wails namespace stable.
type DocumentWorkspaceState struct {
	Ready        bool   `json:"ready"`
	DirtyCount   int    `json:"dirtyCount"`
	StateVersion uint64 `json:"stateVersion"`
}
type DocumentShutdownReply struct {
	RequestID    string `json:"requestId"`
	Decision     string `json:"decision"`
	StateVersion uint64 `json:"stateVersion"`
}

// Only the actual process/window/installer boundaries are injected in tests.
type documentShutdownEffects struct {
	emit       func(documentshutdown.RequestEvent)
	show       func()
	hide       func(context.Context)
	quit       func()
	apply      func(string) error
	afterApply func(func())
	notice     func(string)
}

func (a *App) documentShutdownGate() *documentshutdown.Gate {
	a.documentGateOnce.Do(func() {
		a.documentGate = documentshutdown.New(func(event documentshutdown.RequestEvent) {
			if a.documentEffects.show != nil {
				a.documentEffects.show()
			} else {
				a.bringToFront()
			}
			if a.documentEffects.emit != nil {
				a.documentEffects.emit(event)
			} else {
				wruntime.EventsEmit(a.ctx, "documents:shutdown-request", event)
			}
		})
	})
	return a.documentGate
}

func (a *App) SetDocumentWorkspaceState(state DocumentWorkspaceState) error {
	return a.documentShutdownGate().UpdateState(documentshutdown.State(state))
}

// Released distinguishes terminal continuation failure from a rejected stale reply.
// The update continuation already emits its localized failure notice.
type DocumentShutdownOutcome struct {
	Released bool `json:"released"`
}

func (a *App) ResolveDocumentShutdown(reply DocumentShutdownReply) (DocumentShutdownOutcome, error) {
	err := a.documentShutdownGate().Resolve(documentshutdown.Reply(reply))
	if released, ok := err.(interface{ Released() bool }); ok && released.Released() {
		return DocumentShutdownOutcome{Released: true}, nil
	}
	return DocumentShutdownOutcome{}, err
}

func (a *App) quitAfterDocuments() error {
	if _, err := a.reserveSourceControlShutdown(); err != nil {
		return err
	}
	return a.quitWithSourceControlReservation()
}

func (a *App) quitWithSourceControlReservation() error {
	a.quitting.Store(true)
	if a.documentEffects.quit != nil {
		a.documentEffects.quit()
	} else {
		wruntime.Quit(a.ctx)
	}
	return nil
}

func (a *App) applyUpdateAfterDocuments(downloaded string) error {
	release, err := a.reserveSourceControlShutdown()
	if err != nil {
		return err
	}
	apply := a.documentEffects.apply
	if apply == nil {
		apply = a.updater.Apply
	}
	if err := apply(downloaded); err != nil {
		release()
		notice := i18n.T("updates.flash.installFailed", map[string]any{"error": err.Error()})
		if a.documentEffects.notice != nil {
			a.documentEffects.notice(notice)
		} else {
			wruntime.EventsEmit(a.ctx, "app:notice", notice)
		}
		return err
	}
	quit := func() { _ = a.quitWithSourceControlReservation() }
	if a.documentEffects.afterApply != nil {
		a.documentEffects.afterApply(quit)
	} else {
		time.AfterFunc(500*time.Millisecond, quit)
	}
	return nil
}

func (a *App) hideMainWindow(ctx context.Context) {
	if a.documentEffects.hide != nil {
		a.documentEffects.hide(ctx)
	} else {
		wruntime.WindowHide(ctx)
	}
}

func (a *App) sourceControlBusyNotice() error {
	notice := i18n.T("backend.sourceControl.busyShutdown", nil)
	if a.documentEffects.notice != nil {
		a.documentEffects.notice(notice)
	} else if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "app:notice", notice)
	}
	return errors.New(notice)
}

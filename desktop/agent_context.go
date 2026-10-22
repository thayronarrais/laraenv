package main

import (
	"lara4ever/internal/i18n"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// SelectAgentContextDirectory returns an existing folder chosen for a session.
// Cancelling the native dialog returns an empty path without changing context.
func (a *App) SelectAgentContextDirectory() (string, error) {
	if err := a.requireAgentSessionsEnabled(); err != nil {
		return "", err
	}
	if a.ctx == nil {
		return "", i18n.Errorf("errors.app.windowNotReady", nil)
	}
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: i18n.T("backend.app.addFolderToAgentSessionTitle", nil),
	})
}

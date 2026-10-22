package main

import "lara4ever/internal/clipboard"

// ReadClipboardText avoids browser focus/permission restrictions and the
// Windows GlobalUnlock bug in Wails 2.12's built-in clipboard reader.
func (a *App) ReadClipboardText() (string, error) { return clipboard.ReadText(a.ctx) }

func (a *App) WriteClipboardText(text string) error { return clipboard.WriteText(a.ctx, text) }

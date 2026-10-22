package main

import (
	"context"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Win32 procs to restore + focus the main window. We deliberately avoid the
// WindowSetAlwaysOnTop toggle for this: it corrupts WebView2 on repeated
// hide/show cycles and the window paints black. SetForegroundWindow (the same
// approach the tray's "Show Window" uses) is reliable.
var (
	winUser32          = syscall.NewLazyDLL("user32.dll")
	winKernel32        = syscall.NewLazyDLL("kernel32.dll")
	winFindWindowW     = winUser32.NewProc("FindWindowW")
	winShowWindow      = winUser32.NewProc("ShowWindow")
	winSetForeground   = winUser32.NewProc("SetForegroundWindow")
	winAllowSetFG      = winUser32.NewProc("AllowSetForegroundWindow")
	winGetForeground   = winUser32.NewProc("GetForegroundWindow")
	winGetWndThreadPID = winUser32.NewProc("GetWindowThreadProcessId")
	winAttachThreadIn  = winUser32.NewProc("AttachThreadInput")
	winBringToTop      = winUser32.NewProc("BringWindowToTop")
	winSetFocus        = winUser32.NewProc("SetFocus")
	winSysParamsInfo   = winUser32.NewProc("SystemParametersInfoW")
	winKeybdEvent      = winUser32.NewProc("keybd_event")
	winGetCurThreadID  = winKernel32.NewProc("GetCurrentThreadId")
)

const (
	vkMenu          = 0x12 // ALT
	keyeventfKeyUp  = 0x02
)

const (
	spiGetForegroundLockTimeout = 0x2000
	spiSetForegroundLockTimeout = 0x2001
	spifSendChange              = 0x2
)

// allowForeground grants the given process the right to call
// SetForegroundWindow (Windows blocks foreground-stealing from processes that
// didn't originate the user input). Called by the main process — which holds
// foreground rights at global-hotkey time — for the spawned command bar.
func allowForeground(pid int) {
	if pid > 0 {
		winAllowSetFG.Call(uintptr(pid))
	}
}

// focusWindowByTitle force-activates a top-level window (matched by exact
// title) so its webview input receives keystrokes — a single best-effort
// attempt. See focusWindowByTitleUntilForeground for the retrying variant used
// at command-bar startup.
func focusWindowByTitle(title string) {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	hwnd, _, _ := winFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return
	}
	activateWindow(hwnd)
}

// focusWindowByTitleUntilForeground keeps trying to activate the window (matched
// by exact title) until it actually becomes the foreground window or the
// deadline elapses. A freshly-spawned WebView2 process can take several hundred
// ms to create its host HWND, so a couple of fixed early attempts routinely miss
// (SetForegroundWindow silently fails and only flashes the taskbar). Re-finding
// + retrying with a foreground check waits out init and stops the moment focus
// lands, so the bar is type-ready without a mouse click.
func focusWindowByTitleUntilForeground(title string, deadline time.Duration) {
	// Pin to one OS thread for the whole sequence. AttachThreadInput,
	// keybd_event, GetCurrentThreadId and SetForegroundWindow are thread-affine;
	// without this the Go scheduler can migrate the goroutine between OS threads
	// mid-sequence, so the thread we attach is not the thread that calls
	// SetForegroundWindow and the foreground-steal silently no-ops. (hotkey.loop
	// locks its thread for the same reason.)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	const interval = 80 * time.Millisecond
	var host uintptr
	// Phase 1: ensure the top-level host window is foreground. CRITICAL: a freshly
	// spawned Wails window is already foreground on its own, so the ALT-tap steal
	// in activateWindow is not only unnecessary — tapping ALT on the *already
	// active* WebView2 window enters Chromium's alt/menu mode, which swallows all
	// keystrokes until a mouse click. So only call activateWindow when the window
	// is NOT already foreground; otherwise just take it as-is.
	for waited := time.Duration(0); waited <= deadline; waited += interval {
		hwnd, _, _ := winFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
		if hwnd == 0 {
			time.Sleep(interval)
			continue
		}
		fg, _, _ := winGetForeground.Call()
		if fg == hwnd {
			host = hwnd
			break
		}
		activateWindow(hwnd) // ALT-based steal — only when something else holds focus
		time.Sleep(interval)
	}
	if host == 0 {
		return
	}
	// Phase 2: hand keyboard focus to the WebView2 content the way Wails itself
	// does. Wails binds the HOST window's WM_SETFOCUS to chromium.Focus(), which
	// moves focus into the WebView2 via its controller — the only thing that
	// actually routes keystrokes to the DOM. Raw SetFocus on the render-widget
	// child bypasses that handler. So we SetFocus the HOST — attaching to its UI
	// thread for the cross-thread call — and force a kill/set focus transition
	// each retry so WM_SETFOCUS re-fires once the webview controller has finished
	// initialising.
	var pid uint32
	hostThread, _, _ := winGetWndThreadPID.Call(host, uintptr(unsafe.Pointer(&pid)))
	myThread, _, _ := winGetCurThreadID.Call()
	attach := hostThread != 0 && hostThread != myThread
	for i := 0; i < 12; i++ {
		if attach {
			winAttachThreadIn.Call(myThread, hostThread, 1)
		}
		winSetFocus.Call(0)    // kill current focus so SetFocus(host) always emits WM_SETFOCUS
		winSetFocus.Call(host) // -> WM_SETFOCUS -> Wails onFocus -> chromium.Focus()
		if attach {
			winAttachThreadIn.Call(myThread, hostThread, 0)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// activateWindow force-activates the given top-level window so its webview input
// receives keystrokes. To beat Windows' foreground lock for a freshly-spawned
// process, it briefly attaches its input queue to the current foreground thread
// before SetForegroundWindow/SetFocus.
func activateWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	// Temporarily zero the foreground-lock timeout so SetForegroundWindow is
	// honoured for this freshly-spawned process, then restore it.
	var oldTimeout uint32
	winSysParamsInfo.Call(spiGetForegroundLockTimeout, 0, uintptr(unsafe.Pointer(&oldTimeout)), 0)
	winSysParamsInfo.Call(spiSetForegroundLockTimeout, 0, 0, spifSendChange)

	fg, _, _ := winGetForeground.Call()
	var pid uint32
	fgThread, _, _ := winGetWndThreadPID.Call(fg, uintptr(unsafe.Pointer(&pid)))
	cur, _, _ := winGetCurThreadID.Call()
	attach := fgThread != 0 && fgThread != cur
	if attach {
		winAttachThreadIn.Call(cur, fgThread, 1)
	}
	// Synthetic ALT tap: tricks Windows into treating this as user-driven input,
	// which lets SetForegroundWindow actually activate the window (and hand the
	// keyboard to the webview), instead of just raising it as always-on-top.
	winKeybdEvent.Call(vkMenu, 0, 0, 0)
	winKeybdEvent.Call(vkMenu, 0, keyeventfKeyUp, 0)
	winShowWindow.Call(hwnd, 5) // SW_SHOW
	winBringToTop.Call(hwnd)
	winSetForeground.Call(hwnd)
	winSetFocus.Call(hwnd)
	if attach {
		winAttachThreadIn.Call(cur, fgThread, 0)
	}

	winSysParamsInfo.Call(spiSetForegroundLockTimeout, 0, uintptr(oldTimeout), spifSendChange)
}

// bringWindowToFront restores the (possibly hidden/minimised) main window and
// forces it to the foreground. Used by the global hotkey and second-instance.
//
// After being hidden, WebView2 suspends rendering and frequently paints black
// when shown again. SetForegroundWindow alone doesn't fix it; nudging the window
// size by 1px sends a WM_SIZE that forces WebView2 to relayout and repaint.
func bringWindowToFront(ctx context.Context) {
	if ctx == nil {
		return
	}
	wruntime.WindowShow(ctx)
	wruntime.WindowUnminimise(ctx)
	// Let Wails process WindowShow before asking Win32 to foreground the HWND.
	// Mirrors the tray's "Show Window" (no AlwaysOnTop toggle, no size nudge —
	// both disturb WebView2).
	go func() {
		time.Sleep(80 * time.Millisecond)
		titlePtr, err := syscall.UTF16PtrFromString("LaraEnv")
		if err != nil {
			return
		}
		hwnd, _, _ := winFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
		if hwnd == 0 {
			return
		}
		winShowWindow.Call(hwnd, 9) // SW_RESTORE
		winSetForeground.Call(hwnd)
	}()
}

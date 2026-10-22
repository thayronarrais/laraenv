// Package hotkey registers a single system-wide global hotkey on Windows via
// the Win32 RegisterHotKey API, so an action can be triggered even when the
// LaraEnv window is minimised or hidden to the tray (the process must still be
// running). The hotkey is rebindable at runtime.
//
// RegisterHotKey with a NULL hwnd posts WM_HOTKEY to the *calling thread's*
// message queue, so registration, the GetMessage loop, and re-registration all
// happen on one OS-locked thread. Other goroutines talk to it via
// PostThreadMessage.
package hotkey

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"lara4ever/internal/i18n"
)

var (
	modUser32          = syscall.NewLazyDLL("user32.dll")
	modKernel32        = syscall.NewLazyDLL("kernel32.dll")
	procRegisterHotKey = modUser32.NewProc("RegisterHotKey")
	procUnregisterHK   = modUser32.NewProc("UnregisterHotKey")
	procGetMessageW    = modUser32.NewProc("GetMessageW")
	procPeekMessageW   = modUser32.NewProc("PeekMessageW")
	procPostThreadMsgW = modUser32.NewProc("PostThreadMessageW")
	procGetCurrentTID  = modKernel32.NewProc("GetCurrentThreadId")
)

const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000

	wmQuit   = 0x0012
	wmHotkey = 0x0312
	wmApp    = 0x8000

	hotkeyID = 1
)

type point struct{ x, y int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type spec struct {
	mods uint32
	vk   uint32
	ok   bool
}

// Manager owns the OS-locked hotkey thread and the currently registered hotkey.
type Manager struct {
	onTrigger func()

	mu      sync.Mutex
	tid     uint32
	desired spec
	current spec
}

// New starts the hotkey thread immediately; it idles (no hotkey registered)
// until Set is called. onTrigger fires on the hotkey thread when the hotkey is
// pressed — keep it light or hand off to another goroutine.
func New(onTrigger func()) *Manager {
	m := &Manager{onTrigger: onTrigger}
	go m.loop()
	return m
}

// Set parses and registers a hotkey string like "Ctrl+Shift+K" or
// "Win+Shift+K". An empty string disables the global hotkey. A parse error is
// returned for malformed strings; a registration failure (e.g. an OS-reserved
// combo) is not an error here — the hotkey simply stays unregistered.
func (m *Manager) Set(hk string) error {
	s, err := parse(hk)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.desired = s
	tid := m.tid
	m.mu.Unlock()
	if tid != 0 {
		procPostThreadMsgW.Call(uintptr(tid), wmApp, 0, 0)
	}
	return nil
}

// Stop unregisters the hotkey and tears down the thread.
func (m *Manager) Stop() {
	m.mu.Lock()
	tid := m.tid
	m.mu.Unlock()
	if tid != 0 {
		procPostThreadMsgW.Call(uintptr(tid), wmQuit, 0, 0)
	}
}

func (m *Manager) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Force the thread's message queue into existence before publishing the
	// thread id, so a Set() racing right after New() can PostThreadMessage us.
	var probe msg
	procPeekMessageW.Call(uintptr(unsafe.Pointer(&probe)), 0, 0, 0, 0)

	tid, _, _ := procGetCurrentTID.Call()
	m.mu.Lock()
	m.tid = uint32(tid)
	m.mu.Unlock()

	for {
		m.reconcile()

		var mm msg
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&mm)), 0, 0, 0)
		if int32(r) == -1 {
			return // GetMessage error
		}
		if r == 0 {
			m.unregister() // WM_QUIT
			return
		}
		if mm.message == wmHotkey && mm.wParam == hotkeyID && m.onTrigger != nil {
			// Run inline on this thread: a WM_HOTKEY grants the receiving thread
			// the right to set/grant foreground, which onTrigger needs to hand to
			// the spawned command-bar window (AllowSetForegroundWindow). The
			// callback only spawns a process — no Wails/webview calls — so it
			// returns immediately and the loop keeps pumping.
			m.onTrigger()
		}
		// wmApp just wakes the loop so reconcile() runs at the top.
	}
}

// reconcile registers/unregisters so the live hotkey matches the desired one.
func (m *Manager) reconcile() {
	m.mu.Lock()
	want := m.desired
	have := m.current
	m.mu.Unlock()
	if want == have {
		return
	}
	m.unregister()
	if want.ok {
		r, _, _ := procRegisterHotKey.Call(0, hotkeyID, uintptr(want.mods|modNoRepeat), uintptr(want.vk))
		if r == 0 {
			return // reserved/in-use combo — leave unregistered
		}
		m.mu.Lock()
		m.current = want
		m.mu.Unlock()
	}
}

func (m *Manager) unregister() {
	m.mu.Lock()
	had := m.current.ok
	m.current = spec{}
	m.mu.Unlock()
	if had {
		procUnregisterHK.Call(0, hotkeyID)
	}
}

// parse converts "Ctrl+Shift+K" into Win32 modifier flags + a virtual key code.
func parse(hk string) (spec, error) {
	hk = strings.TrimSpace(hk)
	if hk == "" {
		return spec{}, nil
	}
	var s spec
	for _, raw := range strings.Split(hk, "+") {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		switch strings.ToLower(p) {
		case "ctrl", "control":
			s.mods |= modControl
		case "alt":
			s.mods |= modAlt
		case "shift":
			s.mods |= modShift
		case "win", "meta", "cmd", "command", "super":
			s.mods |= modWin
		default:
			vk, ok := vkFor(p)
			if !ok {
				return spec{}, i18n.Errorf("errors.hotkey.unknownKey", map[string]any{"key": p})
			}
			s.vk = vk
		}
	}
	if s.vk == 0 {
		return spec{}, i18n.Errorf("errors.hotkey.noKey", map[string]any{"hotkey": hk})
	}
	s.ok = true
	return s, nil
}

func vkFor(k string) (uint32, bool) {
	if len(k) == 1 {
		c := k[0]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			return uint32(c), true
		}
	}
	switch strings.ToLower(k) {
	case "space":
		return 0x20, true
	case "enter", "return":
		return 0x0D, true
	case "tab":
		return 0x09, true
	case "esc", "escape":
		return 0x1B, true
	case "up", "arrowup":
		return 0x26, true
	case "down", "arrowdown":
		return 0x28, true
	case "left", "arrowleft":
		return 0x25, true
	case "right", "arrowright":
		return 0x27, true
	case "home":
		return 0x24, true
	case "end":
		return 0x23, true
	case "pageup":
		return 0x21, true
	case "pagedown":
		return 0x22, true
	case "insert":
		return 0x2D, true
	case "delete", "del":
		return 0x2E, true
	case "backspace":
		return 0x08, true
	}
	if len(k) >= 2 && (k[0] == 'f' || k[0] == 'F') {
		var n int
		if _, err := fmt.Sscanf(k[1:], "%d", &n); err == nil && n >= 1 && n <= 24 {
			return uint32(0x70 + n - 1), true
		}
	}
	return 0, false
}

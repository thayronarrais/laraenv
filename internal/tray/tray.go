// Package tray provides a Windows system-tray icon with a menu mirroring the
// most important actions of the main window. It lets the user keep services
// running after closing the main window: closing hides the window instead of
// quitting; the user explicitly chooses Quit from the tray menu to fully exit.
package tray

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/energye/systray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Service is the minimal info the tray needs about a managed service.
type Service struct {
	Name  string
	Label string
}

// Project is the minimal info the tray needs to list a project and act on it.
type Project struct {
	Name string
	URL  string
	Tags []string
}

// Callbacks wires tray menu actions back into the app.
type Callbacks struct {
	// Services returns the current list of user-facing services (excluding
	// internal ones like php-fpm:*). Called every refresh.
	Services func() []Service
	// Status returns "running" or "stopped" for a given service name.
	Status       func(name string) string
	StartService func(name string) error
	StopService  func(name string) error
	StartAll     func()
	StopAll      func()
	// Projects returns the current list of projects. Called periodically to
	// keep the Projects submenu in sync with what's on disk.
	Projects func() []Project
	// OpenFolder/OpenEditor/OpenTerminal act on a project by name; OpenBrowser
	// opens a project URL. All optional.
	OpenFolder   func(name string) error
	OpenEditor   func(name string) error
	OpenTerminal func(name string) error
	OpenBrowser  func(url string)
	// Quit performs full app shutdown (services, terminals, then exit).
	Quit func()
	// Labels resolves a translation key to the active language's text. Set at
	// construction because onReady builds the menu synchronously, before any
	// language change could arrive.
	Labels func(key string) string
}

// current is the running tray, if any. It used to be a plain *tray, written
// once in Run before the systray goroutine started and read only afterwards
// — ordering alone was the (informal) synchronisation. That assumption broke
// once app.go's startup began calling go a.watchLanguage() — which can reach
// package Relabel(), and so read current — before it calls tray.Run at all,
// let alone before Run finishes assigning it: a plain read/write race the
// Go race detector correctly flags. atomic.Pointer makes the access itself
// safe regardless of call order on either side; current.Load() before Run
// has stored anything simply returns nil, which Relabel already handles.
var current atomic.Pointer[tray]

// Run starts the tray loop in a goroutine. Blocks until systray.Quit() is called.
// Pass an .ico (Windows) or PNG byte slice as icon.
func Run(ctx context.Context, icon []byte, cb Callbacks) {
	t := &tray{ctx: ctx, cb: cb, icon: icon, labels: cb.Labels}
	current.Store(t)
	go func() {
		systray.Run(t.onReady, t.onExit)
	}()
}

// Stop tears down the tray icon. Safe to call multiple times.
func Stop() {
	defer func() { _ = recover() }()
	systray.Quit()
}

// Relabel re-applies the running tray's titles from the catalogue.
func Relabel() {
	if t := current.Load(); t != nil {
		t.Relabel()
	}
}

type serviceRow struct {
	name      string
	label     *systray.MenuItem
	startItem *systray.MenuItem
	stopItem  *systray.MenuItem
}

type tray struct {
	ctx  context.Context
	cb   Callbacks
	icon []byte

	mu        sync.Mutex
	rows      map[string]*serviceRow
	servicesM *systray.MenuItem

	projectsM *systray.MenuItem
	tagMenus  map[string]*systray.MenuItem // key: tag name ("" sentinel for untagged)
	projItems map[string]*systray.MenuItem // key: tag + "\x00" + project name

	showM, startAllM, stopAllM, quitM *systray.MenuItem
	// labels supplies tray titles from the translation catalogue. Set by the
	// app; nil until then, so the tray can run before catalogues load.
	labels func(key string) string
}

// untaggedKey is the sentinel tag used to group projects that have no tags.
const untaggedKey = ""

// async runs a menu action off the tray's message-pump thread. systray
// dispatches Click callbacks synchronously inside the Win32 WndProc, so any
// blocking action (service start/stop, taskkill, editor spawn) freezes the
// pump — the menu stops opening and further clicks are lost until it returns.
func (t *tray) async(action string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				trayDebugLog("action %s: panic: %v", action, r)
			}
		}()
		fn()
	}()
}

func (t *tray) onReady() {
	if len(t.icon) > 0 {
		systray.SetIcon(t.icon)
	}
	systray.SetTitle("LaraEnv")
	systray.SetTooltip("LaraEnv — local dev manager")

	mShow := systray.AddMenuItem("Show Window", "Open the main window")
	t.mu.Lock()
	t.showM = mShow
	t.mu.Unlock()
	mShow.Click(func() { t.async("show-window", t.showWindow) })

	systray.AddSeparator()

	servicesM := systray.AddMenuItem("Services", "Manage running services")
	t.mu.Lock()
	t.servicesM = servicesM
	t.rows = map[string]*serviceRow{}
	t.mu.Unlock()
	t.refreshServices()

	projectsM := systray.AddMenuItem("Projects", "Open projects")
	t.mu.Lock()
	t.projectsM = projectsM
	t.tagMenus = map[string]*systray.MenuItem{}
	t.projItems = map[string]*systray.MenuItem{}
	t.mu.Unlock()
	t.refreshProjects()

	systray.AddSeparator()

	mStartAll := systray.AddMenuItem("Start All", "Start every active service")
	t.mu.Lock()
	t.startAllM = mStartAll
	t.mu.Unlock()
	mStartAll.Click(func() {
		t.async("start-all", func() {
			if t.cb.StartAll != nil {
				t.cb.StartAll()
			}
			t.refreshServices()
		})
	})

	mStopAll := systray.AddMenuItem("Stop All", "Stop every running service")
	t.mu.Lock()
	t.stopAllM = mStopAll
	t.mu.Unlock()
	mStopAll.Click(func() {
		t.async("stop-all", func() {
			if t.cb.StopAll != nil {
				t.cb.StopAll()
			}
			t.refreshServices()
		})
	})

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit LaraEnv", "Exit and stop all services")
	t.mu.Lock()
	t.quitM = mQuit
	t.mu.Unlock()
	mQuit.Click(func() {
		t.async("quit", func() {
			if t.cb.Quit != nil {
				t.cb.Quit()
			}
		})
	})

	// Single left-click and double-click both bring the window back.
	systray.SetOnClick(func(menu systray.IMenu) { t.async("icon-click", t.showWindow) })
	systray.SetOnDClick(func(menu systray.IMenu) { t.async("icon-dclick", t.showWindow) })

	// Periodically refresh service rows + their status labels so the menu
	// reflects what's actually running without forcing the user to reopen it.
	go t.statusLoop()

	// Apply whatever language is already stored, right now. The items above
	// are built from hardcoded English, and config.Store.Watch only reports
	// *changes* — a returning user whose config.json already says "pt-BR"
	// would otherwise see an English tray on every launch until they touched
	// the language setting again. t.labels is set at construction (Run), so
	// it is safe to use here.
	t.Relabel()
}

func (t *tray) onExit() {}

// Relabel re-applies every static title from the catalogue. Called when the
// language changes, so the tray follows the window without a restart.
func (t *tray) Relabel() {
	if t.labels == nil {
		return
	}
	set := func(item *systray.MenuItem, key string) {
		if item != nil {
			item.SetTitle(t.labels(key))
		}
	}

	// The six top-level menu items are written once, in onReady, on the
	// systray goroutine — but Relabel can run concurrently on whatever
	// goroutine called tray.Relabel() (app.go's watchLanguage), so the reads
	// need the same t.mu that guards onReady's writes to these fields.
	// Snapshot under the lock, then call SetTitle outside it: SetTitle isn't
	// tray state t.mu protects, and onReady never holds the lock while
	// waiting on anything, so there's no reason to serialise on it too.
	t.mu.Lock()
	showM, servicesM, projectsM := t.showM, t.servicesM, t.projectsM
	startAllM, stopAllM, quitM := t.startAllM, t.stopAllM, t.quitM
	untagged := t.tagMenus[untaggedKey]
	t.mu.Unlock()

	systray.SetTooltip(t.labels("backend.tray.tooltip"))
	set(showM, "backend.tray.showWindow")
	set(servicesM, "backend.tray.services")
	set(projectsM, "backend.tray.projects")
	set(startAllM, "backend.tray.startAll")
	set(stopAllM, "backend.tray.stopAll")
	set(quitM, "backend.tray.quit")

	// The untagged group heading is created once, on first sight of a tagless
	// project, and refreshProjects never re-titles an existing tag submenu.
	// Retitle it here too, so a language switch reaches it instead of leaving
	// it stuck in whatever language was active when it was first created.
	set(untagged, "backend.tray.untagged")
}

var (
	modUser32        = syscall.NewLazyDLL("user32.dll")
	procFindWindowW  = modUser32.NewProc("FindWindowW")
	procShowWin      = modUser32.NewProc("ShowWindow")
	procSetFGWin     = modUser32.NewProc("SetForegroundWindow")
	procIsWinVisible = modUser32.NewProc("IsWindowVisible")
)

// trayDebugLog appends a timestamped line to %TEMP%\laraenv-tray-debug.log.
// Diagnostic instrumentation for the "window won't come back from the tray"
// reports: each show attempt records whether the HWND was found and whether it
// actually became visible, so a failed restore leaves evidence behind.
func trayDebugLog(format string, args ...any) {
	p := filepath.Join(os.TempDir(), "laraenv-tray-debug.log")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s [pid %d] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), os.Getpid(), fmt.Sprintf(format, args...))
}

// showWindow restores the hidden main window and forces it to the foreground.
// Using AlwaysOnTop toggle corrupts Wails window state on repeated hide/show
// cycles, so we call SetForegroundWindow via Win32 directly instead.
//
// The Wails runtime call alone has proven unreliable after long hide periods,
// so this also verifies with IsWindowVisible and falls back to direct Win32
// ShowWindow calls, retrying for ~2s before giving up (and logging the failure).
func (t *tray) showWindow() {
	trayDebugLog("showWindow: requested")
	wruntime.WindowShow(t.ctx)
	wruntime.WindowUnminimise(t.ctx)
	// Give the Wails runtime a moment to process WindowShow before asking
	// Win32 to bring the HWND to the foreground.
	go func() {
		titlePtr, err := syscall.UTF16PtrFromString("LaraEnv")
		if err != nil {
			return
		}
		const attempts = 10
		for i := 0; i < attempts; i++ {
			time.Sleep(80 * time.Millisecond)
			hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
			if hwnd == 0 {
				trayDebugLog("showWindow: attempt %d — HWND not found", i+1)
				continue
			}
			procShowWin.Call(hwnd, 9) // SW_RESTORE
			procShowWin.Call(hwnd, 5) // SW_SHOW — covers hidden-but-not-minimised
			procSetFGWin.Call(hwnd)
			if vis, _, _ := procIsWinVisible.Call(hwnd); vis != 0 {
				if i > 0 {
					trayDebugLog("showWindow: visible after %d attempt(s)", i+1)
				}
				return
			}
			// Not visible yet — ask the Wails runtime again too; one of the
			// two paths (runtime dispatch vs direct Win32) usually lands.
			trayDebugLog("showWindow: attempt %d — hwnd=%#x still not visible", i+1, hwnd)
			wruntime.WindowShow(t.ctx)
		}
		trayDebugLog("showWindow: FAILED — window never became visible")
	}()
}

func (t *tray) statusLoop() {
	tk := time.NewTicker(2 * time.Second)
	defer tk.Stop()
	// Projects rarely change and ListProjects scans disk, so reconcile the
	// Projects submenu on a much slower cadence than the service status.
	pk := time.NewTicker(30 * time.Second)
	defer pk.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-tk.C:
			t.refreshServices()
		case <-pk.C:
			t.refreshProjects()
		}
	}
}

func (t *tray) refreshServices() {
	t.mu.Lock()
	servicesM := t.servicesM
	t.mu.Unlock()
	if t.cb.Services == nil || servicesM == nil {
		return
	}
	list := t.cb.Services()

	t.mu.Lock()
	defer t.mu.Unlock()

	seen := map[string]bool{}
	for _, s := range list {
		seen[s.Name] = true
		row, ok := t.rows[s.Name]
		if !ok {
			row = t.addRow(s)
			t.rows[s.Name] = row
		}
		t.updateRow(row, s)
	}
	// Hide rows for services that disappeared (e.g. stack changed).
	for name, row := range t.rows {
		if !seen[name] {
			row.label.Hide()
		} else {
			row.label.Show()
		}
	}
}

func (t *tray) addRow(s Service) *serviceRow {
	parent := t.servicesM.AddSubMenuItem(s.Label, "")
	startItem := parent.AddSubMenuItem("Start", "")
	stopItem := parent.AddSubMenuItem("Stop", "")

	name := s.Name
	startItem.Click(func() {
		t.async("start:"+name, func() {
			if t.cb.StartService != nil {
				_ = t.cb.StartService(name)
			}
			t.refreshServices()
		})
	})
	stopItem.Click(func() {
		t.async("stop:"+name, func() {
			if t.cb.StopService != nil {
				_ = t.cb.StopService(name)
			}
			t.refreshServices()
		})
	})
	return &serviceRow{name: s.Name, label: parent, startItem: startItem, stopItem: stopItem}
}

func (t *tray) updateRow(row *serviceRow, s Service) {
	status := "stopped"
	if t.cb.Status != nil {
		status = t.cb.Status(s.Name)
	}
	dot := "●"
	statusLabel := t.labels("backend.tray.statusRunning")
	if status != "running" {
		dot = "○"
		statusLabel = t.labels("backend.tray.statusStopped")
	}
	row.label.SetTitle(fmt.Sprintf("%s  %s  (%s)", dot, s.Label, statusLabel))
	if status == "running" {
		row.startItem.Disable()
		row.stopItem.Enable()
	} else {
		row.startItem.Enable()
		row.stopItem.Disable()
	}
}

// projKey builds the composite key for a (tag, project) pair.
func projKey(tag, name string) string { return tag + "\x00" + name }

// refreshProjects reconciles the Projects submenu with the current project
// list. Like refreshServices, it relies on hide/show since systray can't
// remove items: a project appears once per tag it has (or under "Untagged").
func (t *tray) refreshProjects() {
	t.mu.Lock()
	projectsM := t.projectsM
	t.mu.Unlock()
	if t.cb.Projects == nil || projectsM == nil {
		return
	}
	list := t.cb.Projects()

	t.mu.Lock()
	defer t.mu.Unlock()

	// Desired set of (tag, project) pairs and which tags are wanted.
	want := map[string]bool{}
	tagWanted := map[string]bool{}
	for _, p := range list {
		tags := p.Tags
		if len(tags) == 0 {
			tags = []string{untaggedKey}
		}
		for _, tag := range tags {
			tagWanted[tag] = true
			key := projKey(tag, p.Name)
			want[key] = true
			tagMenu, ok := t.tagMenus[tag]
			if !ok {
				label := tag
				if tag == untaggedKey {
					label = "Untagged"
					if t.labels != nil {
						label = t.labels("backend.tray.untagged")
					}
				}
				tagMenu = t.projectsM.AddSubMenuItem(label, "")
				t.tagMenus[tag] = tagMenu
			}
			item, ok := t.projItems[key]
			if !ok {
				item = t.addProjectRow(tagMenu, p)
				t.projItems[key] = item
			}
			item.Show()
		}
	}

	// Hide project entries that are no longer wanted.
	for key, item := range t.projItems {
		if !want[key] {
			item.Hide()
		}
	}
	// Hide tag submenus that no longer contain any project.
	for tag, tagMenu := range t.tagMenus {
		if tagWanted[tag] {
			tagMenu.Show()
		} else {
			tagMenu.Hide()
		}
	}
}

// addProjectRow creates a project submenu under tagMenu with its action items.
func (t *tray) addProjectRow(tagMenu *systray.MenuItem, p Project) *systray.MenuItem {
	proj := tagMenu.AddSubMenuItem(p.Name, p.URL)
	name := p.Name
	url := p.URL

	folder := proj.AddSubMenuItem("Open Folder", "")
	folder.Click(func() {
		t.async("open-folder:"+name, func() {
			if t.cb.OpenFolder != nil {
				_ = t.cb.OpenFolder(name)
			}
		})
	})
	editor := proj.AddSubMenuItem("Open Editor", "")
	editor.Click(func() {
		t.async("open-editor:"+name, func() {
			if t.cb.OpenEditor != nil {
				_ = t.cb.OpenEditor(name)
			}
		})
	})
	term := proj.AddSubMenuItem("Open Terminal", "")
	term.Click(func() {
		t.async("open-terminal:"+name, func() {
			if t.cb.OpenTerminal != nil {
				_ = t.cb.OpenTerminal(name)
			}
		})
	})
	browser := proj.AddSubMenuItem("Open in Browser", "")
	browser.Click(func() {
		t.async("open-browser:"+name, func() {
			if t.cb.OpenBrowser != nil {
				t.cb.OpenBrowser(url)
			}
		})
	})
	return proj
}

package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/signal"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/paths"
	"lara4ever/internal/version"
	"lara4ever/internal/webviewruntime"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var trayIcon []byte

//go:embed all:langs
var langs embed.FS

func main() {
	// MCP stdout is exclusively JSON-RPC. Do not create managers, load
	// catalogues (which may report overrides) or initialize the GUI here.
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		connection, err := mcpCLIConnection(os.Args)
		if err == nil {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()
			err = mcpbridge.RunProxy(ctx, connection, version.Version)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "laraenv mcp:", err)
			os.Exit(1)
		}
		return
	}
	// Load translation catalogues into i18n.Global() before anything else.
	// This must run before ALL early-return CLI modes below (agent-hook,
	// agent-event, --cron-run) as well as before NewApp: every one of those
	// paths can hit an i18n.Errorf/i18n.Wrapf/i18n.T call synchronously, and
	// with Global() nil those calls render bare key paths (e.g. a Windows
	// Task Scheduler log reading "cron-run: errors.cron.jobNotFoundInConfig"
	// instead of a real message). Loading first means Global() is never nil
	// during any of these windows. See LoadCatalogues's doc comment in app.go.
	LoadCatalogues(langs)

	helperContext := &agentHelperContext{registry: pkgagents.NewActivityOwnerRegistry(paths.AgentActivityRoot(), nil), getenv: os.Getenv, stdout: os.Stdout}
	if handled, err := writeAgentActivity(os.Args, os.Stdin, os.Stdout, os.Getenv, helperContext.registry); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, "agent-activity:", err)
			os.Exit(1)
		}
		return
	}
	if handled, err := writeAgentHook(os.Args, os.Stdin, os.Getenv, paths.AgentEventsRoot(), helperContext); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, "agent-hook:", err)
			os.Exit(1)
		}
		return
	}

	if handled, err := writeAgentEvent(os.Args, os.Stdin, paths.AgentEventsRoot(), helperContext); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, "agent-event:", err)
			os.Exit(1)
		}
		return
	}

	// CLI subcommand: --cron-run <jobID>
	// Used by Windows Task Scheduler entries we register for jobs with
	// BackgroundMode=true. Runs the job once (if its schedule matches the
	// current minute) and exits without ever bringing up the Wails UI.
	if len(os.Args) >= 3 && os.Args[1] == "--cron-run" {
		if err := runProjectCron(paths.ConfigFile(), os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "cron-run:", err)
			os.Exit(1)
		}
		return
	}

	// The stdio endpoint must start from an ordinary client process without
	// a UAC prompt. The GUI still needs elevation for hosts/services setup.
	if relaunched, err := ensureGUIElevation(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "laraenv:", err)
		os.Exit(1)
	} else if relaunched {
		return
	}
	// A stale WebView2 registration can hide an intact machine runtime.
	// Recovery selects a verified Microsoft installation for this process only;
	// it never repairs global registration or persists a particular version.
	browserFolder, browserErr := webviewruntime.RecoveryBrowserFolder()
	if browserErr != nil {
		fmt.Fprintln(os.Stderr, "laraenv: WebView2 discovery:", browserErr)
	}
	if browserFolder != "" {
		fmt.Fprintln(os.Stderr, "laraenv: using verified WebView2 recovery folder:", browserFolder)
	}
	app := NewApp()

	// Satellite "terminal window" mode: a second process launched via
	// `--terminal-window <seedFile>` runs a minimal window with just the
	// terminal canvas, seeded from the file. It gets no tray and a lighter
	// startup so it can't disturb the main process's services.
	onStartup := app.startup
	title := "LaraEnv"
	width, height := 1280, 760
	minWidth, minHeight := 960, 600
	frameless := true
	if len(os.Args) >= 3 && os.Args[1] == "--terminal-window" {
		app.detached = true
		app.detachedSeedFile = os.Args[2]
		onStartup = app.startupDetached
		title = "LaraEnv · Terminal"
		width, height = 980, 620
		minWidth, minHeight = 560, 360
		// Detached terminals use the native Windows frame. Besides restoring
		// normal double-click maximise/restore behaviour, this is what lets
		// Windows recognise the maximise caption button and show Snap Layouts.
		frameless = false
	} else if len(os.Args) >= 2 && os.Args[1] == "--command-bar" {
		// Standalone Spotlight-style command bar: small frameless window.
		app.commandBar = true
		onStartup = app.startupCommandBar
		title = "LaraEnv · Command"
		width, height = 680, 460
		minWidth, minHeight = 680, 120
	} else {
		app.SetTrayIcon(trayIcon)
	}

	// Single-instance lock for the MAIN window only: relaunching the exe should
	// surface the existing window instead of spawning a duplicate process that
	// can't see the first instance's running dev servers (and reports their
	// ports as busy). Detached terminal windows and the command bar are
	// legitimate second processes, so they must NOT set the lock.
	var singleInstance *options.SingleInstanceLock
	if !app.detached && !app.commandBar {
		singleInstance = &options.SingleInstanceLock{
			UniqueId:               "laraenv-main-single-instance",
			OnSecondInstanceLaunch: app.onSecondInstance,
		}
	}

	err := wails.Run(&options.App{
		Title:     title,
		Width:     width,
		Height:    height,
		MinWidth:  minWidth,
		MinHeight: minHeight,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour:   &options.RGBA{R: 11, G: 16, B: 38, A: 1},
		OnStartup:          onStartup,
		OnBeforeClose:      app.beforeClose,
		OnShutdown:         app.shutdown,
		SingleInstanceLock: singleInstance,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewBrowserPath:                browserFolder,
			WebviewIsTransparent:              false,
			WindowIsTranslucent:               true,
			DisableWindowIcon:                 false,
			DisableFramelessWindowDecorations: false,
		},
		Frameless: frameless,
	})
	if err != nil {
		panic(err)
	}
}

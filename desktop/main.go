// Command Rexion-desktop is the Wails shell around the Rexion kernel: a native
// window hosting a webview frontend, with the Go-side control.Controller bound
// directly to the UI (no HTTP hop — bindings in, runtime events out). It lives in
// a nested module (Rexion/desktop) so the CGO/WebKit desktop build never touches
// the CLI's CGO_ENABLED=0 single-static-binary guarantee, while still importing
// the same internal/* kernel.
package main

import (
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"rexion/internal/config"

	// Blank imports wire compile-time built-ins into their registries, exactly as
	// cmd/Rexion does — boot.Build resolves providers/tools from these registries.
	_ "rexion/internal/provider/anthropic"
	_ "rexion/internal/provider/openai"
	_ "rexion/internal/tool/builtin"
)

// assets embeds the built frontend. `all:` so dotfiles (e.g. the dist .gitkeep
// that keeps this directive compilable before the first `pnpm build`) are
// included. A real run requires `pnpm build` (or `wails build`) to populate dist.
//
//go:embed all:frontend/dist
var assets embed.FS

// version is injected at build time via `wails build -ldflags "-X main.version=..."`,
// mirroring cmd/Rexion/main.go. The auto-updater reads it (App.Version) to compare
// against the published manifest; an un-injected dev build stays "dev" and never
// prompts to update.
var version = "dev"

func main() {
	// Brand migration first: the Reasonix → Rexion rename must run before
	// initLogging/EnsureMemoryDir create %AppData%\Rexion or ~/.rexion. Those
	// directories otherwise exist by the time boot.Build runs the migration, and
	// the old %AppData%\reasonix config would be stranded (the app would boot
	// with defaults and show onboarding again).
	config.MigrateReasonixIfNeeded()

	// Set up dual-output logging: write to both stderr (for console/wails dev)
	// and a log file (for production diagnostics). The log file lives at
	//   Windows: %AppData%\Rexion\logs\Rexion.log
	//   macOS:   ~/Library/Application Support/Rexion/logs/Rexion.log
	//   Linux:   ~/.config/Rexion/logs/Rexion.log
	initLogging()

	// Cap V8's old-space heap at 512 MB so long sessions don't balloon
	// the WebView2 renderer process to multi-GB. Without this limit V8
	// will keep growing until the OS runs out of memory.
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--js-flags=--max-old-space-size=512")

	app := NewApp()

	// Restore saved window size, or fall back to the default.
	width, height := 1240, 720
	if saved, ok := loadWindowState(); ok {
		if saved.Width > 0 {
			width = saved.Width
		}
		if saved.Height > 0 {
			height = saved.Height
		}
	}

	err := wails.Run(&options.App{
		Title:     "Rexion",
		Width:     width,
		Height:    height,
		MinWidth:  760,
		MinHeight: 480,
		// Match the dark UI shell so the initial webview background doesn't flash
		// white before CSS loads — particularly visible on WebKitGTK.
		BackgroundColour:   &options.RGBA{R: 26, G: 26, B: 46, A: 255},
		AssetServer:        &assetserver.Options{Assets: assets, Middleware: app.workspaceMediaMiddleware()},
		OnStartup:          app.startup,
		OnDomReady:         app.domReady,
		OnBeforeClose:      app.beforeClose,
		OnShutdown:         app.shutdown,
		Bind:               []any{app},
		SingleInstanceLock: singleInstanceLock(app),

		// Start hidden — domReady positions and shows the window after restoring
		// geometry, so the user never sees the default size/position flash.
		StartHidden: true,

		// Native application menu (File > Settings, Edit, Window).
		Menu: app.createAppMenu(),

		// Native OS file drops: the webview withholds dropped files' paths from the
		// HTML drop event, so the frontend (composer) reads them via runtime.OnFileDrop
		// against the --wails-drop-target element instead.
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},

		// --- per-platform adaptation (see desktop/README.md for the rationale) ---
		Mac: &mac.Options{
			// Inset traffic-lights over a frameless-feeling header; the frontend
			// leaves a drag region at the top (CSS --wails-draggable).
			TitleBar: mac.TitleBarHiddenInset(),
			// Follow the OS appearance so the title bar matches light/dark system
			// preference instead of being locked to dark.
			Appearance: mac.DefaultAppearance,
		},
		Windows: &windows.Options{
			// Follow the OS theme so the title bar matches light/dark system
			// preference instead of being locked to dark.
			Theme: windows.SystemDefault,
		},
		Linux: &linux.Options{
			ProgramName: "Rexion",
			// WebKitGTK GPU compositing is inconsistent across distros/drivers and
			// is the one real cross-platform rough edge for a Go+webview stack:
			// "always" can yield blank or flickering webviews on some setups, so
			// we let the webview decide on demand. Users still hitting artifacts
			// can fall back to WEBKIT_DISABLE_COMPOSITING_MODE=1 (see README).
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

// initLogging sets up slog to write to both stderr and a log file.
// The log file is rotated per session (appended across sessions for
// continuity). On error, it falls back to stderr-only silently.
func initLogging() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(configDir, "Rexion", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return
	}
	logPath := filepath.Join(logDir, "Rexion.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	// Dual writer: stderr + file.
	multi := &dualWriter{w1: os.Stderr, w2: f}
	slog.SetDefault(slog.New(slog.NewTextHandler(multi, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	slog.Info("Rexion: logging initialized", "log_file", logPath)
}

// dualWriter writes to two writers simultaneously.
type dualWriter struct {
	w1, w2 *os.File
}

func (d *dualWriter) Write(p []byte) (n int, err error) {
	n1, e1 := d.w1.Write(p)
	n2, e2 := d.w2.Write(p)
	if e1 != nil {
		return n1, e1
	}
	return n2, e2
}

// recoverPanic is the desktop-side panic backstop. Wails routes bound Go methods
// straight onto the UI thread; an unrecovered panic there (or in any goroutine
// they spawn without their own recover) takes down the whole process — and
// because production Wails builds have no stderr attached, the stack trace is
// lost. Deferring this at the entry of every bound method turns those crashes
// into a logged error the user can recover from by retrying.
//
// The recovered value and a goroutine stack snapshot are written through slog,
// which initLogging has already pointed at Rexion.log (and stderr in dev).
// Recovering here does NOT keep a panicking turn alive — controller.runGuarded
// has its own recover for in-turn panics — this is the outer guard for panics
// in tab lookup, transcript snapshotting, approval plumbing, etc.
func recoverPanic(label string) {
	r := recover()
	if r == nil {
		return
	}
	buf := make([]byte, 8192)
	n := goruntime.Stack(buf, false)
	slog.Error("desktop: panic in bound method",
		"label", label,
		"panic", fmt.Sprintf("%v", r),
		"stack", string(buf[:n]))
}

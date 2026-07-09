//go:build windows

package main

import (
	"runtime"

	"fyne.io/systray"
)

// startDesktopTray launches the system tray on Windows.
//
// The systray message pump (nativeLoop → GetMessageW/DispatchMessageW) MUST
// stay pinned to one OS thread: Windows associates each hidden window's
// messages with the thread that created it. If the goroutine migrates to a
// different thread, GetMessageW will block on that thread's (empty) queue
// while the hidden window's messages pile up unprocessed on the original
// thread — causing the tray icon to become permanently unresponsive until
// the process restarts.
//
// The library's init() calls runtime.LockOSThread, but that only locks the
// goroutine that imports the package (the main goroutine), NOT this new
// goroutine. We must lock it here.
func startDesktopTray(onReady, onExit func()) func() {
	go func() {
		runtime.LockOSThread()
		systray.Run(onReady, onExit)
	}()
	return systray.Quit
}

//go:build !windows

package main

import (
	"fmt"
	"sync"
)

var clipboardHotkeyMu sync.Mutex
var clipboardHotkeyRegistered bool

// RegisterClipboardHotkey registers Ctrl+Shift+R as a global hotkey.
// On non-Windows platforms, this is a stub that emits the event for the frontend
// to handle. Platform-specific native hotkey registration can be added later.
func (a *App) RegisterClipboardHotkey() error {
	if a.ctx == nil {
		return fmt.Errorf("app context not initialised")
	}
	clipboardHotkeyMu.Lock()
	defer clipboardHotkeyMu.Unlock()
	if clipboardHotkeyRegistered {
		return nil
	}
	// On macOS/Linux, the hotkey is handled at the application level.
	// Mark as registered so the frontend can listen for the event.
	clipboardHotkeyRegistered = true
	return nil
}

// UnregisterClipboardHotkey removes the global hotkey registration (stub).
func (a *App) UnregisterClipboardHotkey() {
	clipboardHotkeyMu.Lock()
	defer clipboardHotkeyMu.Unlock()
	clipboardHotkeyRegistered = false
}

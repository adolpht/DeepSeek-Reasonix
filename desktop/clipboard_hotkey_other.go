//go:build !windows

package main

import (
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var clipboardHotkeyMu sync.Mutex
var clipboardHotkeyRegistered bool

// RegisterClipboardHotkey registers Ctrl+Shift+R as a global hotkey.
//
// PLATFORM STUB (non-Windows): native global hotkey registration is not
// implemented on this OS, so the hotkey is genuinely unavailable here. We
// deliberately do NOT flip clipboardHotkeyRegistered to true — leaving it false
// keeps the runtime state honest (the hotkey is not actually registered) so the
// frontend can distinguish "platform unsupported" from "registered but not yet
// triggered". Instead we emit a "hotkey-unsupported" event (payload: "clipboard")
// that the UI can listen for to show a one-time notice / fallback affordance.
// Platform-specific native hotkey registration can be added later by replacing
// this stub with a real implementation.
func (a *App) RegisterClipboardHotkey() error {
	if a.ctx == nil {
		return fmt.Errorf("app context not initialised")
	}
	clipboardHotkeyMu.Lock()
	defer clipboardHotkeyMu.Unlock()
	if clipboardHotkeyRegistered {
		return nil
	}
	// Platform stub: global hotkeys are unavailable on this OS. Keep
	// clipboardHotkeyRegistered false and notify the frontend so it can prompt.
	runtime.EventsEmit(a.ctx, "hotkey-unsupported", "clipboard")
	return nil
}

// UnregisterClipboardHotkey removes the global hotkey registration (stub).
func (a *App) UnregisterClipboardHotkey() {
	clipboardHotkeyMu.Lock()
	defer clipboardHotkeyMu.Unlock()
	clipboardHotkeyRegistered = false
}

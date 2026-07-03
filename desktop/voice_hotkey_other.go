//go:build !windows

package main

import (
	"fmt"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	voiceHotkeyMu         sync.Mutex
	voiceHotkeyRegistered bool
)

// RegisterVoiceHotkey registers Ctrl+Shift+V as a global hotkey for voice input.
//
// PLATFORM STUB (non-Windows): native global hotkey registration is not
// implemented on this OS, so the hotkey is genuinely unavailable here. We
// deliberately do NOT flip voiceHotkeyRegistered to true — leaving it false
// keeps the runtime state honest (the hotkey is not actually registered) so the
// frontend can distinguish "platform unsupported" from "registered but not yet
// triggered". Instead we emit a "hotkey-unsupported" event (payload: "voice")
// that the UI can listen for to show a one-time notice / fallback affordance.
// Platform-specific native hotkey registration can be added later by replacing
// this stub with a real implementation.
func (a *App) RegisterVoiceHotkey() error {
	if a.ctx == nil {
		return fmt.Errorf("app context not initialised")
	}
	voiceHotkeyMu.Lock()
	defer voiceHotkeyMu.Unlock()
	if voiceHotkeyRegistered {
		return nil
	}
	// Platform stub: global hotkeys are unavailable on this OS. Keep
	// voiceHotkeyRegistered false and notify the frontend so it can prompt.
	runtime.EventsEmit(a.ctx, "hotkey-unsupported", "voice")
	return nil
}

// UnregisterVoiceHotkey removes the global voice hotkey registration (stub).
func (a *App) UnregisterVoiceHotkey() {
	voiceHotkeyMu.Lock()
	defer voiceHotkeyMu.Unlock()
	voiceHotkeyRegistered = false
}

//go:build windows

package main

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	voiceHotkeyID = 0x0002
)

var (
	voiceHotkeyMu         sync.Mutex
	voiceHotkeyRegistered bool
	voiceHotkeyStop       chan struct{}
)

// RegisterVoiceHotkey registers Ctrl+Shift+V as a global hotkey for voice input.
// When pressed, it emits a "voice-hotkey" event to the frontend so the Composer
// can start recording audio for whisper transcription.
func (a *App) RegisterVoiceHotkey() error {
	if a.ctx == nil {
		return fmt.Errorf("app context not initialised")
	}
	voiceHotkeyMu.Lock()
	defer voiceHotkeyMu.Unlock()
	if voiceHotkeyRegistered {
		return nil
	}

	// Register Ctrl+Shift+V (virtual key code 0x56 = 'V')
	ret, _, err := procRegisterHotKey.Call(
		0, // NULL = use the calling thread's message queue
		voiceHotkeyID,
		uintptr(MOD_CONTROL|MOD_SHIFT),
		0x56, // VK_V
	)
	if ret == 0 {
		return fmt.Errorf("RegisterHotKey failed: %w", err)
	}
	voiceHotkeyRegistered = true

	voiceHotkeyStop = make(chan struct{})
	go a.listenVoiceHotkey()

	return nil
}

// UnregisterVoiceHotkey removes the global voice hotkey registration.
func (a *App) UnregisterVoiceHotkey() {
	voiceHotkeyMu.Lock()
	defer voiceHotkeyMu.Unlock()
	if !voiceHotkeyRegistered {
		return
	}
	procUnregisterHotKey.Call(0, voiceHotkeyID)
	voiceHotkeyRegistered = false
	if voiceHotkeyStop != nil {
		close(voiceHotkeyStop)
		voiceHotkeyStop = nil
	}
}

// listenVoiceHotkey runs on a background goroutine, pumping the Windows message
// queue and emitting the "voice-hotkey" event when WM_HOTKEY is received.
func (a *App) listenVoiceHotkey() {
	var msg struct {
		HWnd    uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      struct{ X, Y int32 }
	}
	for {
		ret, _, _ := procGetMessage.Call(
			uintptr(unsafe.Pointer(&msg)),
			0,
			WM_HOTKEY,
			WM_HOTKEY,
		)
		if ret == 0 {
			return
		}
		select {
		case <-voiceHotkeyStop:
			return
		default:
		}
		if msg.Message == WM_HOTKEY {
			runtime.EventsEmit(a.ctx, "voice-hotkey")
		}
	}
}

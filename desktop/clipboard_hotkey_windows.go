//go:build windows

package main

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
	procGetMessage       = user32.NewProc("GetMessageW")
)

const (
	WM_HOTKEY   = 0x0312
	MOD_ALT     = 0x0001
	MOD_CONTROL = 0x0002
	MOD_SHIFT   = 0x0004
	HOTKEY_ID   = 0x0001
)

// clipboardHotkeyMu guards the hotkey registration.
var clipboardHotkeyMu sync.Mutex

// clipboardHotkeyRegistered tracks whether the global hotkey has been registered.
var clipboardHotkeyRegistered bool

// clipboardHotkeyStop is used to signal the hotkey listener to stop.
var clipboardHotkeyStop chan struct{}

// RegisterClipboardHotkey registers Ctrl+Shift+R as a global hotkey.
// When pressed, it triggers the floating clipboard assistant window by emitting
// a "clipboard-hotkey" event to the frontend.
func (a *App) RegisterClipboardHotkey() error {
	if a.ctx == nil {
		return fmt.Errorf("app context not initialised")
	}
	clipboardHotkeyMu.Lock()
	defer clipboardHotkeyMu.Unlock()
	if clipboardHotkeyRegistered {
		return nil
	}

	// Register Ctrl+Shift+R (virtual key code 0x52 = 'R')
	ret, _, err := procRegisterHotKey.Call(
		0, // NULL = use the calling thread's message queue
		HOTKEY_ID,
		uintptr(MOD_CONTROL|MOD_SHIFT),
		0x52, // VK_R
	)
	if ret == 0 {
		return fmt.Errorf("RegisterHotKey failed: %w", err)
	}
	clipboardHotkeyRegistered = true

	// Start a background goroutine to listen for the hotkey message
	clipboardHotkeyStop = make(chan struct{})
	go a.listenHotkey()

	return nil
}

// UnregisterClipboardHotkey removes the global hotkey registration.
func (a *App) UnregisterClipboardHotkey() {
	clipboardHotkeyMu.Lock()
	defer clipboardHotkeyMu.Unlock()
	if !clipboardHotkeyRegistered {
		return
	}
	procUnregisterHotKey.Call(0, HOTKEY_ID)
	clipboardHotkeyRegistered = false
	if clipboardHotkeyStop != nil {
		close(clipboardHotkeyStop)
		clipboardHotkeyStop = nil
	}
}

// listenHotkey runs on a background goroutine, pumping the Windows message
// queue and emitting the "clipboard-hotkey" event when WM_HOTKEY is received.
func (a *App) listenHotkey() {
	var msg struct {
		HWnd    uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      struct{ X, Y int32 }
	}
	for {
		// GetMessage blocks until a WM_HOTKEY message arrives for this thread
		ret, _, _ := procGetMessage.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, // all messages for this thread
			WM_HOTKEY,
			WM_HOTKEY,
		)
		if ret == 0 {
			// WM_QUIT received
			return
		}
		select {
		case <-clipboardHotkeyStop:
			return
		default:
		}
		if msg.Message == WM_HOTKEY {
			runtime.EventsEmit(a.ctx, "clipboard-hotkey")
		}
	}
}

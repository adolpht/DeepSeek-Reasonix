//go:build windows

package main

import (
	"fmt"
	"unsafe"
)

// SendInput / mouse / keyboard constants
const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseMoveF     = 0x0001
	mouseLeftDown  = 0x0002
	mouseLeftUp    = 0x0004
	mouseRightDown = 0x0008
	mouseRightUp   = 0x0010
	mouseMidDown   = 0x0020
	mouseMidUp     = 0x0040
	mouseWheel     = 0x0800
	mouseAbsolute  = 0x8000

	keyUp      = 0x0002
	keyUnicode = 0x0004

	wheelDelta = 120
)

// mouseInput mirrors MOUSEINPUT (the largest member of the INPUT union).
// For keyboard events we pack wVk|wScan into Dx and dwFlags into Dy, which
// overlap the same union bytes.
type mouseInput struct {
	Dx        int32
	Dy        int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

type input struct {
	Type uint32
	// Go inserts 4 bytes of padding here on 64-bit to align Mi to 8 bytes,
	// matching the Windows INPUT layout. On 32-bit there is no padding.
	Mi mouseInput
}

var procSendInput = user32.NewProc("SendInput")

// --- public API (called from computer.go) ---

// mouseClick moves the cursor to (x,y) and performs a click.
func mouseClick(x, y int, button string, double bool) error {
	moveMouse(x, y)
	sleepMs(20)

	var down, up uint32
	switch button {
	case "left", "":
		down, up = mouseLeftDown, mouseLeftUp
	case "right":
		down, up = mouseRightDown, mouseRightUp
	case "middle":
		down, up = mouseMidDown, mouseMidUp
	default:
		return fmt.Errorf("unknown button %q (want left/right/middle)", button)
	}

	sendMouse(0, down)
	sleepMs(10)
	sendMouse(0, up)
	if double {
		sleepMs(40)
		sendMouse(0, down)
		sleepMs(10)
		sendMouse(0, up)
	}
	return nil
}

// mouseScroll scrolls the wheel at (x,y).
func mouseScroll(x, y int, direction string, amount int) error {
	moveMouse(x, y)
	sleepMs(10)
	var delta int32
	if direction == "up" {
		delta = int32(amount * wheelDelta)
	} else {
		delta = int32(-amount * wheelDelta)
	}
	sendMouse(uint32(delta), mouseWheel)
	return nil
}

// mouseDrag drags from (fromX,fromY) to (toX,toY).
func mouseDrag(fromX, fromY, toX, toY int) error {
	moveMouse(fromX, fromY)
	sleepMs(30)
	sendMouse(0, mouseLeftDown)
	sleepMs(30)
	moveMouse(toX, toY)
	sleepMs(30)
	sendMouse(0, mouseLeftUp)
	return nil
}

// typeText types a string via Unicode key events. If clearFirst is true,
// it sends Ctrl+A then Delete to clear the current selection first.
func typeText(text string, clearFirst bool) error {
	if clearFirst {
		if err := pressKey("a", []string{"ctrl"}); err != nil {
			return err
		}
		sleepMs(10)
		if err := pressKey("Delete", nil); err != nil {
			return err
		}
		sleepMs(10)
	}

	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	// Batch all key events into one SendInput call for speed and atomicity.
	in := make([]input, 0, len(runes)*2)
	for _, r := range runes {
		scan := uint16(r & 0xFFFF)
		in = append(in, kbInput(scan, keyUnicode))
		in = append(in, kbInput(scan, keyUnicode|keyUp))
	}
	if err := sendInputs(in); err != nil {
		return err
	}
	return nil
}

// pressKey presses a named key, optionally with modifiers held.
func pressKey(key string, modifiers []string) error {
	vk, err := keyNameToVK(key)
	if err != nil {
		return err
	}
	modVKs := make([]uint16, 0, len(modifiers))
	for _, m := range modifiers {
		mv, err := modNameToVK(m)
		if err != nil {
			return err
		}
		modVKs = append(modVKs, mv)
	}

	in := make([]input, 0, len(modVKs)*2+2)
	// modifiers down
	for _, mv := range modVKs {
		in = append(in, kbVKInput(mv, 0))
	}
	// key down + up
	in = append(in, kbVKInput(vk, 0))
	in = append(in, kbVKInput(vk, keyUp))
	// modifiers up (reverse order)
	for i := len(modVKs) - 1; i >= 0; i-- {
		in = append(in, kbVKInput(modVKs[i], keyUp))
	}
	return sendInputs(in)
}

// --- helpers ---

// moveMouse moves the cursor to absolute screen coords (x,y) using the
// virtual-screen-normalized mapping that SendInput expects.
func moveMouse(x, y int) {
	virtW := int(getSystemMetrics(smCXVirtualScreen))
	virtH := int(getSystemMetrics(smCYVirtualScreen))
	virtX := int32(getSystemMetrics(smXVirtualScreen))
	virtY := int32(getSystemMetrics(smYVirtualScreen))

	if virtW <= 0 {
		virtW = 1
	}
	if virtH <= 0 {
		virtH = 1
	}
	// Map [virtX, virtX+virtW) -> [0, 65536)
	dx := int32((int(x)-int(virtX))*65536/virtW)
	dy := int32((int(y)-int(virtY))*65536/virtH)
	sendMouseAt(dx, dy, mouseMoveF|mouseAbsolute)
}

// sendMouse sends a mouse event at the current cursor position.
func sendMouse(mouseData uint32, flags uint32) {
	in := input{Type: inputMouse}
	in.Mi.MouseData = mouseData
	in.Mi.Flags = flags
	sendInputs([]input{in})
}

// sendMouseAt sends a mouse-move/absolute event with the given dx,dy.
func sendMouseAt(dx, dy int32, flags uint32) {
	in := input{Type: inputMouse}
	in.Mi.Dx = dx
	in.Mi.Dy = dy
	in.Mi.Flags = flags
	sendInputs([]input{in})
}

// kbInput builds a Unicode keyboard input (scan code = rune).
func kbInput(scan uint16, flags uint32) input {
	in := input{Type: inputKeyboard}
	// Pack wVk(0) + wScan into Dx (first 4 bytes of union).
	in.Mi.Dx = int32(scan)
	in.Mi.Dy = int32(flags)
	return in
}

// kbVKInput builds a virtual-key keyboard input.
func kbVKInput(vk uint16, flags uint32) input {
	in := input{Type: inputKeyboard}
	in.Mi.Dx = int32(vk)
	in.Mi.Dy = int32(flags)
	return in
}

// sendInputs dispatches a batch of INPUT structs via SendInput.
func sendInputs(in []input) error {
	if len(in) == 0 {
		return nil
	}
	n, _, _ := procSendInput.Call(
		uintptr(len(in)),
		uintptr(unsafe.Pointer(&in[0])),
		unsafe.Sizeof(input{}),
	)
	if n != uintptr(len(in)) {
		return fmt.Errorf("SendInput only processed %d of %d events", n, len(in))
	}
	return nil
}

// keyNameToVK maps a key name (case-insensitive) to a Windows virtual-key code.
func keyNameToVK(name string) (uint16, error) {
	low := stringsToLowerASCII(name)
	if len(name) == 1 {
		c := name[0]
		// Single printable ASCII char -> its VK code (uppercase for letters).
		if c >= 'a' && c <= 'z' {
			return uint16(c - 'a' + 'A'), nil
		}
		if c >= 'A' && c <= 'Z' {
			return uint16(c), nil
		}
		if c >= '0' && c <= '9' {
			return uint16(c), nil
		}
		// common punctuation falls through to VkKeyScan below
	}

	vk, ok := namedKeys[low]
	if ok {
		return vk, nil
	}

	// Fallback: VkKeyScanW for single chars (e.g. punctuation like '-', '/', '.').
	if len(name) == 1 {
		ks, err := vkKeyScan(name[0])
		if err == nil && ks != 0 {
			return uint16(ks & 0xFF), nil
		}
	}
	return 0, fmt.Errorf("unknown key name %q", name)
}

// modNameToVK maps a modifier name to its VK code.
func modNameToVK(name string) (uint16, error) {
	switch stringsToLowerASCII(name) {
	case "ctrl", "control":
		return 0x11, nil // VK_CONTROL
	case "shift":
		return 0x10, nil // VK_SHIFT
	case "alt", "option":
		return 0x12, nil // VK_MENU
	case "meta", "win", "cmd", "super":
		return 0x5B, nil // VK_LWIN
	default:
		return 0, fmt.Errorf("unknown modifier %q (want ctrl/shift/alt/meta)", name)
	}
}

// namedKeys maps friendly key names to virtual-key codes.
var namedKeys = map[string]uint16{
	"enter":     0x0D,
	"return":    0x0D,
	"escape":    0x1B,
	"esc":       0x1B,
	"tab":       0x09,
	"space":     0x20,
	"backspace": 0x08,
	"back":      0x08,
	"delete":    0x2E,
	"del":       0x2E,
	"insert":    0x2D,
	"home":      0x24,
	"end":       0x23,
	"pageup":    0x21,
	"pgup":      0x21,
	"pagedown":  0x22,
	"pgdn":      0x22,
	"arrowup":   0x26,
	"up":        0x26,
	"arrowdown": 0x28,
	"down":      0x28,
	"arrowleft": 0x25,
	"left":      0x25,
	"arrowright": 0x27,
	"right":     0x27,
	"capslock":  0x14,
	"numlock":   0x90,
	"scrolllock": 0x91,
	"printscreen": 0x2C,
	"pause":     0x13,
	"f1":  0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73, "f5": 0x74,
	"f6":  0x75, "f7": 0x76, "f8": 0x77, "f9": 0x78, "f10": 0x79,
	"f11": 0x7A, "f12": 0x7B,
}

// vkKeyScan wraps VkKeyScanW to resolve a single char to its VK code.
var procVkKeyScanW = user32.NewProc("VkKeyScanW")

func vkKeyScan(c byte) (uint16, error) {
	r, _, _ := procVkKeyScanW.Call(uintptr(uint16(c)))
	return uint16(r), nil
}

// stringsToLowerASCII lowercases an ASCII string without importing strings
// (keeps this file self-contained for the windows build).
func stringsToLowerASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

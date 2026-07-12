//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// rectWin mirrors the Windows RECT struct.
type rectWin struct {
	Left, Top, Right, Bottom int32
}

const (
	swRestore = 9

	processQueryLimitedInformation = 0x1000
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

var (
	procEnumWindows           = user32.NewProc("EnumWindows")
	procGetWindowTextW        = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW  = user32.NewProc("GetWindowTextLengthW")
	procIsWindowVisible       = user32.NewProc("IsWindowVisible")
	procGetWindowRect         = user32.NewProc("GetWindowRect")
	procGetWindowThreadProcID = user32.NewProc("GetWindowThreadProcessId")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procShowWindow            = user32.NewProc("ShowWindow")
	procIsIconic              = user32.NewProc("IsIconic")

	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
)

// windowEntry is the intermediate record collected by the EnumWindows callback.
type windowEntry struct {
	Hwnd  uintptr
	Title string
	Rect  rectWin
	PID   uint32
}

// listApps returns visible top-level windows with their titles and process names.
func listApps() ([]appInfo, error) {
	entries, err := enumVisibleWindows()
	if err != nil {
		return nil, err
	}
	out := make([]appInfo, 0, len(entries))
	for _, e := range entries {
		if e.Title == "" {
			continue
		}
		out = append(out, appInfo{
			Title:   e.Title,
			Process: processName(e.PID),
		})
	}
	return out, nil
}

// listWindows returns visible top-level windows with title, rect, and process name.
func listWindows() ([]windowInfo, error) {
	entries, err := enumVisibleWindows()
	if err != nil {
		return nil, err
	}
	out := make([]windowInfo, 0, len(entries))
	for _, e := range entries {
		if e.Title == "" {
			continue
		}
		out = append(out, windowInfo{
			Title:   e.Title,
			Process: processName(e.PID),
			X:       int(e.Rect.Left),
			Y:       int(e.Rect.Top),
			W:       int(e.Rect.Right - e.Rect.Left),
			H:       int(e.Rect.Bottom - e.Rect.Top),
		})
	}
	return out, nil
}

// switchApp finds a window matching app_name (title substring) or
// process_name, then brings it to the foreground. Returns its title.
func switchApp(appName, processNameArg string) (string, error) {
	entries, err := enumVisibleWindows()
	if err != nil {
		return "", err
	}

	needleTitle := stringsToLowerASCII(appName)
	needleProc := stringsToLowerASCII(processNameArg)

	var match *windowEntry
	for i := range entries {
		e := &entries[i]
		if e.Title == "" {
			continue
		}
		pname := processName(e.PID)
		if needleTitle != "" && containsLower(stringsToLowerASCII(e.Title), needleTitle) {
			match = e
			break
		}
		if needleProc != "" && containsLower(stringsToLowerASCII(pname), needleProc) {
			match = e
			break
		}
	}
	if match == nil {
		return "", fmt.Errorf("no matching window found (app_name=%q process_name=%q)", appName, processNameArg)
	}

	// Restore if minimized, then foreground.
	if isIconic(match.Hwnd) {
		procShowWindow.Call(match.Hwnd, uintptr(swRestore))
	}
	ret, _, _ := procSetForegroundWindow.Call(match.Hwnd)
	if ret == 0 {
		// SetForegroundWindow often fails due to focus-stealing prevention;
		// the window may still have been restored. Report best-effort.
		return match.Title, nil
	}
	return match.Title, nil
}

// enumVisibleWindows enumerates all visible top-level windows with non-empty titles.
func enumVisibleWindows() ([]windowEntry, error) {
	var collected []windowEntry
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1 // continue
		}
		length, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if length == 0 {
			return 1
		}
		buf := make([]uint16, length+1)
		n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(length+1))
		if n == 0 {
			return 1
		}
		title := syscall.UTF16ToString(buf)

		var rc rectWin
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))

		var pid uint32
		procGetWindowThreadProcID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))

		collected = append(collected, windowEntry{
			Hwnd:  hwnd,
			Title: title,
			Rect:  rc,
			PID:   pid,
		})
		return 1
	})

	ret, _, err := procEnumWindows.Call(cb, 0)
	if ret == 0 {
		return nil, fmt.Errorf("EnumWindows failed: %v", err)
	}
	return collected, nil
}

// processName returns the executable name (e.g. "EXCEL.EXE") for a PID.
func processName(pid uint32) string {
	h, _, _ := procOpenProcess.Call(uintptr(processQueryLimitedInformation), 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)

	const maxPath = 1024
	buf := make([]uint16, maxPath)
	size := uint32(maxPath)
	ret, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ret == 0 {
		return ""
	}
	full := syscall.UTF16ToString(buf)
	// Strip directory, keep executable filename.
	for i := len(full) - 1; i >= 0; i-- {
		if full[i] == '\\' || full[i] == '/' {
			return full[i+1:]
		}
	}
	return full
}

// isIconic returns true if the window is minimized.
func isIconic(hwnd uintptr) bool {
	r, _, _ := procIsIconic.Call(hwnd)
	return r != 0
}

// containsLower reports whether s contains substr (both already lowercased ASCII).
func containsLower(s, substr string) bool {
	if substr == "" {
		return true
	}
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

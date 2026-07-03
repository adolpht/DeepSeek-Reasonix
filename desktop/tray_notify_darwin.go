//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

// notifyPlatform sends a macOS notification via terminal-notifier or osascript.
func notifyPlatform(title, body string) {
	// Use osascript as a fallback (works on all macOS systems).
	// Escape double quotes inside title/body so the AppleScript string stays valid.
	escape := func(s string) string {
		return strings.ReplaceAll(s, `"`, `\"`)
	}
	script := `display notification "` + escape(body) + `" with title "` + escape(title) + `"`
	_ = exec.Command("osascript", "-e", script).Run()
}

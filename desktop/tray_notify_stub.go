//go:build !windows && !darwin && !linux

package main

// notifyPlatform sends a desktop notification. This stub does nothing;
// platform-specific implementations are in tray_notify_windows.go etc.
func notifyPlatform(title, body string) {
	// no-op on unsupported platforms
}

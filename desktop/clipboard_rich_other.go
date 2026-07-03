//go:build !windows

package main

// readClipboardRichNonText is a platform stub. On non-Windows platforms the
// clipboard monitor captures text only; image (CF_DIB) and file-list (CF_HDROP)
// capture rely on win32 clipboard-format APIs that have no portable equivalent
// here, so they are implemented Windows-only (see clipboard_rich_windows.go).
//
// Returning present=false makes monitorClipboard fall through to the text path,
// preserving the original text-only behaviour. Add a platform-specific
// implementation here if/when macOS/Linux rich-clipboard capture is needed.
func readClipboardRichNonText() (kind, content string, present bool) {
	return "", "", false
}

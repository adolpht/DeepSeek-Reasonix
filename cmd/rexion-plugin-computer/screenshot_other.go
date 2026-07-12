//go:build !windows

package main

import "fmt"

// screenshotDisplay is unsupported on non-Windows platforms.
func screenshotDisplay(display int, region *rect) ([]byte, int, int, error) {
	return nil, 0, 0, fmt.Errorf("screenshot not supported on this platform")
}

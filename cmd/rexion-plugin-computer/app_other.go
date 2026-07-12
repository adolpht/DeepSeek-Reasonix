//go:build !windows

package main

import "fmt"

func listApps() ([]appInfo, error) {
	return nil, fmt.Errorf("app listing not supported on this platform")
}

func listWindows() ([]windowInfo, error) {
	return nil, fmt.Errorf("window listing not supported on this platform")
}

func switchApp(appName, processName string) (string, error) {
	return "", fmt.Errorf("app switching not supported on this platform")
}

//go:build windows

package main

import (
	"os/exec"
)

// notifyPlatform sends a Windows toast notification via PowerShell.
func notifyPlatform(title, body string) {
	psCmd := `Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show('` + body + `', '` + title + `', 'OK', 'Information')`
	_ = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd).Run()
}

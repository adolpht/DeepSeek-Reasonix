//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// trashPath moves a file or directory to the Windows Recycle Bin.
// Uses PowerShell's Microsoft.VisualBasic.FileIO.FileSystem API which
// reliably sends items to the Recycle Bin on all Windows versions.
func trashPath(path string) error {
	// Escape single quotes in the path for PowerShell
	escaped := strings.ReplaceAll(path, "'", "''")
	// Try deleting as a file first, then as a directory.
	// The VB API has separate methods for files and directories.
	psFile := fmt.Sprintf(
		`[Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile('%s', 'OnlyErrorDialogs', 'SendToRecycleBin')`,
		escaped,
	)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Add-Type -AssemblyName Microsoft.VisualBasic; "+psFile)
	if err := cmd.Run(); err == nil {
		return nil
	}
	// File delete failed — try directory version
	psDir := fmt.Sprintf(
		`[Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory('%s', 'OnlyErrorDialogs', 'SendToRecycleBin')`,
		escaped,
	)
	cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Add-Type -AssemblyName Microsoft.VisualBasic; "+psDir)
	return cmd.Run()
}

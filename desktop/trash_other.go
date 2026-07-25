//go:build !windows && !darwin

package main

import (
	"os"
	"os/exec"
)

// trashPath moves a file or directory to the trash on Linux/Unix.
// Tries `gio trash` (GNOME) first, then falls back to permanent delete.
func trashPath(path string) error {
	// Try gio trash (GNOME/FreeDesktop standard)
	if _, err := exec.LookPath("gio"); err == nil {
		if err := exec.Command("gio", "trash", path).Run(); err == nil {
			return nil
		}
	}
	// Try trash-put (CLI trash utility)
	if _, err := exec.LookPath("trash-put"); err == nil {
		if err := exec.Command("trash-put", path).Run(); err == nil {
			return nil
		}
	}
	// Fallback: permanent delete (no standard trash on all Linux distros)
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

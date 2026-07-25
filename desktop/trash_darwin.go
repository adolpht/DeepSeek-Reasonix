//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// trashPath moves a file or directory to the macOS Trash.
func trashPath(path string) error {
	// Use the macOS `mv` to ~/.Trash which is the standard Trash location.
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	trashDir := filepath.Join(home, ".Trash")
	// Ensure .Trash exists
	os.MkdirAll(trashDir, 0o700)

	dst := filepath.Join(trashDir, filepath.Base(path))
	// Handle name collision by appending a number
	counter := 0
	for {
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			break
		}
		counter++
		ext := filepath.Ext(path)
		name := filepath.Base(path[:len(path)-len(ext)])
		dst = filepath.Join(trashDir, name+" "+strconv.Itoa(counter)+ext)
	}
	return exec.Command("mv", path, dst).Run()
}

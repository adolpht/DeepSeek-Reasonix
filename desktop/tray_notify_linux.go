//go:build linux

package main

import "os/exec"

// notifyPlatform sends a Linux notification via notify-send.
func notifyPlatform(title, body string) {
	_ = exec.Command("notify-send", title, body).Run()
}

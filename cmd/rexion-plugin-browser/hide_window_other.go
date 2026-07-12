//go:build !windows

package main

import "os/exec"

// hideWindow 在非Windows平台上是空操作
func hideWindow(_ *exec.Cmd) {}

//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// hideWindow 防止Chrome子进程在Windows上弹出控制台窗口
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

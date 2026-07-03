//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// unixPty implements ptyProcess using creack/pty (POSIX).
type unixPty struct {
	cmd       *exec.Cmd
	ptmx      *os.File
	closeOnce sync.Once
	closed    chan struct{}
}

// startPty creates a new PTY session on non-Windows platforms using creack/pty.
func startPty(exe string, args []string, cwd string, cols, rows int) (ptyProcess, error) {
	cmd := exec.Command(exe, args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}

	if cols > 0 && rows > 0 {
		_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	}

	return &unixPty{cmd: cmd, ptmx: ptmx, closed: make(chan struct{})}, nil
}

func (u *unixPty) Read(p []byte) (int, error) {
	return u.ptmx.Read(p)
}

func (u *unixPty) Write(p []byte) (int, error) {
	return u.ptmx.Write(p)
}

func (u *unixPty) Resize(cols, rows uint16) error {
	return pty.Setsize(u.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

func (u *unixPty) Wait() (int, error) {
	select {
	case <-u.closed:
		return -1, fmt.Errorf("pty already closed")
	default:
	}
	waitErr := u.cmd.Wait()
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return -1, waitErr
	}
	return 0, nil
}

func (u *unixPty) Close() {
	u.closeOnce.Do(func() {
		close(u.closed)
		_ = u.ptmx.Close()
		if u.cmd.Process != nil {
			_ = u.cmd.Process.Kill()
		}
	})
}

func (u *unixPty) Pid() int {
	if u.cmd.Process != nil {
		return u.cmd.Process.Pid
	}
	return 0
}

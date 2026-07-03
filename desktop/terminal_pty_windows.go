//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	conpty "github.com/admpub/conpty"
)

// windowsPty implements ptyProcess using Windows ConPTY.
type windowsPty struct {
	cpty      *conpty.ConPty
	closeOnce sync.Once
	closed    chan struct{}
}

// startPty creates a new PTY session on Windows using ConPTY.
func startPty(exe string, args []string, cwd string, cols, rows int) (ptyProcess, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, fmt.Errorf("ConPTY is not available on this Windows version (requires Windows 10 1809+)")
	}

	cmdWithArgs := []string{exe}
	cmdWithArgs = append(cmdWithArgs, args...)

	opts := []conpty.ConPtyOption{
		conpty.ConPtyDimensions(cols, rows),
		conpty.ConPtyWorkDir(cwd),
		conpty.ConPtyEnv(os.Environ()),
	}

	cpty, err := conpty.Start(cmdWithArgs, opts...)
	if err != nil {
		return nil, fmt.Errorf("conpty start: %w", err)
	}

	return &windowsPty{cpty: cpty, closed: make(chan struct{})}, nil
}

func (w *windowsPty) Read(p []byte) (int, error) {
	return w.cpty.Read(p)
}

func (w *windowsPty) Write(p []byte) (int, error) {
	return w.cpty.Write(p)
}

func (w *windowsPty) Resize(cols, rows uint16) error {
	return w.cpty.Resize(int(cols), int(rows))
}

func (w *windowsPty) Wait() (int, error) {
	// If Close() was already called externally (e.g. by kill()), the process
	// and pipe handles are gone; Wait() would use stale handles and might
	// close handles that Windows has already reassigned to a new ConPTY
	// session.  Detect this and return immediately.
	select {
	case <-w.closed:
		return -1, fmt.Errorf("pty already closed")
	default:
	}
	exitCode, err := w.cpty.Wait(context.Background())
	if err != nil {
		return -1, err
	}
	return int(exitCode), nil
}

func (w *windowsPty) Close() {
	w.closeOnce.Do(func() {
		close(w.closed)
		_ = w.cpty.Close()
	})
}

func (w *windowsPty) Pid() int {
	return w.cpty.Pid()
}

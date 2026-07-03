package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// terminalOutputChannel is the Wails event name the frontend subscribes to for
// terminal output and lifecycle events. Kept separate from "agent:event" so the
// high-frequency byte stream never touches the agent event pipeline.
const terminalOutputChannel = "terminal:output"

// TerminalView is the JSON shape returned to the frontend when a session starts.
type TerminalView struct {
	ID    string `json:"id"`
	Shell string `json:"shell"`
	Cwd   string `json:"cwd"`
	PID   int    `json:"pid"`
}

// TerminalOutput is the event payload streamed to the frontend via EventsEmit.
// Data carries raw PTY bytes; when Exit is true the session has ended.
type TerminalOutput struct {
	ID   string `json:"id"`
	Data string `json:"data"`
	Exit bool   `json:"exit"`
	Code int    `json:"code,omitempty"`
	Err  string `json:"err,omitempty"`
}

// ptyProcess is the platform-agnostic interface for a running PTY session.
// Implementations live in terminal_pty_windows.go and terminal_pty_other.go.
type ptyProcess interface {
	// Read reads output from the PTY. Returns io.EOF when the session ends.
	Read(p []byte) (int, error)
	// Write sends input to the PTY.
	Write(p []byte) (int, error)
	// Resize changes the terminal dimensions.
	Resize(cols, rows uint16) error
	// Wait blocks until the child process exits and returns its exit code.
	Wait() (int, error)
	// Close releases all resources associated with the PTY.
	Close()
	// Pid returns the child process ID, or 0 if not yet started.
	Pid() int
}

// terminalSession owns one live PTY child process.
type terminalSession struct {
	id   string
	pty  ptyProcess
	shell string
	cwd  string
	done chan struct{}
}

// terminalManager owns all live terminal sessions: a mutex-guarded map keyed by
// session id, modeled after mediaTokenStore.
type terminalManager struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
}

func newTerminalManager() *terminalManager {
	return &terminalManager{sessions: map[string]*terminalSession{}}
}

// resolveShell maps a frontend shell hint to an executable path + args.
func resolveShell(preferred string) (string, []string, error) {
	p := strings.TrimSpace(preferred)
	switch strings.ToLower(p) {
	case "", "default":
		if goruntime.GOOS == "windows" {
			return "powershell.exe", []string{"-NoLogo"}, nil
		}
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/bash"
		}
		return sh, []string{"-l"}, nil
	case "cmd":
		if goruntime.GOOS != "windows" {
			return "", nil, fmt.Errorf("cmd is only available on Windows")
		}
		return "cmd.exe", nil, nil
	case "powershell", "powershell.exe":
		if goruntime.GOOS != "windows" {
			return "", nil, fmt.Errorf("powershell.exe is only available on Windows")
		}
		return "powershell.exe", []string{"-NoLogo"}, nil
	case "pwsh":
		exe, err := exec.LookPath("pwsh")
		if err != nil {
			return "", nil, fmt.Errorf("pwsh not found in PATH: %w", err)
		}
		return exe, []string{"-NoLogo"}, nil
	case "bash":
		exe, err := exec.LookPath("bash")
		if err != nil {
			return "", nil, fmt.Errorf("bash not found in PATH: %w", err)
		}
		return exe, nil, nil
	case "zsh":
		exe, err := exec.LookPath("zsh")
		if err != nil {
			return "", nil, fmt.Errorf("zsh not found in PATH: %w", err)
		}
		return exe, []string{"-l"}, nil
	default:
		exe, err := exec.LookPath(p)
		if err != nil {
			return p, nil, nil
		}
		return exe, nil, nil
	}
}

// emitter is the closure passed to terminalManager.start so it can fire Wails
// events without the manager importing the runtime package directly.
type emitter interface{ emit(string, TerminalOutput) }

type wailsEmitter struct{ a *App }

func (w wailsEmitter) emit(name string, payload TerminalOutput) {
	wailsruntime.EventsEmit(w.a.ctx, name, payload)
}

// start spawns the shell in a PTY and streams output to the frontend.
func (m *terminalManager) start(em emitter, id, cwd, shell string, cols, rows int) error {
	exe, args, err := resolveShell(shell)
	if err != nil {
		return err
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	pty, err := startPty(exe, args, cwd, cols, rows)
	if err != nil {
		return fmt.Errorf("start pty: %w", err)
	}

	sess := &terminalSession{
		id:    id,
		pty:   pty,
		shell: exe,
		cwd:   cwd,
		done:  make(chan struct{}),
	}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	go func() {
		buf := make([]byte, 8192)
		for {
			n, readErr := pty.Read(buf)
			if n > 0 {
				em.emit(terminalOutputChannel, TerminalOutput{ID: id, Data: string(buf[:n])})
			}
			if readErr != nil {
				break
			}
		}
		code := 0
		exitCode, waitErr := pty.Wait()
		pty.Close()
		if waitErr != nil {
			code = -1
		} else {
			code = exitCode
		}
		em.emit(terminalOutputChannel, TerminalOutput{ID: id, Exit: true, Code: code, Err: waitErrString(waitErr)})
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		close(sess.done)
	}()

	return nil
}

func waitErrString(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if strings.Contains(s, "signal:") {
		return ""
	}
	return s
}

func (m *terminalManager) write(id string, data string) error {
	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("terminal session %s not found", id)
	}
	_, err := sess.pty.Write([]byte(data))
	return err
}

func (m *terminalManager) resize(id string, cols, rows int) error {
	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("terminal session %s not found", id)
	}
	return sess.pty.Resize(uint16(cols), uint16(rows))
}

func (m *terminalManager) kill(id string) error {
	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("terminal session %s not found", id)
	}
	sess.pty.Close()
	return nil
}

// closeAll kills every live session. Called from App.shutdown so no orphan
// shell processes survive a window close.
func (m *terminalManager) closeAll() {
	m.mu.Lock()
	all := make([]*terminalSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		s.pty.Close()
	}
}

func makeTerminalID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "term_" + hex.EncodeToString(b)
}

// activeCwd resolves the working directory a new terminal should start in.
func (a *App) activeCwd() string {
	a.mu.RLock()
	tab := a.tabs[a.activeTabID]
	a.mu.RUnlock()
	if tab != nil && tab.WorkspaceRoot != "" {
		return tab.WorkspaceRoot
	}
	wd, _ := os.Getwd()
	return wd
}

// --- App bindings (exposed to frontend via Wails generate) ---

// TerminalStart spawns a new interactive shell in a PTY and returns its handle.
func (a *App) TerminalStart(cwd string, shell string, cols int, rows int) (TerminalView, error) {
	if cwd == "" {
		cwd = a.activeCwd()
	}
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	id := makeTerminalID()
	if err := a.terminals.start(wailsEmitter{a}, id, cwd, shell, cols, rows); err != nil {
		return TerminalView{}, err
	}
	a.terminals.mu.Lock()
	sess := a.terminals.sessions[id]
	a.terminals.mu.Unlock()
	pid := 0
	sh := shell
	if sess != nil {
		pid = sess.pty.Pid()
		sh = sess.shell
	}
	return TerminalView{ID: id, Shell: sh, Cwd: cwd, PID: pid}, nil
}

// TerminalWrite sends raw input bytes to the PTY stdin.
func (a *App) TerminalWrite(id string, data string) error {
	return a.terminals.write(id, data)
}

// TerminalResize updates the PTY window size.
func (a *App) TerminalResize(id string, cols int, rows int) error {
	return a.terminals.resize(id, cols, rows)
}

// TerminalKill terminates the session's child process.
func (a *App) TerminalKill(id string) error {
	return a.terminals.kill(id)
}

// TerminalShells returns the shell options available on this platform.
func (a *App) TerminalShells() []string {
	if goruntime.GOOS == "windows" {
		shells := []string{"powershell", "cmd"}
		if _, err := exec.LookPath("pwsh"); err == nil {
			shells = append([]string{"pwsh"}, shells...)
		}
		if p, err := exec.LookPath("bash"); err == nil {
			shells = append(shells, p)
		}
		return shells
	}
	shells := []string{}
	if p, err := exec.LookPath("zsh"); err == nil {
		shells = append(shells, p)
	}
	if p, err := exec.LookPath("bash"); err == nil {
		shells = append(shells, p)
	}
	if _, err := exec.LookPath("pwsh"); err == nil {
		shells = append(shells, "pwsh")
	}
	return shells
}

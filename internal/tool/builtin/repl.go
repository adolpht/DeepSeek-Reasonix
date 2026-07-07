package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"rexion/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&JSEvalTool{})
	tool.RegisterBuiltin(&PythonEvalTool{})
}

// REPLManager manages persistent REPL sessions for js_eval and python_eval.
// It is a singleton shared by both tools, created during boot and wired into
// each tool instance. All public methods are goroutine-safe.
type REPLManager struct {
	mu       sync.Mutex
	sessions map[string]*replSession // key: "js" | "python"
	jsPath   string
	pyPath   string
	timeout  time.Duration
}

// replSession represents a persistent interpreter process (node or python3).
type replSession struct {
	lang   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	output *syncBuffer
	cancel context.CancelFunc
	alive  bool
}

// syncBuffer is a goroutine-safe bytes.Buffer that also implements io.Writer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Bytes()
}

func (b *syncBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// NewREPLManager creates a new REPL manager.
func NewREPLManager(jsPath, pyPath string, timeout time.Duration) *REPLManager {
	return &REPLManager{
		sessions: make(map[string]*replSession),
		jsPath:   jsPath,
		pyPath:   pyPath,
		timeout:  timeout,
	}
}

// Eval executes code in a persistent REPL session. If reset is true, any
// existing session is killed and a fresh one is started. The manager is
// goroutine-safe: concurrent Eval calls are serialised per language.
func (m *REPLManager) Eval(ctx context.Context, lang, code string, reset bool) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if reset {
		m.killSession(lang)
	}

	sess, ok := m.sessions[lang]
	if !ok || !sess.alive {
		var err error
		switch lang {
		case "js":
			sess, err = m.startJSREPL(ctx)
		case "python":
			sess, err = m.startPythonREPL(ctx)
		default:
			return "", fmt.Errorf("unsupported REPL language: %s", lang)
		}
		if err != nil {
			return "", err
		}
		m.sessions[lang] = sess
	}

	evalCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	result, err := sess.eval(evalCtx, code)
	if err != nil {
		// Session died — mark it so a subsequent call restarts it.
		sess.alive = false
		m.killSession(lang)
		return "", err
	}
	return result, nil
}

// Close terminates all active REPL sessions. Called on agent session end.
func (m *REPLManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for lang := range m.sessions {
		m.killSession(lang)
	}
}

// killSession kills and removes the session for the given language.
// Caller must hold m.mu.
func (m *REPLManager) killSession(lang string) {
	sess, ok := m.sessions[lang]
	if !ok {
		return
	}
	if sess.cancel != nil {
		sess.cancel()
	}
	sess.alive = false
	delete(m.sessions, lang)
}

// startJSREPL attempts to start a Node.js REPL, trying the configured path
// first, then falling back to "node" and "nodejs". Returns the session or
// an error.
func (m *REPLManager) startJSREPL(ctx context.Context) (*replSession, error) {
	// Try the configured path first.
	if _, err := exec.LookPath(m.jsPath); err == nil {
		return m.startREPL(ctx, "js", m.jsPath, []string{"-i"})
	}
	// Fallback: try "node" then "nodejs" if the configured path isn't found.
	for _, candidate := range []string{"node", "nodejs"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return m.startREPL(ctx, "js", candidate, []string{"-i"})
		}
	}
	return nil, fmt.Errorf("js_eval: node not found on PATH (install Node.js or set [tools.repl] js_path to the node binary)")
}

// startPythonREPL attempts to start a Python REPL, trying the configured path
// first, then falling back to "python3" and "python". Returns the session or
// an error.
func (m *REPLManager) startPythonREPL(ctx context.Context) (*replSession, error) {
	// Try the configured path first.
	if _, err := exec.LookPath(m.pyPath); err == nil {
		return m.startREPL(ctx, "python", m.pyPath, []string{"-i"})
	}
	// Fallback: try "python3" then "python" if the configured path isn't found.
	for _, candidate := range []string{"python3", "python"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return m.startREPL(ctx, "python", candidate, []string{"-i"})
		}
	}
	return nil, fmt.Errorf("python_eval: python3 not found on PATH (install Python 3 or set [tools.repl] python_path to the python3 binary)")
}

// startREPL launches a new interactive interpreter process and returns a
// replSession wrapping it. Caller must hold m.mu.
func (m *REPLManager) startREPL(ctx context.Context, lang, bin string, args []string) (*replSession, error) {
	label := lang + "_eval"
	if _, err := exec.LookPath(bin); err != nil {
		hint := fmt.Sprintf("set [tools.repl] %s_path to the %s binary", lang, lang)
		if lang == "js" {
			hint = "install Node.js or set [tools.repl] js_path to the node binary"
		} else if lang == "python" {
			hint = "install Python 3 or set [tools.repl] python_path to the python3 binary"
		}
		return nil, fmt.Errorf("%s: %s not found on PATH (%s): %w", label, bin, hint, err)
	}

	sessionCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(sessionCtx, bin, args...)
	cmd.Env = bashCommandEnv(ctx)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s: failed to create stdin pipe: %w", label, err)
	}

	output := &syncBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("%s: failed to start %s: %w", label, bin, err)
	}

	sess := &replSession{
		lang:   lang,
		cmd:    cmd,
		stdin:  stdinPipe,
		output: output,
		cancel: cancel,
		alive:  true,
	}

	// Give the interpreter a moment to print its startup banner, then discard it.
	time.Sleep(500 * time.Millisecond)
	output.Reset()

	return sess, nil
}

// eval sends code to the interpreter's stdin and waits for output using a
// delimiter-based detection strategy.
func (s *replSession) eval(ctx context.Context, code string) (string, error) {
	delimiter := fmt.Sprintf("__REPL_DELIM_%d__", time.Now().UnixNano())

	var delimiterStmt string
	switch s.lang {
	case "js":
		delimiterStmt = fmt.Sprintf("console.log('%s')", delimiter)
	case "python":
		delimiterStmt = fmt.Sprintf("print('%s')", delimiter)
	}

	// Write the user's code, then a delimiter print statement.
	codeToSend := code + "\n" + delimiterStmt + "\n"
	if _, err := s.stdin.Write([]byte(codeToSend)); err != nil {
		return "", fmt.Errorf("%s_eval: write to stdin failed: %w", s.lang, err)
	}

	return readUntilDelimiter(ctx, s, delimiter)
}

// readUntilDelimiter polls the session's output buffer until the delimiter
// appears, then returns the output before it. It respects context
// cancellation for timeouts.
func readUntilDelimiter(ctx context.Context, sess *replSession, delimiter string) (string, error) {
	const (
		maxOutput    = 1 * 1024 * 1024 // 1MB max output
		pollInterval = 10 * time.Millisecond
		startupWait  = 50 * time.Millisecond
	)

	// Brief pause to let the interpreter start processing.
	time.Sleep(startupWait)

	var accumulated []byte

	for {
		select {
		case <-ctx.Done():
			out := sess.output.Bytes()
			sess.output.Reset()
			result := combineOutput(accumulated, out, delimiter)
			return cleanREPLOutput(result, sess.lang), ctx.Err()
		default:
		}

		out := sess.output.Bytes()
		sess.output.Reset()

		combined := make([]byte, 0, len(accumulated)+len(out))
		combined = append(combined, accumulated...)
		combined = append(combined, out...)

		// Check if delimiter is in the output.
		if idx := bytes.Index(combined, []byte(delimiter)); idx >= 0 {
			result := string(combined[:idx])
			return cleanREPLOutput(result, sess.lang), nil
		}

		// Truncate if output is too large.
		if len(combined) > maxOutput {
			result := string(combined[:maxOutput])
			return cleanREPLOutput(result, sess.lang) + "\n[output truncated at 1MB]", nil
		}

		accumulated = combined

		// Check deadline.
		if deadline, ok := ctx.Deadline(); ok && time.Now().After(deadline) {
			return cleanREPLOutput(string(accumulated), sess.lang), fmt.Errorf("%s_eval: evaluation timed out", sess.lang)
		}

		time.Sleep(pollInterval)
	}
}

// combineOutput merges accumulated and new output, stripping any delimiter.
func combineOutput(accumulated, new []byte, delimiter string) string {
	combined := make([]byte, 0, len(accumulated)+len(new))
	combined = append(combined, accumulated...)
	combined = append(combined, new...)
	result := string(combined)
	result = strings.ReplaceAll(result, delimiter, "")
	return result
}

// cleanREPLOutput strips REPL prompts and noise from the raw output.
func cleanREPLOutput(output, lang string) string {
	lines := strings.Split(output, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := line
		switch lang {
		case "js":
			// Strip node REPL prompts: "> " at start, "..." continuation.
			trimmed = stripPrompt(trimmed, "> ", "... ")
		case "python":
			// Strip Python REPL prompts: ">>> " at start, "... " continuation.
			trimmed = stripPrompt(trimmed, ">>> ", "... ")
		}
		// Skip empty echo lines where the REPL just echoed the input.
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

// stripPrompt removes REPL prompt prefixes from a line.
func stripPrompt(line, primary, continuation string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == strings.TrimSpace(primary) || trimmed == strings.TrimSpace(continuation) {
		return ""
	}
	if strings.HasPrefix(line, primary) {
		return strings.TrimPrefix(line, primary)
	}
	if strings.HasPrefix(line, continuation) {
		return strings.TrimPrefix(line, continuation)
	}
	return line
}

// --- JSEvalTool ---

// JSEvalTool implements tool.Tool for JavaScript evaluation in a persistent
// Node.js REPL session.
type JSEvalTool struct {
	manager *REPLManager
}

func (t *JSEvalTool) Name() string { return "js_eval" }

func (t *JSEvalTool) Description() string {
	return "Evaluate JavaScript code in a persistent REPL session. " +
		"Variables and state persist across calls within the same session. " +
		"Use reset=true to clear the session state."
}

func (t *JSEvalTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "code": {"type": "string", "description": "JavaScript code to evaluate"},
    "reset": {"type": "boolean", "description": "Reset the REPL state (clear all variables)"}
  },
  "required": ["code"]
}`)
}

func (t *JSEvalTool) ReadOnly() bool { return false }

func (t *JSEvalTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Code  string `json:"code"`
		Reset bool   `json:"reset"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Code == "" {
		return "", fmt.Errorf("code is required")
	}
	if t.manager == nil {
		return "", fmt.Errorf("js_eval: REPL manager not configured")
	}
	result, err := t.manager.Eval(ctx, "js", p.Code, p.Reset)
	if err != nil {
		return "", err
	}
	return result, nil
}

// --- PythonEvalTool ---

// PythonEvalTool implements tool.Tool for Python evaluation in a persistent
// Python REPL session.
type PythonEvalTool struct {
	manager *REPLManager
}

func (t *PythonEvalTool) Name() string { return "python_eval" }

func (t *PythonEvalTool) Description() string {
	return "Evaluate Python code in a persistent REPL session. " +
		"Variables and state persist across calls within the same session. " +
		"Use reset=true to clear the session state."
}

func (t *PythonEvalTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "code": {"type": "string", "description": "Python code to evaluate"},
    "reset": {"type": "boolean", "description": "Reset the REPL state (clear all variables)"}
  },
  "required": ["code"]
}`)
}

func (t *PythonEvalTool) ReadOnly() bool { return false }

func (t *PythonEvalTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Code  string `json:"code"`
		Reset bool   `json:"reset"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Code == "" {
		return "", fmt.Errorf("code is required")
	}
	if t.manager == nil {
		return "", fmt.Errorf("python_eval: REPL manager not configured")
	}
	result, err := t.manager.Eval(ctx, "python", p.Code, p.Reset)
	if err != nil {
		return "", err
	}
	return result, nil
}

// ConfineREPL returns the js_eval and python_eval tools bound to a REPLManager.
func ConfineREPL(mgr *REPLManager) []tool.Tool {
	return []tool.Tool{
		&JSEvalTool{manager: mgr},
		&PythonEvalTool{manager: mgr},
	}
}

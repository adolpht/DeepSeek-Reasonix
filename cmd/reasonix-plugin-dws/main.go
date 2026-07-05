// Command reasonix-plugin-dws is an MCP stdio server that wraps the DingTalk
// Workspace CLI (dws), exposing its full product surface (contacts, calendar,
// documents, AI tables, chat, attendance, approval, mail, drive, etc.) as MCP
// tools so the Reasonix agent can operate DingTalk via natural language.
//
// On startup the plugin auto-checks whether dws is installed and authenticated
// (via the dws_check auto-start tool). If dws is missing it logs an install
// hint; if unauthenticated it automatically runs "dws auth login" which opens
// the browser for a one-time OAuth authorization — similar to WorkBuddy's
// seamless onboarding experience.
//
// Wire it up in reasonix.toml:
//
//	[[plugins]]
//	name    = "dws"
//	command = "reasonix-plugin-dws"
//	auto_start_tool = "dws_check"
//
// Environment variables:
//
//	DWS_PATH - Path to dws binary (default: "dws" from PATH)
//
// Reasonix surfaces its tools as mcp__dws__dws_call / dws_schema / dws_auth / dws_check.
//
// Protocol: newline-delimited JSON-RPC 2.0 on stdin/stdout. Logs go to stderr;
// stdout is reserved for JSON-RPC.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// version is overridable via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log.SetPrefix("reasonix-plugin-dws: ")
	log.SetFlags(0)
	if err := serve(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// --- JSON-RPC framing ---

type request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	protocolVersion    = "2024-11-05"
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

func serve(in *os.File, out *os.File) error {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	defer w.Flush()

	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if rerr := handleLine(line, w); rerr != nil {
				return rerr
			}
			if ferr := w.Flush(); ferr != nil {
				return ferr
			}
		}
		if err != nil {
			return nil
		}
	}
}

func handleLine(line []byte, w *bufio.Writer) error {
	line = trimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		log.Printf("skipping unparseable line: %v", err)
		return nil
	}
	if req.ID == nil {
		return nil
	}

	resp := response{JSONRPC: "2.0", ID: *req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "reasonix-plugin-dws", "version": version},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolList()}
	case "tools/call":
		resp.Result, resp.Error = callTool(req.Params)
	default:
		resp.Error = &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// --- tools ---

type toolDef struct {
	name        string
	description string
	schema      map[string]any
	readOnly    bool
	run         func(args map[string]any) (any, error)
}

var tools = []toolDef{
	dwsCheckTool,
	dwsCallTool,
	dwsSchemaTool,
	dwsAuthTool,
}

func toolList() []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":        t.name,
			"description": t.description,
			"inputSchema": t.schema,
			"annotations": map[string]any{
				"readOnlyHint": t.readOnly,
				"title":        t.name,
			},
		})
	}
	return out
}

func callTool(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	for _, t := range tools {
		if t.name != p.Name {
			continue
		}
		result, err := t.run(p.Arguments)
		if err != nil {
			return textResult(err.Error(), true), nil
		}
		if s, ok := result.(string); ok {
			return textResult(s, false), nil
		}
		return result, nil
	}
	return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
}

func textResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// --- dws binary resolution ---

// dwsBin returns the path to the dws CLI binary. Resolution order:
//  1. DWS_PATH environment variable (explicit override)
//  2. "dws" from PATH (standard install)
//  3. <exe_dir>/dws (bundled alongside the plugin in the install directory)
func dwsBin() string {
	if p := os.Getenv("DWS_PATH"); p != "" {
		return p
	}
	// Check if dws is bundled alongside the plugin executable.
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "dws")
		if runtime.GOOS == "windows" {
			candidate += ".exe"
		}
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	return "dws"
}

// dwsBinExists checks whether the dws binary is findable and executable.
func dwsBinExists() bool {
	bin := dwsBin()
	// Absolute path (from DWS_PATH or bundled) — just stat it.
	if filepath.IsAbs(bin) {
		fi, err := os.Stat(bin)
		return err == nil && !fi.IsDir()
	}
	// Relative / bare name — use LookPath.
	if _, err := exec.LookPath(bin); err != nil {
		return false
	}
	return true
}

// --- tool definitions ---

// dwsCheckTool is the auto-start tool. When the plugin connects, Reasonix
// calls it automatically (via auto_start_tool = "dws_check"). It:
//  1. Checks if dws is installed (binary findable on PATH or DWS_PATH).
//  2. If installed, checks authentication status.
//  3. If unauthenticated, automatically runs "dws auth login" to open the
//     browser for OAuth — mirroring the WorkBuddy one-click onboarding.
//
// The result is a human-readable status string that the AI model can relay
// to the user.
var dwsCheckTool = toolDef{
	name: "dws_check",
	description: "Auto-start health check: verify dws is installed and authenticated. " +
		"If dws is missing, returns install instructions. " +
		"If dws is installed but unauthenticated, automatically triggers browser-based OAuth login (dws auth login). " +
		"If already authenticated, confirms readiness. " +
		"This tool is called automatically when the plugin starts (auto_start_tool).",
	readOnly: true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"auto_login": map[string]any{
				"type":        "boolean",
				"description": "If true and dws is unauthenticated, automatically open browser for OAuth login (default: true). Set to false to only check status without triggering login.",
				"default":     true,
			},
		},
	},
	run: func(args map[string]any) (any, error) {
		autoLogin := true
		if v, ok := args["auto_login"]; ok {
			if b, ok := v.(bool); ok {
				autoLogin = b
			}
		}

		// Step 1: Check if dws binary exists.
		if !dwsBinExists() {
			msg := "dws CLI is not installed or not found on PATH.\n\n" +
				"Install with one of:\n" +
				"  macOS/Linux: curl -fsSL https://raw.githubusercontent.com/DingTalk-Real-AI/dingtalk-workspace-cli/main/scripts/install.sh | sh\n" +
				"  Windows:     irm https://raw.githubusercontent.com/DingTalk-Real-AI/dingtalk-workspace-cli/main/scripts/install.ps1 | iex\n" +
				"  npm:         npm install -g dingtalk-workspace-cli\n\n" +
				"Or set DWS_PATH to the full path of the dws binary.\n" +
				"After installing, restart Reasonix or reconnect the dws plugin."
			log.Print("dws binary not found")
			return msg, nil
		}

		// Step 2: Check authentication status.
		authResult, err := runDwsCommand("auth status", []string{"--format", "json"}, 10)
		if err != nil {
			// auth status command itself failed — might be a version too old
			// to support --format json. Try without --format.
			authResult, err = runDwsCommand("auth status", nil, 10)
			if err != nil {
				return fmt.Sprintf("dws is installed but auth check failed: %v\nTry running 'dws auth login' manually.", err), nil
			}
		}

		authStr, ok := authResult.(string)
		if !ok {
			authStr = fmt.Sprintf("%v", authResult)
		}

		// Check if the auth status indicates we are logged in.
		// dws auth status --format json typically returns something like:
		//   {"authenticated": true, ...} or {"authenticated": false, ...}
		// Or in plain text: "Logged in as ..." / "Not authenticated"
		isAuthenticated := strings.Contains(strings.ToLower(authStr), `"authenticated":true`) ||
			strings.Contains(strings.ToLower(authStr), `"authenticated": true`) ||
			strings.Contains(strings.ToLower(authStr), "logged in") ||
			strings.Contains(strings.ToLower(authStr), "authenticated")

		// More precise: if the string says "not authenticated" that's NOT auth'd
		if strings.Contains(strings.ToLower(authStr), "not authenticated") ||
			strings.Contains(strings.ToLower(authStr), `"authenticated":false`) ||
			strings.Contains(strings.ToLower(authStr), `"authenticated": false`) ||
			strings.Contains(strings.ToLower(authStr), "not logged in") ||
			strings.Contains(strings.ToLower(authStr), "no credentials") {
			isAuthenticated = false
		}

		if isAuthenticated {
			log.Print("dws is installed and authenticated")
			return fmt.Sprintf("dws is ready. Auth status: %s", authStr), nil
		}

		// Step 3: Not authenticated — auto-login if requested.
		if !autoLogin {
			log.Print("dws is installed but not authenticated (auto_login=false)")
			return fmt.Sprintf("dws is installed but not authenticated. Auth status: %s\nCall dws_auth(action='login') to authenticate via browser.", authStr), nil
		}

		log.Print("dws not authenticated, auto-triggering dws auth login ...")
		loginResult, loginErr := runDwsCommand("auth login", nil, 120)
		if loginErr != nil {
			return fmt.Sprintf("dws auth check: not authenticated. Auto-login failed: %v\nAuth status was: %s\nPlease try 'dws auth login' manually.", loginErr, authStr), nil
		}

		loginStr, ok := loginResult.(string)
		if !ok {
			loginStr = fmt.Sprintf("%v", loginResult)
		}

		// After login, check auth status again to confirm.
		confirmResult, confirmErr := runDwsCommand("auth status", []string{"--format", "json"}, 10)
		confirmStr := ""
		if confirmErr != nil {
			confirmStr = fmt.Sprintf("(status check failed: %v)", confirmErr)
		} else if s, ok := confirmResult.(string); ok {
			confirmStr = s
		} else {
			confirmStr = fmt.Sprintf("%v", confirmResult)
		}

		return fmt.Sprintf("dws auth login completed. Login output: %s\nVerification: %s", loginStr, confirmStr), nil
	},
}

var dwsCallTool = toolDef{
	name: "dws_call",
	description: "Execute a dws CLI command to operate DingTalk products (contacts, calendar, documents, AI tables, chat, attendance, approval, mail, drive, minutes, todo, etc.). " +
		"Use dws_schema first to discover available products and tool parameter schemas. " +
		"Always add --format json for structured output. Add --dry-run to preview without executing. " +
		"For write/delete operations, add --yes to confirm.",
	readOnly: false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "dws subcommand path, e.g. 'contact user search', 'calendar event list', 'aitable record query'",
			},
			"args": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Additional flags and arguments, e.g. ['--query', '张三', '--format', 'json', '--dry-run']",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Command timeout in seconds (default 30)",
				"default":     30,
			},
		},
		"required": []string{"command"},
	},
	run: func(args map[string]any) (any, error) {
		cmdStr, err := argString(args, "command")
		if err != nil {
			return nil, err
		}
		extraArgs := argStringSliceDefault(args, "args", nil)
		timeout := argIntDefault(args, "timeout", 30)

		return runDwsCommand(cmdStr, extraArgs, timeout)
	},
}

var dwsSchemaTool = toolDef{
	name:        "dws_schema",
	description: "Discover available dws products and tool parameter schemas. Call without arguments to list all products. Call with a tool path (e.g. 'aitable.query_records') to get its JSON Schema.",
	readOnly:    true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tool": map[string]any{
				"type":        "string",
				"description": "Optional tool path to inspect (e.g. 'aitable.query_records', 'calendar.event_list'). Omit to list all products.",
			},
			"jq": map[string]any{
				"type":        "string",
				"description": "Optional jq expression to filter the schema output",
			},
		},
	},
	run: func(args map[string]any) (any, error) {
		var cmdParts []string
		toolPath := argStringDefault(args, "tool", "")
		jqExpr := argStringDefault(args, "jq", "")

		cmdParts = append(cmdParts, "schema")
		if toolPath != "" {
			cmdParts = append(cmdParts, toolPath)
		}
		if jqExpr != "" {
			cmdParts = append(cmdParts, "--jq", jqExpr)
		}
		cmdParts = append(cmdParts, "--format", "json")

		return runDwsCommand(strings.Join(cmdParts, " "), nil, 15)
	},
}

var dwsAuthTool = toolDef{
	name:        "dws_auth",
	description: "Manage dws authentication: check login status, login, or logout. Use 'status' to check if dws is authenticated.",
	readOnly:    false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"status", "login", "logout"},
				"description": "Auth action to perform. 'status' checks current auth state (safe, read-only). 'login' opens browser for OAuth. 'logout' removes stored credentials.",
				"default":     "status",
			},
		},
	},
	run: func(args map[string]any) (any, error) {
		action := argStringDefault(args, "action", "status")
		switch action {
		case "status":
			return runDwsCommand("auth status", []string{"--format", "json"}, 10)
		case "login":
			return runDwsCommand("auth login", nil, 120)
		case "logout":
			return runDwsCommand("auth logout", []string{"--yes"}, 10)
		default:
			return nil, fmt.Errorf("unknown auth action: %q (use status/login/logout)", action)
		}
	},
}

// --- dws command execution ---

func runDwsCommand(cmdStr string, extraArgs []string, timeoutSec int) (any, error) {
	bin := dwsBin()

	// Parse the command string into parts (e.g. "contact user search" → ["contact", "user", "search"])
	parts := strings.Fields(cmdStr)
	allArgs := append(parts, extraArgs...)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, allArgs...)

	// Capture stdout (the dws output) and stderr separately
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	log.Printf("dws %s (%v)", cmdStr, elapsed.Round(time.Millisecond))

	out := stdout.String()
	errOut := stderr.String()

	if err != nil {
		// If we got stderr output, include it in the error
		errMsg := fmt.Sprintf("dws %s failed: %v", cmdStr, err)
		if errOut != "" {
			errMsg += "\n" + strings.TrimSpace(errOut)
		}
		if out != "" {
			errMsg += "\n" + strings.TrimSpace(out)
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	result := strings.TrimSpace(out)
	if result == "" && errOut != "" {
		// Some dws commands output to stderr (like auth login)
		result = strings.TrimSpace(errOut)
	}
	if result == "" {
		result = "(no output)"
	}

	return result, nil
}

// --- arg helpers ---

func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	return s, nil
}

func argStringDefault(args map[string]any, key, def string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return def
	}
	if s == "" {
		return def
	}
	return s
}

func argIntDefault(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return def
		}
		return int(n)
	case int:
		return n
	default:
		return def
	}
}

func argStringSliceDefault(args map[string]any, key string, def []string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	arr, ok := v.([]any)
	if !ok {
		return def
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return def
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return def
	}
	return out
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r' || b[len(b)-1] == '\n') {
		b = b[:len(b)-1]
	}
	return b
}

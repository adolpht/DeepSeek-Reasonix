// Command rexion-plugin-computer is an MCP stdio server exposing desktop
// automation tools to Rexion (screenshot, mouse, keyboard, app/window
// management). It is a standalone binary wired in via Rexion.toml:
//
//	[[plugins]]
//	name    = "computer"
//	command = "rexion-plugin-computer"
//
// Rexion then surfaces its tools as mcp__computer__computer_screenshot /
// computer_click / computer_type / computer_key / computer_scroll /
// computer_app_switch / computer_app_list / computer_window_list /
// computer_drag.
//
// The plugin talks directly to the OS via the standard library + syscall
// (Windows API on Windows; stubs elsewhere). No third-party dependencies.
//
// Protocol: newline-delimited JSON-RPC 2.0 on stdin/stdout. Logs go to
// stderr; stdout is reserved for JSON-RPC.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
)

// version is overridable via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log.SetPrefix("rexion-plugin-computer: ")
	log.SetFlags(0)
	if err := serve(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// --- JSON-RPC framing (mirrors rexion-plugin-browser / -calendar) ---

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
			"serverInfo":      map[string]any{"name": "rexion-plugin-computer", "version": version},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolList()}
	case "tools/call":
		resp.Result, resp.Error = callTool(req.Params)
	case "ping":
		resp.Result = map[string]any{}
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

// tools 注册所有桌面自动化工具
var tools = []toolDef{
	{
		name:        "computer_screenshot",
		description: "Take a screenshot of the desktop (or a region of it). Returns a base64-encoded PNG image. Optional: display (index, default 0 = primary), region ({x,y,w,h} in pixels).",
		readOnly:    true,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"display": map[string]any{"type": "integer", "description": "Display index (0 = primary monitor). Default 0."},
				"region": map[string]any{
					"type": "object",
					"description": "Optional sub-rectangle to crop (in screen pixels).",
					"properties": map[string]any{
						"x": map[string]any{"type": "integer"},
						"y": map[string]any{"type": "integer"},
						"w": map[string]any{"type": "integer"},
						"h": map[string]any{"type": "integer"},
					},
				},
			},
		},
		run: runScreenshot,
	},
	{
		name:        "computer_click",
		description: "Click at screen coordinates (x, y). button: left (default) | right | middle. Set double_click=true for a double click.",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"x":            map[string]any{"type": "integer", "description": "Screen X coordinate in pixels."},
				"y":            map[string]any{"type": "integer", "description": "Screen Y coordinate in pixels."},
				"button":       map[string]any{"type": "string", "description": "left | right | middle. Default left.", "default": "left"},
				"double_click": map[string]any{"type": "boolean", "description": "Perform a double click. Default false.", "default": false},
			},
			"required": []string{"x", "y"},
		},
		run: runClick,
	},
	{
		name:        "computer_type",
		description: "Type a text string at the current cursor focus. Set clear_first=true to select-all & delete before typing (useful for replacing a field's contents).",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text":        map[string]any{"type": "string", "description": "Text to type."},
				"clear_first": map[string]any{"type": "boolean", "description": "Ctrl+A then Delete before typing. Default false.", "default": false},
			},
			"required": []string{"text"},
		},
		run: runType,
	},
	{
		name:        "computer_key",
		description: "Press a key or key combination. key is a key name (e.g. Enter, Escape, Tab, Space, Backspace, F1, arrowup, a). modifiers is a list of ctrl/shift/alt/meta (win).",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":        map[string]any{"type": "string", "description": "Key name, e.g. Enter, Escape, Tab, Space, Backspace, Delete, Home, End, PageUp, PageDown, arrowup, arrowdown, arrowleft, arrowright, F1..F12, or a single character like a."},
				"modifiers":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Modifiers to hold: ctrl, shift, alt, meta (Win key)."},
			},
			"required": []string{"key"},
		},
		run: runKey,
	},
	{
		name:        "computer_scroll",
		description: "Scroll the wheel at (x, y). direction: up | down. amount is the number of wheel notches (default 3).",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"x":         map[string]any{"type": "integer", "description": "Screen X coordinate in pixels."},
				"y":         map[string]any{"type": "integer", "description": "Screen Y coordinate in pixels."},
				"direction": map[string]any{"type": "string", "description": "up | down. Default down.", "default": "down"},
				"amount":    map[string]any{"type": "integer", "description": "Number of wheel notches. Default 3.", "default": 3},
			},
			"required": []string{"x", "y"},
		},
		run: runScroll,
	},
	{
		name:        "computer_app_switch",
		description: "Bring an application to the foreground. Match by window title substring (app_name) or process name (process_name). At least one must be provided; app_name is tried first.",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"app_name":     map[string]any{"type": "string", "description": "Window title substring to match (case-insensitive), e.g. \"Excel\"."},
				"process_name": map[string]any{"type": "string", "description": "Process executable name to match, e.g. \"EXCEL.EXE\"."},
			},
		},
		run: runAppSwitch,
	},
	{
		name:        "computer_app_list",
		description: "List currently running applications (visible top-level windows). Returns each window's title, process name, and hwnd.",
		readOnly:    true,
		schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		run: runAppList,
	},
	{
		name:        "computer_window_list",
		description: "List all visible top-level windows with title, position (x, y, w, h), and process name. Useful to find click targets.",
		readOnly:    true,
		schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		run: runWindowList,
	},
	{
		name:        "computer_drag",
		description: "Drag the mouse from (from_x, from_y) to (to_x, to_y). Used for drag-and-drop, slider movement, or selection.",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"from_x": map[string]any{"type": "integer", "description": "Start screen X in pixels."},
				"from_y": map[string]any{"type": "integer", "description": "Start screen Y in pixels."},
				"to_x":   map[string]any{"type": "integer", "description": "End screen X in pixels."},
				"to_y":   map[string]any{"type": "integer", "description": "End screen Y in pixels."},
			},
			"required": []string{"from_x", "from_y", "to_x", "to_y"},
		},
		run: runDrag,
	},
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

// callTool 分发tools/call请求到具体工具处理函数
// 工具执行错误作为isError content返回（模型可读），而非JSON-RPC error
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
		// 如果结果已经是结构化格式（如截图），直接返回
		if m, ok := result.(map[string]any); ok {
			return m, nil
		}
		// 字符串结果包装为text content
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

// argIntDefault 获取整数参数，带默认值。支持 float64（JSON 数字默认解析为 float64）和 int。
func argIntDefault(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return def
	}
}

func argBoolDefault(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return stringsToLower(b) == "true"
	default:
		return def
	}
}

// argStringSlice 获取字符串数组参数；接受 []string 或 []any（每个元素会被转为 string）。
func argStringSlice(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func stringsToLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
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

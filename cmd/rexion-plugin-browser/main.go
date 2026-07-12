// Command rexion-plugin-browser is an MCP stdio server exposing browser
// automation tools to Rexion via Chrome DevTools Protocol (CDP). It is a
// standalone binary wired in via Rexion.toml:
//
//	[[plugins]]
//	name    = "browser"
//	command = "rexion-plugin-browser"
//
// Rexion then surfaces its tools as mcp__browser__browser_navigate /
// browser_click / browser_type / browser_screenshot / browser_evaluate /
// browser_get_text.
//
// The plugin launches Chrome in headless mode and communicates via CDP
// WebSocket. No external CDP library (e.g. chromedp) is used; CDP messages
// are sent directly over WebSocket.
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
	log.SetPrefix("rexion-plugin-browser: ")
	log.SetFlags(0)
	if err := serve(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// --- JSON-RPC framing (mirrors rexion-plugin-calendar / -search) ---

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
			"serverInfo":      map[string]any{"name": "rexion-plugin-browser", "version": version},
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

// tools 注册所有浏览器自动化工具
var tools = []toolDef{
	{
		name:        "browser_navigate",
		description: "Navigate the browser to a URL. Returns the page title and final URL after loading.",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "The URL to navigate to"},
			},
			"required": []string{"url"},
		},
		run: runNavigate,
	},
	{
		name:        "browser_click",
		description: "Click an element on the page matching the given CSS selector.",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"selector": map[string]any{"type": "string", "description": "CSS selector of the element to click"},
			},
			"required": []string{"selector"},
		},
		run: runClick,
	},
	{
		name:        "browser_type",
		description: "Type text into an input element matching the given CSS selector. Optionally submit the form.",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"selector": map[string]any{"type": "string", "description": "CSS selector of the input element"},
				"text":     map[string]any{"type": "string", "description": "Text to type into the element"},
				"submit":   map[string]any{"type": "boolean", "description": "Whether to submit the form after typing (default: false)"},
			},
			"required": []string{"selector", "text"},
		},
		run: runType,
	},
	{
		name:        "browser_screenshot",
		description: "Take a screenshot of the page or a specific element. Returns a base64-encoded PNG image.",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"selector":   map[string]any{"type": "string", "description": "CSS selector of element to screenshot (optional, screenshots full page if omitted)"},
				"full_page":  map[string]any{"type": "boolean", "description": "Capture the full scrollable page instead of the viewport (default: false)"},
			},
		},
		run: runScreenshot,
	},
	{
		name:        "browser_evaluate",
		description: "Execute a JavaScript expression in the browser and return the result.",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"script": map[string]any{"type": "string", "description": "JavaScript code to evaluate"},
			},
			"required": []string{"script"},
		},
		run: runEvaluate,
	},
	{
		name:        "browser_get_text",
		description: "Get the text content of the page or a specific element. Returns the innerText.",
		readOnly:    true,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"selector": map[string]any{"type": "string", "description": "CSS selector of element to get text from (optional, gets full page text if omitted)"},
			},
		},
		run: runGetText,
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

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r' || b[len(b)-1] == '\n') {
		b = b[:len(b)-1]
	}
	return b
}

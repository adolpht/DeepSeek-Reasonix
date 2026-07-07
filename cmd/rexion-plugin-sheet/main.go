// Command Rexion-plugin-sheet is an MCP stdio server exposing spreadsheet
// (xlsx/csv) read/write/query/chart tools to Rexion. It is a standalone binary
// wired in via Rexion.toml:
//
//	[[plugins]]
//	name    = "sheet"
//	command = "Rexion-plugin-sheet"
//
// Rexion then surfaces its tools as mcp__sheet__read_sheet / write_sheet /
// query_sheet / chart_sheet. Read-only tools declare readOnlyHint so the agent
// can batch them in parallel and the permission layer auto-allows them.
//
// Protocol: newline-delimited JSON-RPC 2.0 on stdin/stdout (see
// cmd/Rexion-plugin-example for the contract). Logs go to stderr; stdout is
// reserved for JSON-RPC.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
)

// version is overridable via -ldflags "-X main.version=...". Reported in
// initialize's serverInfo so Rexion (and humans) can see which build is running.
var version = "dev"

func main() {
	log.SetPrefix("Rexion-plugin-sheet: ")
	log.SetFlags(0)
	if err := serve(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// --- JSON-RPC framing (mirrors Rexion-plugin-example) ---

type request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"` // nil ⇒ notification (no reply)
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

// serve runs the read-dispatch-reply loop until stdin closes.
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
			return nil // EOF or pipe closed: clean shutdown
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
		return nil // notification: no reply
	}

	resp := response{JSONRPC: "2.0", ID: *req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{"name": "Rexion-plugin-sheet", "version": version},
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

// toolDef is one exposed tool: metadata plus a handler returning MCP content.
// A handler error becomes an isError content result (in-band, model-visible);
// JSON-RPC errors are reserved for protocol-level faults.
type toolDef struct {
	name        string
	description string
	schema      map[string]any
	readOnly    bool
	run         func(args map[string]any) (any, error)
}

// tools is the registry. Order is stable for tools/list output.
var tools = []toolDef{
	readSheetTool,
	writeSheetTool,
	querySheetTool,
	chartSheetTool,
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

// callTool dispatches a tools/call. Handler errors are reported as isError
// content results (in-band, so the model sees and can adapt to them).
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
		// result may already be a full content envelope (e.g. image), or a plain string.
		switch v := result.(type) {
		case string:
			return textResult(v, false), nil
		default:
			return v, nil
		}
	}
	return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
}

// textResult builds a text content envelope. isError=true signals the model the
// tool call failed but keeps the error message in-band for recovery.
func textResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// imageResult builds an image content envelope (PNG base64) plus a short text
// caption. Models read the text; the image is rendered by the client UI.
func imageResult(pngBase64, caption string) map[string]any {
	content := []map[string]any{{
		"type":     "image",
		"data":     pngBase64,
		"mimeType": "image/png",
	}}
	if caption != "" {
		content = append(content, map[string]any{"type": "text", "text": caption})
	}
	return map[string]any{"content": content}
}

// --- shared arg helpers ---

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
	case float64: // JSON numbers unmarshal to float64
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

// trimSpace trims leading/trailing ASCII whitespace.
func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r' || b[len(b)-1] == '\n') {
		b = b[:len(b)-1]
	}
	return b
}

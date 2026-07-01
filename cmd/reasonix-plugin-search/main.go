// Command reasonix-plugin-search is an MCP stdio server exposing web search,
// web page extraction, and comparison table tools to Reasonix. It is a standalone
// binary wired in via reasonix.toml:
//
//	[[plugins]]
//	name    = "search"
//	command = "reasonix-plugin-search"
//
// Reasonix then surfaces its tools as mcp__search__web_search / web_extract /
// compare_table. web_search requires SEARCH_API_KEY (and optionally
// SEARCH_API_PROVIDER=serpapi|bing); the other two tools work without any API key.
//
// Protocol: newline-delimited JSON-RPC 2.0 on stdin/stdout. Logs go to stderr;
// stdout is reserved for JSON-RPC.
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
	log.SetPrefix("reasonix-plugin-search: ")
	log.SetFlags(0)
	if err := serve(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// --- JSON-RPC framing (mirrors reasonix-plugin-office / -sheet) ---

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
			"serverInfo":      map[string]any{"name": "reasonix-plugin-search", "version": version},
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
	webSearchTool,
	webExtractTool,
	compareTableTool,
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

func argStringSlice(args map[string]any, key string) ([]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, fmt.Errorf("missing required argument %q", key)
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("argument %q must be an array, got %T", key, v)
	}
	out := make([]string, 0, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("argument %q[%d] must be a string, got %T", key, i, item)
		}
		out = append(out, s)
	}
	return out, nil
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

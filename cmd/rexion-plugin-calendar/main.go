// Command Rexion-plugin-calendar is an MCP stdio server exposing calendar
// event and todo management tools to Rexion. It is a standalone binary wired
// in via Rexion.toml:
//
//	[[plugins]]
//	name    = "calendar"
//	command = "Rexion-plugin-calendar"
//
// Rexion then surfaces its tools as mcp__calendar__read_event / add_event /
// list_todo / add_todo / update_todo.
//
// Calendar integration:
//   - Windows: PowerShell COM automation with Outlook.Application
//   - macOS: icalBuddy / AppleScript with Calendar.app
//
// Todo storage uses a SQLite database at $REXION_HOME/data.db (default
// ~/.rexion/data.db).
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
	log.SetPrefix("Rexion-plugin-calendar: ")
	log.SetFlags(0)
	if err := serve(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// --- JSON-RPC framing (mirrors Rexion-plugin-office / -search) ---

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
			"serverInfo":      map[string]any{"name": "Rexion-plugin-calendar", "version": version},
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
	readEventTool,
	addEventTool,
	listTodoTool,
	addTodoTool,
	updateTodoTool,
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

// --- tool definitions ---

var readEventTool = toolDef{
	name:        "read_event",
	description: "Read calendar events from the system calendar within a date range",
	readOnly:    true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"start_date": map[string]any{"type": "string", "description": "ISO 8601 date (e.g. \"2026-07-01\")"},
			"end_date":   map[string]any{"type": "string", "description": "ISO 8601 date (e.g. \"2026-07-07\")"},
		},
		"required": []string{"start_date", "end_date"},
	},
	run: runReadEvent,
}

var addEventTool = toolDef{
	name:        "add_event",
	description: "Create a new calendar event in the system calendar",
	readOnly:    false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":       map[string]any{"type": "string", "description": "Event title"},
			"start_time":  map[string]any{"type": "string", "description": "ISO 8601 datetime (e.g. \"2026-07-01T14:00:00\")"},
			"end_time":    map[string]any{"type": "string", "description": "ISO 8601 datetime"},
			"description": map[string]any{"type": "string", "description": "Event description/notes"},
			"location":    map[string]any{"type": "string", "description": "Event location"},
		},
		"required": []string{"title", "start_time", "end_time"},
	},
	run: runAddEvent,
}

var listTodoTool = toolDef{
	name:        "list_todo",
	description: "List todo items from the Rexion todo database",
	readOnly:    true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{"type": "string", "description": "Filter by status: \"pending\", \"done\", or \"\" for all"},
			"limit":  map[string]any{"type": "integer", "description": "Maximum items to return (default 20)"},
		},
	},
	run: runListTodo,
}

var addTodoTool = toolDef{
	name:        "add_todo",
	description: "Add a new todo item to the Rexion todo list",
	readOnly:    false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":       map[string]any{"type": "string", "description": "Todo title"},
			"description": map[string]any{"type": "string", "description": "Detailed description"},
			"due_date":    map[string]any{"type": "string", "description": "ISO 8601 date for due date"},
			"priority":    map[string]any{"type": "string", "description": "Priority level: \"low\", \"medium\", or \"high\" (default \"medium\")"},
		},
		"required": []string{"title"},
	},
	run: runAddTodo,
}

var updateTodoTool = toolDef{
	name:        "update_todo",
	description: "Update an existing todo item's status or details",
	readOnly:    false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":        map[string]any{"type": "string", "description": "Todo item ID"},
			"status":    map[string]any{"type": "string", "description": "New status: \"pending\" or \"done\""},
			"title":     map[string]any{"type": "string", "description": "New title"},
			"due_date":  map[string]any{"type": "string", "description": "New due date"},
		},
		"required": []string{"id"},
	},
	run: runUpdateTodo,
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
	if !ok || s == "" {
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

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r' || b[len(b)-1] == '\n') {
		b = b[:len(b)-1]
	}
	return b
}

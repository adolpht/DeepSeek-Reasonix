// Command reasonix-plugin-im is an MCP stdio server exposing IM (Instant
// Messaging) bot integration tools to Reasonix. It supports WeCom (企业微信),
// Feishu (飞书), and DingTalk (钉钉) platforms for receiving remote commands
// and pushing results back.
//
// Wire it up in reasonix.toml:
//
//	[[plugins]]
//	name    = "im"
//	command = "reasonix-plugin-im"
//
// Reasonix then surfaces its tools as mcp__im__start_bot / stop_bot /
// send_message / list_pending_commands / mark_command_done.
//
// Environment variables:
//
//	IM_BOT_PORT       - HTTP listen port (default 9876)
//	IM_WECOM_KEY      - WeCom webhook key
//	IM_FEISHU_KEY     - Feishu webhook key
//	IM_DINGTALK_KEY   - DingTalk access token
//	IM_DINGTALK_SECRET - DingTalk signing secret
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
	log.SetPrefix("reasonix-plugin-im: ")
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
			"serverInfo":      map[string]any{"name": "reasonix-plugin-im", "version": version},
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
	startBotTool,
	stopBotTool,
	sendMessageTool,
	listPendingCommandsTool,
	markCommandDoneTool,
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

var startBotTool = toolDef{
	name:        "start_bot",
	description: "Start the local IM bot HTTP server to receive remote commands from WeCom/Feishu/DingTalk",
	readOnly:    false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"port":      map[string]any{"type": "integer", "description": "HTTP listen port", "default": 9876},
			"platforms": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"wecom", "feishu", "dingtalk"}}, "description": "Active platforms", "default": []string{"wecom"}},
			"token":     map[string]any{"type": "string", "description": "Verification token for incoming webhooks"},
		},
	},
	run: func(args map[string]any) (any, error) {
		port := argIntDefault(args, "port", 9876)
		platforms := argStringSliceDefault(args, "platforms", []string{"wecom"})
		token := argStringDefault(args, "token", "")
		return runStartBot(port, platforms, token)
	},
}

var stopBotTool = toolDef{
	name:        "stop_bot",
	description: "Stop the local IM bot HTTP server",
	readOnly:    false,
	schema: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	},
	run: func(args map[string]any) (any, error) {
		return runStopBot()
	},
}

var sendMessageTool = toolDef{
	name:        "send_message",
	description: "Send a message to a specified IM platform chat",
	readOnly:    false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"platform":     map[string]any{"type": "string", "enum": []string{"wecom", "feishu", "dingtalk"}, "description": "IM platform"},
			"webhook_url":  map[string]any{"type": "string", "description": "Platform webhook URL"},
			"content":      map[string]any{"type": "string", "description": "Message content (text or markdown)"},
			"msg_type":     map[string]any{"type": "string", "enum": []string{"text", "markdown"}, "description": "Message type", "default": "text"},
		},
		"required": []string{"platform", "webhook_url", "content"},
	},
	run: func(args map[string]any) (any, error) {
		platform, err := argString(args, "platform")
		if err != nil {
			return nil, err
		}
		webhookURL, err := argString(args, "webhook_url")
		if err != nil {
			return nil, err
		}
		content, err := argString(args, "content")
		if err != nil {
			return nil, err
		}
		msgType := argStringDefault(args, "msg_type", "text")
		return runSendMessage(platform, webhookURL, content, msgType)
	},
}

var listPendingCommandsTool = toolDef{
	name:        "list_pending_commands",
	description: "List pending remote commands received from IM platforms that haven't been executed yet",
	readOnly:    true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{"type": "integer", "description": "Maximum commands to return", "default": 20},
		},
	},
	run: func(args map[string]any) (any, error) {
		limit := argIntDefault(args, "limit", 20)
		return runListPendingCommands(limit)
	},
}

var markCommandDoneTool = toolDef{
	name:        "mark_command_done",
	description: "Mark a remote command as executed and push the result back to the IM platform",
	readOnly:    false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command_id": map[string]any{"type": "string", "description": "Command ID"},
			"result":     map[string]any{"type": "string", "description": "Execution result text"},
		},
		"required": []string{"command_id", "result"},
	},
	run: func(args map[string]any) (any, error) {
		commandID, err := argString(args, "command_id")
		if err != nil {
			return nil, err
		}
		result, err := argString(args, "result")
		if err != nil {
			return nil, err
		}
		return runMarkCommandDone(commandID, result)
	},
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

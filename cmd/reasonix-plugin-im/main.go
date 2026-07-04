// Command reasonix-plugin-im is an MCP stdio server exposing IM (Instant
// Messaging) bot integration tools to Reasonix. It supports WeCom (企业微信),
// Feishu (飞书), and DingTalk (钉钉) platforms for receiving remote commands
// and pushing results back.
//
// Two transport modes:
//   - Webhook mode (start_bot): local HTTP server, requires public IP or tunnel.
//   - Stream mode (start_stream): WebSocket long-connection, NO public IP needed
//     for DingTalk and Feishu. WeCom still requires webhook mode.
//
// Wire it up in reasonix.toml:
//
//	[[plugins]]
//	name    = "im"
//	command = "reasonix-plugin-im"
//
// Reasonix then surfaces its tools as mcp__im__start_bot / stop_bot /
// start_stream / stop_stream / send_message / list_pending_commands /
// mark_command_done.
//
// Environment variables:
//
//	IM_BOT_PORT          - HTTP listen port (default 9876, used when start_bot omits port)
//	IM_BOT_TOKEN         - Verification token for incoming webhooks (used when start_bot omits token)
//	IM_WECOM_KEY         - WeCom webhook key (also enables wecom platform when start_bot omits platforms)
//	IM_FEISHU_KEY        - Feishu webhook key (also enables feishu platform when start_bot omits platforms)
//	IM_DINGTALK_KEY      - DingTalk access token (also enables dingtalk platform when start_bot omits platforms)
//	IM_DINGTALK_SECRET   - DingTalk signing secret
//	IM_DINGTALK_APP_KEY  - DingTalk enterprise app AppKey (for Stream mode)
//	IM_DINGTALK_APP_SECRET - DingTalk enterprise app AppSecret (for Stream mode)
//	IM_FEISHU_APP_ID     - Feishu enterprise app App ID (for Stream mode)
//	IM_FEISHU_APP_SECRET - Feishu enterprise app App Secret (for Stream mode)
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
	"strings"
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
	startStreamTool,
	stopStreamTool,
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
			"port":      map[string]any{"type": "integer", "description": "HTTP listen port (falls back to IM_BOT_PORT env, default 9876)", "default": 9876},
			"platforms": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"wecom", "feishu", "dingtalk"}}, "description": "Active platforms; auto-detected from IM_*_KEY env when omitted", "default": []string{"wecom"}},
			"token":     map[string]any{"type": "string", "description": "Verification token for incoming webhooks (falls back to IM_BOT_TOKEN env)"},
		},
	},
	run: func(args map[string]any) (any, error) {
		port := argIntDefault(args, "port", envInt("IM_BOT_PORT", 9876))
		platforms := resolvePlatforms(args)
		token := argStringDefault(args, "token", envString("IM_BOT_TOKEN", ""))
		return runStartBot(port, platforms, token)
	},
}

// resolvePlatforms returns the explicit `platforms` arg when provided;
// otherwise it auto-detects from IM_WECOM_KEY / IM_FEISHU_KEY /
// IM_DINGTALK_KEY env vars, so users only need to set the keys for the
// platforms they actually use. Falls back to ["wecom"] when no signal.
func resolvePlatforms(args map[string]any) []string {
	if v, ok := args["platforms"]; ok && v != nil {
		if arr, ok := v.([]any); ok && len(arr) > 0 {
			out := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	var detected []string
	if os.Getenv("IM_WECOM_KEY") != "" {
		detected = append(detected, "wecom")
	}
	if os.Getenv("IM_FEISHU_KEY") != "" {
		detected = append(detected, "feishu")
	}
	if os.Getenv("IM_DINGTALK_KEY") != "" {
		detected = append(detected, "dingtalk")
	}
	if len(detected) > 0 {
		return detected
	}
	return []string{"wecom"}
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

var startStreamTool = toolDef{
	name:        "start_stream",
	description: "Start long-connection (WebSocket) mode for DingTalk/Feishu — NO public IP needed. The bot connects outbound to the platform gateway. WeCom is NOT supported (use start_bot for WeCom). Platforms auto-detected from IM_DINGTALK_APP_KEY / IM_FEISHU_APP_ID env when omitted.",
	readOnly:    false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"platforms": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": []string{"dingtalk", "feishu"}},
				"description": "Platforms to start in stream mode; auto-detected from IM_DINGTALK_APP_KEY / IM_FEISHU_APP_ID env when omitted",
				"default":     []string{"dingtalk"},
			},
			"dingtalk_app_key":    map[string]any{"type": "string", "description": "DingTalk AppKey (falls back to IM_DINGTALK_APP_KEY env)"},
			"dingtalk_app_secret": map[string]any{"type": "string", "description": "DingTalk AppSecret (falls back to IM_DINGTALK_APP_SECRET env)"},
			"feishu_app_id":       map[string]any{"type": "string", "description": "Feishu App ID (falls back to IM_FEISHU_APP_ID env)"},
			"feishu_app_secret":   map[string]any{"type": "string", "description": "Feishu App Secret (falls back to IM_FEISHU_APP_SECRET env)"},
		},
	},
	run: func(args map[string]any) (any, error) {
		platforms := resolveStreamPlatforms(args)
		var results []string
		for _, p := range platforms {
			switch p {
			case "dingtalk":
				appKey := argStringDefault(args, "dingtalk_app_key", envString("IM_DINGTALK_APP_KEY", ""))
				appSecret := argStringDefault(args, "dingtalk_app_secret", envString("IM_DINGTALK_APP_SECRET", ""))
				r, err := runStartDingTalkStream(appKey, appSecret)
				if err != nil {
					results = append(results, fmt.Sprintf("dingtalk: ERROR: %v", err))
				} else {
					results = append(results, fmt.Sprintf("dingtalk: %v", r))
				}
			case "feishu":
				appID := argStringDefault(args, "feishu_app_id", envString("IM_FEISHU_APP_ID", ""))
				appSecret := argStringDefault(args, "feishu_app_secret", envString("IM_FEISHU_APP_SECRET", ""))
				r, err := runStartFeishuStream(appID, appSecret)
				if err != nil {
					results = append(results, fmt.Sprintf("feishu: ERROR: %v", err))
				} else {
					results = append(results, fmt.Sprintf("feishu: %v", r))
				}
			}
		}
		return strings.Join(results, "\n"), nil
	},
}

var stopStreamTool = toolDef{
	name:        "stop_stream",
	description: "Stop long-connection (Stream) mode for DingTalk/Feishu. Stops all running stream platforms when platforms omitted.",
	readOnly:    false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"platforms": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": []string{"dingtalk", "feishu"}},
				"description": "Platforms to stop; defaults to all running",
			},
		},
	},
	run: func(args map[string]any) (any, error) {
		wantAll := true
		var want map[string]bool
		if v, ok := args["platforms"]; ok && v != nil {
			if arr, ok := v.([]any); ok && len(arr) > 0 {
				wantAll = false
				want = make(map[string]bool)
				for _, item := range arr {
					if s, ok := item.(string); ok {
						want[s] = true
					}
				}
			}
		}
		var results []string
		if wantAll || want["dingtalk"] {
			r, err := runStopDingTalkStream()
			if err != nil {
				results = append(results, fmt.Sprintf("dingtalk: ERROR: %v", err))
			} else {
				results = append(results, fmt.Sprintf("dingtalk: %v", r))
			}
		}
		if wantAll || want["feishu"] {
			r, err := runStopFeishuStream()
			if err != nil {
				results = append(results, fmt.Sprintf("feishu: ERROR: %v", err))
			} else {
				results = append(results, fmt.Sprintf("feishu: %v", r))
			}
		}
		return strings.Join(results, "\n"), nil
	},
}

// resolveStreamPlatforms returns the explicit `platforms` arg when provided;
// otherwise auto-detects from IM_DINGTALK_APP_KEY / IM_FEISHU_APP_ID env vars.
// Falls back to ["dingtalk"] when no signal.
func resolveStreamPlatforms(args map[string]any) []string {
	if v, ok := args["platforms"]; ok && v != nil {
		if arr, ok := v.([]any); ok && len(arr) > 0 {
			out := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	var detected []string
	if os.Getenv("IM_DINGTALK_APP_KEY") != "" {
		detected = append(detected, "dingtalk")
	}
	if os.Getenv("IM_FEISHU_APP_ID") != "" {
		detected = append(detected, "feishu")
	}
	if len(detected) > 0 {
		return detected
	}
	return []string{"dingtalk"}
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

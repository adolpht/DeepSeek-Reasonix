// Command rexion-plugin-design is an MCP stdio server exposing design-to-code
// tools to Rexion. It is a standalone binary wired in via Rexion.toml:
//
//	[[plugins]]
//	name    = "design"
//	command = "rexion-plugin-design"
//
// Rexion then surfaces its tools as mcp__design__design_upload /
// design_analyze / design_to_code / figma_import / design_compare.
//
// The plugin turns design mockups (image or Figma URL) into structured layout
// analysis and framework code (HTML/Vue/React). Layout analysis is delegated to
// an OpenAI-compatible vision API (configured via DESIGN_LLM_API_KEY /
// DESIGN_LLM_BASE_URL / DESIGN_LLM_MODEL). Only the Go standard library is used.
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
	log.SetPrefix("rexion-plugin-design: ")
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
			"serverInfo":      map[string]any{"name": "rexion-plugin-design", "version": version},
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

// tools 注册所有设计转代码工具
var tools = []toolDef{
	{
		name:        "design_upload",
		description: "Upload a design mockup image (base64-encoded) and save it to a temp directory. Returns the saved image path and a preview data URL.",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"image_base64": map[string]any{"type": "string", "description": "Base64-encoded image bytes (with or without a data URL prefix)"},
				"filename":     map[string]any{"type": "string", "description": "Optional filename hint (default: design-<timestamp>.png)"},
			},
			"required": []string{"image_base64"},
		},
		run: runDesignUpload,
	},
	{
		name:        "design_analyze",
		description: "Analyze a design mockup and return a structured layout tree (components, colors, fonts, spacing). Uses an OpenAI-compatible vision API configured via DESIGN_LLM_API_KEY / DESIGN_LLM_BASE_URL / DESIGN_LLM_MODEL.",
		readOnly:    true,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"image_path":    map[string]any{"type": "string", "description": "Path to a saved design image (mutually exclusive with image_base64)"},
				"image_base64":  map[string]any{"type": "string", "description": "Base64-encoded image bytes (used when image_path is absent)"},
				"framework":     map[string]any{"type": "string", "enum": []string{"html", "vue", "react"}, "description": "Target framework for downstream code generation (default: html)"},
				"style":         map[string]any{"type": "string", "enum": []string{"tailwind", "css"}, "description": "Preferred styling strategy (default: css)"},
			},
		},
		run: runDesignAnalyze,
	},
	{
		name:        "design_to_code",
		description: "Generate framework code (HTML/CSS or Vue/React component) from a design_analyze result. Returns complete file contents.",
		readOnly:    false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"analysis":      map[string]any{"type": "string", "description": "The analysis JSON returned by design_analyze"},
				"framework":     map[string]any{"type": "string", "enum": []string{"html", "vue", "react"}, "description": "Target framework (default: html)"},
				"style":         map[string]any{"type": "string", "enum": []string{"tailwind", "css"}, "description": "Styling strategy (default: css)"},
				"component_name": map[string]any{"type": "string", "description": "Component name for Vue/React output (default: DesignComponent)"},
			},
		},
		run: runDesignToCode,
	},
	{
		name:        "figma_import",
		description: "Import a design from a Figma file URL. Calls the Figma REST API (token from FIGMA_TOKEN env or figma_token argument) and returns a simplified layout tree.",
		readOnly:    true,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"figma_url":  map[string]any{"type": "string", "description": "Figma file URL (https://www.figma.com/file/<key>/... or https://www.figma.com/design/<key>/...)"},
				"figma_token": map[string]any{"type": "string", "description": "Figma personal access token (alternatively set FIGMA_TOKEN env)"},
				"node_id":    map[string]any{"type": "string", "description": "Optional specific node id to import (default: root canvas)"},
			},
			"required": []string{"figma_url"},
		},
		run: runFigmaImport,
	},
	{
		name:        "design_compare",
		description: "Compare a design mockup against a generated-code screenshot and return a difference analysis. Both inputs are base64 images.",
		readOnly:    true,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"design_image":    map[string]any{"type": "string", "description": "Base64-encoded original design image"},
				"screenshot_image": map[string]any{"type": "string", "description": "Base64-encoded screenshot of the generated code"},
			},
			"required": []string{"design_image", "screenshot_image"},
		},
		run: runDesignCompare,
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
		if m, ok := result.(map[string]any); ok {
			return m, nil
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

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r' || b[len(b)-1] == '\n') {
		b = b[:len(b)-1]
	}
	return b
}

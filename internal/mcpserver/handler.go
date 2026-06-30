package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/event"
)

// MCP protocol version supported by this server.
const protocolVersion = "2024-11-05"

// JSON-RPC 2.0 error codes.
const (
	errParseError     = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternal       = -32603
)

var errUnknownTransport = errors.New("unknown transport: must be stdio or http")

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// IsNotification reports whether the request is a notification (no ID).
func (r *Request) IsNotification() bool {
	return len(r.ID) == 0
}

// handleRequest dispatches an incoming JSON-RPC request to the appropriate handler.
func (s *Server) handleRequest(ctx context.Context, req Request) Response {
	if req.JSONRPC != "2.0" {
		return errorResponse(req.ID, errInvalidRequest, "invalid jsonrpc version")
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized":
		// Client notification that initialization is complete; nothing to do.
		s.mu.Lock()
		s.initialized = true
		s.mu.Unlock()
		return Response{JSONRPC: "2.0", ID: req.ID}
	case "ping":
		return s.handlePing(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		return errorResponse(req.ID, errMethodNotFound, "method not found: "+req.Method)
	}
}

// handleInitialize handles the MCP initialize request.
func (s *Server) handleInitialize(req Request) Response {
	result := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{
				"listChanged": false,
			},
		},
		"serverInfo": map[string]any{
			"name":    "reasonix",
			"version": "1.0.0",
		},
	}
	return Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// handlePing handles the MCP ping request.
func (s *Server) handlePing(req Request) Response {
	return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
}

// handleToolsList handles the MCP tools/list request.
func (s *Server) handleToolsList(req Request) Response {
	return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"tools": tools,
	}}
}

// toolsCallParams holds the parameters for a tools/call request.
type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// handleToolsCall handles the MCP tools/call request by dispatching to the
// controller. Output is collected from the controller's event sink.
func (s *Server) handleToolsCall(ctx context.Context, req Request) Response {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, errInvalidParams, "invalid params: "+err.Error())
	}

	// Validate the tool name.
	valid := false
	for _, t := range tools {
		if t.Name == params.Name {
			valid = true
			break
		}
	}
	if !valid {
		return errorResponse(req.ID, errInvalidParams, "unknown tool: "+params.Name)
	}

	prompt := buildPrompt(params.Name, params.Arguments)
	if prompt == "" {
		return errorResponse(req.ID, errInvalidParams, "empty prompt")
	}

	sbMode := resolveSandboxMode(params.Name, params.Arguments)
	s.controller.SetSandboxMode(sbMode)

	// Create a collector to capture events during execution.
	collector := &textCollector{}

	// Route events to both the base sink and our collector.
	if s.sinkRouter != nil {
		s.sinkRouter.SetExtra(collector)
		defer s.sinkRouter.SetExtra(nil)
	}

	err := s.controller.Run(ctx, prompt)
	if err != nil {
		return toolErrorResponse(req.ID, fmt.Sprintf("execution failed: %s", err.Error()))
	}

	output := collector.Text()
	if output == "" {
		output = "(task completed with no text output)"
	}

	return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": output},
		},
	}}
}

// textCollector accumulates text output from the agent event stream.
type textCollector struct {
	mu   sync.Mutex
	text strings.Builder
}

// Emit implements event.Sink.
func (tc *textCollector) Emit(e event.Event) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	switch e.Kind {
	case event.Text:
		tc.text.WriteString(e.Text)
	case event.Message:
		// Full message: reset and write the complete text.
		tc.text.Reset()
		tc.text.WriteString(e.Text)
	case event.Reasoning:
		// Skip reasoning in MCP output by default.
	case event.ToolResult:
		if e.Tool.Output != "" {
			tc.text.WriteString("\n")
			tc.text.WriteString(e.Tool.Output)
		}
		if e.Tool.Err != "" {
			tc.text.WriteString("\n[error] ")
			tc.text.WriteString(e.Tool.Err)
		}
	case event.Notice:
		tc.text.WriteString("\n[note] ")
		tc.text.WriteString(e.Text)
	case event.TurnDone:
		// Capture turn-level errors via the Err field.
		if e.Err != nil {
			tc.text.WriteString("\n[error] ")
			tc.text.WriteString(e.Err.Error())
		}
	}
}

// Text returns the collected text.
func (tc *textCollector) Text() string {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return strings.TrimSpace(tc.text.String())
}

// errorResponse creates a JSON-RPC error response.
func errorResponse(id json.RawMessage, code int, message string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}

// toolErrorResponse creates a JSON-RPC response with an MCP tool error result.
func toolErrorResponse(id json.RawMessage, message string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": message},
			},
			"isError": true,
		},
	}
}

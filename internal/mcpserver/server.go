// Package mcpserver implements an MCP (Model Context Protocol) tool server that
// exposes Rexion as a callable tool server for other agents (Claude Code,
// Cursor, Copilot, etc.). It supports both stdio and HTTP transports and
// follows the MCP specification version 2024-11-05.
package mcpserver

import (
	"context"
	"sync"

	"rexion/internal/control"
	"rexion/internal/event"
	"rexion/internal/sandbox"
)

// Server is an MCP tool server that exposes Rexion capabilities.
type Server struct {
	controller *control.Controller
	transport  string // "stdio" | "http"
	addr       string // HTTP listen address
	mu         sync.Mutex
	initialized bool
	// sinkRouter is a sink that can dynamically route events to different
	// collectors. It is passed to the controller at construction time.
	sinkRouter *sinkRouter
	// sessions tracks active HTTP transport sessions.
	sessions *sessionManager
}

// sinkRouter is an event.Sink that can dynamically route events to a
// per-request collector while always forwarding to the base sink.
type sinkRouter struct {
	mu     sync.Mutex
	base   event.Sink
	extra  event.Sink
}

func (r *sinkRouter) Emit(e event.Event) {
	r.mu.Lock()
	base := r.base
	extra := r.extra
	r.mu.Unlock()
	if base != nil {
		base.Emit(e)
	}
	if extra != nil {
		extra.Emit(e)
	}
}

// SetExtra sets an additional sink that receives events alongside the base.
func (r *sinkRouter) SetExtra(s event.Sink) {
	r.mu.Lock()
	r.extra = s
	r.mu.Unlock()
}

// ToolDef describes an MCP tool exposed by the server.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any  `json:"inputSchema"`
}

// tools are the MCP tool definitions this server exposes.
var tools = []ToolDef{
	{
		Name:        "Rexion_code",
		Description: "Execute a coding task: write, edit, or refactor code. The agent has full access to file writing and shell commands within the workspace.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "The coding task to execute",
				},
				"sandbox_mode": map[string]any{
					"type":        "string",
					"description": "Sandbox mode: read-only, workspace-write, full-access",
					"enum":        []string{"read-only", "workspace-write", "full-access"},
				},
			},
			"required": []string{"prompt"},
		},
	},
	{
		Name:        "Rexion_explore",
		Description: "Explore and analyze code without making changes. Read-only: can search, read files, and run read-only commands.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "What to explore or analyze",
				},
			},
			"required": []string{"prompt"},
		},
	},
	{
		Name:        "Rexion_review",
		Description: "Review code for quality, correctness, and best practices. Read-only analysis with structured feedback.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target": map[string]any{
					"type":        "string",
					"description": "File, directory, or description of what to review",
				},
				"focus": map[string]any{
					"type":        "string",
					"description": "Review focus: correctness, security, performance, style, or general",
				},
			},
			"required": []string{"target"},
		},
	},
	{
		Name:        "Rexion_test",
		Description: "Run tests and optionally fix failures. Can execute test commands and repair failing tests.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Test command to run",
				},
				"fix": map[string]any{
					"type":        "boolean",
					"description": "Whether to attempt fixing failures (default: false)",
				},
			},
			"required": []string{"command"},
		},
	},
}

// NewServer creates a new MCP server wrapping the given controller.
// The sinkRouter must have been passed as the controller's sink at
// construction time so events flow through it.
func NewServer(ctrl *control.Controller, transport, addr string, router *sinkRouter) *Server {
	return &Server{
		controller:  ctrl,
		transport:   transport,
		addr:        addr,
		sinkRouter:  router,
		sessions:    newSessionManager(),
	}
}

// NewSinkRouter creates a sinkRouter that forwards to base.
func NewSinkRouter(base event.Sink) *sinkRouter {
	return &sinkRouter{base: base}
}

// Sink returns the sinkRouter to pass to boot.Build as the event sink.
func (s *Server) Sink() event.Sink {
	return s.sinkRouter
}

// Run starts the MCP server using the configured transport and blocks until
// the context is cancelled or a fatal error occurs.
func (s *Server) Run(ctx context.Context) error {
	switch s.transport {
	case "stdio":
		return s.runStdio(ctx)
	case "http":
		return s.runHTTP(ctx)
	default:
		return errUnknownTransport
	}
}

// resolveSandboxMode maps an MCP tool name to the appropriate sandbox mode.
func resolveSandboxMode(toolName string, params map[string]any) sandbox.SandboxMode {
	if raw, ok := params["sandbox_mode"]; ok {
		if str, ok := raw.(string); ok {
			return sandbox.NormalizeSandboxMode(str)
		}
	}
	switch toolName {
	case "Rexion_explore", "Rexion_review":
		return sandbox.SandboxReadOnly
	case "Rexion_test":
		if fix, _ := params["fix"]; fix == true {
			return sandbox.SandboxWorkspaceWrite
		}
		return sandbox.SandboxReadOnly
	case "Rexion_code":
		return sandbox.SandboxWorkspaceWrite
	default:
		return sandbox.SandboxWorkspaceWrite
	}
}

// buildPrompt constructs the controller prompt from tool name and params.
func buildPrompt(toolName string, params map[string]any) string {
	switch toolName {
	case "Rexion_code":
		prompt, _ := params["prompt"].(string)
		return prompt
	case "Rexion_explore":
		prompt, _ := params["prompt"].(string)
		return "Explore and analyze the following (read-only, do not modify files): " + prompt
	case "Rexion_review":
		target, _ := params["target"].(string)
		focus, _ := params["focus"].(string)
		if focus != "" {
			return "Review the following with focus on " + focus + " (read-only, do not modify files): " + target
		}
		return "Review the following (read-only, do not modify files): " + target
	case "Rexion_test":
		command, _ := params["command"].(string)
		fix, _ := params["fix"].(bool)
		if fix {
			return "Run the test command and fix any failures: " + command
		}
		return "Run the test command and report results (read-only, do not fix): " + command
	default:
		if p, ok := params["prompt"].(string); ok {
			return p
		}
		return ""
	}
}

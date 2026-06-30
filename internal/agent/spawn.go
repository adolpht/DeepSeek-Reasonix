package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"reasonix/internal/tool"
)

// SpawnAgentTool spawns a child agent with a specified role.
type SpawnAgentTool struct {
	pool *Pool
}

func NewSpawnAgentTool(pool *Pool) *SpawnAgentTool {
	return &SpawnAgentTool{pool: pool}
}

func (t *SpawnAgentTool) Name() string { return "spawn_agent" }
func (t *SpawnAgentTool) Description() string {
	return "Spawn a child agent with a specific role. The child agent runs its own full agent loop in parallel with other agents. Use roles: default (general), worker (code changes), explorer (read-only research), or monitor (watch commands). Returns the agent ID immediately."
}
func (t *SpawnAgentTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "prompt":{"type":"string","description":"Task description for the child agent"},
  "role":{"type":"string","description":"Role: default, worker, explorer, or monitor","enum":["default","worker","explorer","monitor"]},
  "description":{"type":"string","description":"Short label for display"},
  "model":{"type":"string","description":"Optional model override (provider/model name)"},
  "effort":{"type":"string","description":"Optional reasoning effort (e.g. high, max)"},
  "max_steps":{"type":"integer","description":"Max tool-call rounds override","minimum":1},
  "tools":{"type":"array","items":{"type":"string"},"description":"Tool whitelist for the child agent"}
},
"required":["prompt"]
}`)
}
func (t *SpawnAgentTool) ReadOnly() bool { return false }
func (t *SpawnAgentTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Prompt      string   `json:"prompt"`
		Role        string   `json:"role"`
		Description string   `json:"description"`
		Model       string   `json:"model"`
		Effort      string   `json:"effort"`
		MaxSteps    int      `json:"max_steps"`
		Tools       []string `json:"tools"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	role := ResolveRole(p.Role, nil)
	if len(p.Tools) > 0 {
		role.Tools = p.Tools
	}

	parentID, _, _, ok := CallContext(ctx)
	agentID := fmt.Sprintf("agent-%d", time.Now().UnixNano())
	if ok && parentID != "" {
		agentID = parentID + "/" + agentID
	}

	child, err := t.pool.Spawn(ctx, agentID, role, p.Prompt, p.Model, p.Effort, p.MaxSteps)
	if err != nil {
		return "", err
	}

	label := p.Description
	if label == "" {
		label = p.Role
		if label == "" {
			label = "default"
		}
	}

	return fmt.Sprintf("Spawned %s agent %q (%s). Use wait_agent with id %q to get results.", role.Name, child.ID, label, child.ID), nil
}

// WaitAgentTool waits for a child agent to complete.
type WaitAgentTool struct {
	pool *Pool
}

func NewWaitAgentTool(pool *Pool) *WaitAgentTool {
	return &WaitAgentTool{pool: pool}
}

func (t *WaitAgentTool) Name() string { return "wait_agent" }
func (t *WaitAgentTool) Description() string {
	return "Wait for a spawned child agent to finish and return its result. Blocks until the agent completes or the timeout expires."
}
func (t *WaitAgentTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "agent_id":{"type":"string","description":"ID of the child agent to wait for"},
  "timeout":{"type":"integer","description":"Timeout in seconds (default 300)"}
},
"required":["agent_id"]
}`)
}
func (t *WaitAgentTool) ReadOnly() bool { return true }
func (t *WaitAgentTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		AgentID string `json:"agent_id"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.AgentID == "" {
		return "", fmt.Errorf("agent_id is required")
	}
	timeout := time.Duration(p.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	result, err := t.pool.Wait(ctx, p.AgentID, timeout)
	if err != nil {
		return "", err
	}
	if result.Error != nil {
		return fmt.Sprintf("Agent %s failed after %s: %v\nPartial result: %s", p.AgentID, result.Duration.Round(time.Second), result.Error, truncateString(result.Summary, 500)), nil
	}
	return fmt.Sprintf("Agent %s completed in %s (tool calls: %d, tokens: %d)\n\n%s",
		p.AgentID, result.Duration.Round(time.Second), result.ToolCalls, result.Usage.TotalTokens, result.Summary), nil
}

// SendInputTool sends additional instructions to a completed child agent.
type SendInputTool struct {
	pool *Pool
}

func NewSendInputTool(pool *Pool) *SendInputTool {
	return &SendInputTool{pool: pool}
}

func (t *SendInputTool) Name() string { return "send_input" }
func (t *SendInputTool) Description() string {
	return "Send additional instructions to a completed child agent to continue its work. The agent will be restarted with the new message. Running agents must be waited on first."
}
func (t *SendInputTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "agent_id":{"type":"string","description":"ID of the target child agent"},
  "message":{"type":"string","description":"Additional instructions to send"}
},
"required":["agent_id","message"]
}`)
}
func (t *SendInputTool) ReadOnly() bool { return true }
func (t *SendInputTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		AgentID string `json:"agent_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.AgentID == "" {
		return "", fmt.Errorf("agent_id is required")
	}
	if p.Message == "" {
		return "", fmt.Errorf("message is required")
	}
	if err := t.pool.SendInput(ctx, p.AgentID, p.Message); err != nil {
		return "", err
	}
	return fmt.Sprintf("Input sent to agent %s. Use wait_agent to get the updated result.", p.AgentID), nil
}

// CloseAgentTool terminates a running child agent.
type CloseAgentTool struct {
	pool *Pool
}

func NewCloseAgentTool(pool *Pool) *CloseAgentTool {
	return &CloseAgentTool{pool: pool}
}

func (t *CloseAgentTool) Name() string { return "close_agent" }
func (t *CloseAgentTool) Description() string {
	return "Terminate a running child agent. Returns its partial result."
}
func (t *CloseAgentTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "agent_id":{"type":"string","description":"ID of the child agent to terminate"}
},
"required":["agent_id"]
}`)
}
func (t *CloseAgentTool) ReadOnly() bool { return true }
func (t *CloseAgentTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.AgentID == "" {
		return "", fmt.Errorf("agent_id is required")
	}
	if err := t.pool.Close(ctx, p.AgentID); err != nil {
		return "", err
	}
	result, _ := t.pool.GetResult(p.AgentID)
	if result != nil {
		summary := result.Summary
		if summary == "" {
			summary = "(no partial result)"
		}
		return fmt.Sprintf("Agent %s closed. Duration: %s\nPartial result: %s", p.AgentID, result.Duration.Round(time.Second), truncateString(summary, 500)), nil
	}
	return fmt.Sprintf("Agent %s closed.", p.AgentID), nil
}

var (
	_ tool.Tool = (*SpawnAgentTool)(nil)
	_ tool.Tool = (*WaitAgentTool)(nil)
	_ tool.Tool = (*SendInputTool)(nil)
	_ tool.Tool = (*CloseAgentTool)(nil)
)

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

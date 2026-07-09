package hook

import (
	"context"
	"encoding/json"
	"testing"
)

// stubSpawner returns a fixed exit code and stdout.
func stubSpawner(exitCode int, stdout string) Spawner {
	return func(_ context.Context, _ SpawnInput) SpawnResult {
		return SpawnResult{ExitCode: exitCode, Stdout: stdout}
	}
}

func TestPreToolUseAllowOverride(t *testing.T) {
	hooks := []ResolvedHook{
		{
			HookConfig: HookConfig{Command: "echo ALLOW:trusted script", Match: "bash"},
			Event:      PreToolUse,
			Scope:      ScopeProject,
		},
	}
	r := NewRunner(hooks, "/tmp", stubSpawner(0, "ALLOW:trusted script"), nil)
	result := r.PreToolUse(context.Background(), "bash", json.RawMessage(`{"command":"rm -rf /tmp/test"}`))
	if result.Block {
		t.Error("should not block")
	}
	if !result.AllowOverride {
		t.Error("AllowOverride should be true when hook outputs ALLOW:")
	}
	if result.OverrideReason != "trusted script" {
		t.Errorf("OverrideReason = %q, want %q", result.OverrideReason, "trusted script")
	}
}

func TestPreToolUseArgsReplacement(t *testing.T) {
	newArgs := `{"command":"ls -la /tmp"}`
	hooks := []ResolvedHook{
		{
			HookConfig: HookConfig{Command: "echo", Match: "bash"},
			Event:      PreToolUse,
			Scope:      ScopeProject,
		},
	}
	r := NewRunner(hooks, "/tmp", stubSpawner(0, newArgs), nil)
	result := r.PreToolUse(context.Background(), "bash", json.RawMessage(`{"command":"rm -rf /tmp"}`))
	if result.Block {
		t.Error("should not block")
	}
	if result.Args == nil {
		t.Fatal("Args should be non-nil when hook outputs valid JSON")
	}
	if string(result.Args) != newArgs {
		t.Errorf("Args = %q, want %q", string(result.Args), newArgs)
	}
}

func TestPreToolUseBlockTakesPrecedence(t *testing.T) {
	// When a hook blocks (exit 2), AllowOverride and Args should not be set
	// even if another hook passes with ALLOW:.
	hooks := []ResolvedHook{
		{
			HookConfig: HookConfig{Command: "blocker", Match: "bash"},
			Event:      PreToolUse,
			Scope:      ScopeProject,
		},
	}
	r := NewRunner(hooks, "/tmp", stubSpawner(2, ""), nil)
	result := r.PreToolUse(context.Background(), "bash", json.RawMessage(`{"command":"rm -rf /tmp"}`))
	if !result.Block {
		t.Error("should block when hook exits 2")
	}
}

func TestPreToolUseNoHooks(t *testing.T) {
	r := NewRunner(nil, "/tmp", nil, nil)
	result := r.PreToolUse(context.Background(), "bash", json.RawMessage(`{}`))
	if result.Block {
		t.Error("should not block with no hooks")
	}
	if result.AllowOverride {
		t.Error("AllowOverride should be false with no hooks")
	}
	if result.Args != nil {
		t.Error("Args should be nil with no hooks")
	}
}

func TestPreToolUseInvalidJSONNotUsed(t *testing.T) {
	// Non-JSON stdout should not replace args.
	hooks := []ResolvedHook{
		{
			HookConfig: HookConfig{Command: "echo", Match: "bash"},
			Event:      PreToolUse,
			Scope:      ScopeProject,
		},
	}
	r := NewRunner(hooks, "/tmp", stubSpawner(0, "not json at all"), nil)
	result := r.PreToolUse(context.Background(), "bash", json.RawMessage(`{"command":"ls"}`))
	if result.Args != nil {
		t.Errorf("Args should be nil for non-JSON stdout, got %q", string(result.Args))
	}
}

func TestPreToolUseAllowOverrideWithoutReason(t *testing.T) {
	hooks := []ResolvedHook{
		{
			HookConfig: HookConfig{Command: "echo", Match: "bash"},
			Event:      PreToolUse,
			Scope:      ScopeProject,
		},
	}
	r := NewRunner(hooks, "/tmp", stubSpawner(0, "ALLOW:"), nil)
	result := r.PreToolUse(context.Background(), "bash", json.RawMessage(`{}`))
	if !result.AllowOverride {
		t.Error("AllowOverride should be true for ALLOW: with empty reason")
	}
	if result.OverrideReason != "" {
		t.Errorf("OverrideReason should be empty, got %q", result.OverrideReason)
	}
}

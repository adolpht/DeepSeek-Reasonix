package permission

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGateBashValidation_BlocksDestructiveReadOnly(t *testing.T) {
	policy := Policy{Mode: Allow} // even in allow mode, bash validation blocks
	gate := &Gate{Policy: policy, WorkspaceRoot: "/workspace"}

	// rm should be blocked in read-only mode by bash validation
	args := json.RawMessage(`{"command":"rm -rf /tmp/test"}`)
	allow, reason, err := gate.Check(context.Background(), "bash", args, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allow {
		t.Fatalf("rm -rf should be blocked in read-only mode, got allow=true reason=%q", reason)
	}
	if reason == "" {
		t.Fatalf("expected a block reason")
	}
}

func TestGateBashValidation_WarnsDestructive(t *testing.T) {
	policy := Policy{Mode: Allow}
	gate := &Gate{Policy: policy, WorkspaceRoot: "/workspace"}

	// rm -rf is allowed but warned in non-read-only mode
	args := json.RawMessage(`{"command":"rm -rf /tmp/test"}`)
	allow, reason, err := gate.Check(context.Background(), "bash", args, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allow {
		t.Fatalf("rm -rf should be allowed (warn only) in non-read-only mode, got allow=false")
	}
	if reason == "" {
		t.Fatalf("expected a warning reason for rm -rf")
	}
	if reason[:5] != "warn:" {
		t.Fatalf("warning reason should start with 'warn:', got %q", reason)
	}
}

func TestGateBashValidation_AllowsSafe(t *testing.T) {
	policy := Policy{Mode: Allow}
	gate := &Gate{Policy: policy, WorkspaceRoot: "/workspace"}

	// ls should be allowed without warning
	args := json.RawMessage(`{"command":"ls -la"}`)
	allow, reason, err := gate.Check(context.Background(), "bash", args, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allow {
		t.Fatalf("ls should be allowed, got allow=false reason=%q", reason)
	}
	if reason != "" {
		t.Fatalf("ls should have no warning, got reason=%q", reason)
	}
}

func TestGateBashValidation_BlocksSedInPlace(t *testing.T) {
	policy := Policy{Mode: Allow}
	gate := &Gate{Policy: policy, WorkspaceRoot: "/workspace"}

	// sed -i should be blocked in read-only mode
	args := json.RawMessage(`{"command":"sed -i 's/old/new/g' file.txt"}`)
	allow, reason, err := gate.Check(context.Background(), "bash", args, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allow {
		t.Fatalf("sed -i should be blocked in read-only mode, got allow=true reason=%q", reason)
	}
}

func TestGateBashValidation_WarnsPathTraversal(t *testing.T) {
	policy := Policy{Mode: Allow}
	gate := &Gate{Policy: policy, WorkspaceRoot: "/workspace"}

	// cat with ../ traversal outside workspace should warn
	args := json.RawMessage(`{"command":"cat ../../etc/passwd"}`)
	allow, reason, err := gate.Check(context.Background(), "bash", args, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allow {
		t.Fatalf("cat with path traversal should be allowed (warn only), got allow=false")
	}
	if reason == "" || reason[:5] != "warn:" {
		t.Fatalf("expected warn: prefix, got reason=%q", reason)
	}
}

func TestGateBashValidation_NonBypassedForNonBash(t *testing.T) {
	policy := Policy{Mode: Allow}
	gate := &Gate{Policy: policy, WorkspaceRoot: "/workspace"}

	// Non-bash tools should not be affected by bash validation
	args := json.RawMessage(`{"path":"/etc/passwd"}`)
	allow, reason, err := gate.Check(context.Background(), "read_file", args, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allow {
		t.Fatalf("read_file should not be affected by bash validation, got allow=false reason=%q", reason)
	}
	if reason != "" {
		t.Fatalf("read_file should have no warning, got reason=%q", reason)
	}
}

func TestGateBashValidation_WorkspaceRootUsed(t *testing.T) {
	policy := Policy{Mode: Allow}
	gate := &Gate{Policy: policy, WorkspaceRoot: "/workspace/project"}

	// ls within workspace should be clean
	args := json.RawMessage(`{"command":"ls /workspace/project/src"}`)
	allow, _, err := gate.Check(context.Background(), "bash", args, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allow {
		t.Fatalf("ls within workspace should be allowed")
	}
}

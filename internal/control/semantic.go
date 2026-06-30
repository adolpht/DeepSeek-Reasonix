package control

import (
	"context"
	"errors"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// wrapSemanticError classifies a runner error into a SemanticError with an
// appropriate exit code for CI/CD headless consumption. Nil errors and errors
// that are already SemanticError pass through unchanged.
func wrapSemanticError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	// Already semantic — pass through.
	var se *agent.SemanticError
	if errors.As(err, &se) {
		return err
	}
	// Context deadline / timeout.
	if ctx.Err() == context.DeadlineExceeded {
		return agent.NewTimeoutError("execution timed out", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return agent.NewTimeoutError("execution timed out", err)
	}
	// Permission denied (gate blocked the call).
	if isPermissionBlocked(err) {
		return agent.NewPermissionError("permission denied in non-interactive mode", err)
	}
	// API / auth errors.
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		return agent.NewAPIError("model API error", apiErr)
	}
	var authErr *provider.AuthError
	if errors.As(err, &authErr) {
		return agent.NewAPIError("authentication failed", authErr)
	}
	// Context overflow: the agent's compaction failed to free enough space,
	// indicated by an error message containing the standard phrase.
	if isContextOverflow(err) {
		return agent.NewContextOverflowError("context window exceeded even after compaction")
	}
	return err
}

// isPermissionBlocked detects a permission-gate block by checking for the
// permission denied marker the gate writes into the tool result error.
func isPermissionBlocked(err error) bool {
	return err != nil && strings.Contains(err.Error(), "permission denied")
}

// isContextOverflow detects context overflow by checking for known phrases in
// the error message.
func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "context window") ||
		strings.Contains(msg, "context length") ||
		strings.Contains(msg, "maximum context length")
}

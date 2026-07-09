package agent

import (
	"fmt"
	"unicode/utf8"
)

// ToolOutputBudget defines dynamic truncation thresholds based on the agent's
// current context usage. When context is tight, tool output is truncated more
// aggressively to avoid blowing the window; when context is loose, more output
// is preserved for richer model context.
//
// The budget is computed from the agent's contextWindow and last known prompt
// token count. It returns a byte limit that truncateToolOutputWithBudget uses
// to head+tail the output.
type ToolOutputBudget struct {
	// MaxBytes is the absolute ceiling (even when context is empty).
	MaxBytes int
	// MinBytes is the floor (even when context is tight).
	MinBytes int
	// ContextWindow is the model's total context window (0 = unknown).
	ContextWindow int
	// PromptTokens is the last known prompt token count (0 = unknown).
	PromptTokens int
}

// DefaultToolOutputBudget returns a budget with sensible defaults.
func DefaultToolOutputBudget(contextWindow, promptTokens int) ToolOutputBudget {
	return ToolOutputBudget{
		MaxBytes:      32 * 1024, // 32KB absolute ceiling
		MinBytes:      4 * 1024,  // 4KB floor even when tight
		ContextWindow: contextWindow,
		PromptTokens:  promptTokens,
	}
}

// Limit returns the effective byte limit for tool output, adjusted by context
// pressure. When context usage is low (<50%), the full MaxBytes is used.
// As usage rises, the limit shrinks proportionally down to MinBytes.
func (b ToolOutputBudget) Limit() int {
	if b.ContextWindow <= 0 || b.PromptTokens <= 0 {
		return b.MaxBytes
	}
	usage := float64(b.PromptTokens) / float64(b.ContextWindow)
	if usage < 0.5 {
		return b.MaxBytes
	}
	// Linear shrink from MaxBytes (at 50%) to MinBytes (at 95%+).
	// Beyond 95%, use MinBytes to preserve at least a usable snippet.
	if usage >= 0.95 {
		return b.MinBytes
	}
	// Interpolate: at 50% → MaxBytes, at 95% → MinBytes.
	fraction := (usage - 0.5) / 0.45 // 0.0 to 1.0
	limit := float64(b.MaxBytes) - fraction*(float64(b.MaxBytes)-float64(b.MinBytes))
	return int(limit)
}

// UsagePercent returns the context usage as a percentage (0-100), or -1 if unknown.
func (b ToolOutputBudget) UsagePercent() int {
	if b.ContextWindow <= 0 || b.PromptTokens <= 0 {
		return -1
	}
	return int(float64(b.PromptTokens) / float64(b.ContextWindow) * 100)
}

// truncateToolOutputWithBudget is like truncateToolOutput but uses a dynamic
// budget instead of the fixed maxToolOutputBytes constant. Returns the
// possibly-truncated body, a user-facing truncation notice (empty if no
// truncation), and the effective byte limit used.
func truncateToolOutputWithBudget(s string, budget ToolOutputBudget) (string, string, int) {
	limit := budget.Limit()
	if len(s) <= limit {
		return s, "", limit
	}
	keep := limit / 2
	head := snapToRuneBoundary(s, 0, keep)
	tail := snapToRuneBoundary(s, len(s)-keep, len(s))
	omitted := len(s) - len(head) - len(tail)
	usagePct := budget.UsagePercent()

	var notice string
	if usagePct >= 0 && usagePct >= 70 {
		notice = fmt.Sprintf("tool output truncated to %d bytes (context at %d%%): %d of %d bytes elided — use narrower args (grep, offset/limit, specific paths) to avoid wasting context budget", limit, usagePct, omitted, len(s))
	} else {
		notice = fmt.Sprintf("tool output truncated: %d of %d bytes elided", omitted, len(s))
	}

	body := head + fmt.Sprintf("\n\n…[truncated %d of %d bytes — rerun with narrower args to see the middle]…\n\n", omitted, len(s)) + tail
	return body, notice, limit
}

// estimateTokensRough gives a rough token estimate for a string (1 token ≈ 4 bytes
// for ASCII, ≈ 2 bytes for CJK). This is only for budget display, not for
// compaction decisions.
func estimateTokensRough(s string) int {
	if s == "" {
		return 0
	}
	bytes := len(s)
	runes := utf8.RuneCountInString(s)
	byBytes := (bytes + 3) / 4
	if runes > byBytes {
		return runes
	}
	return byBytes
}

package agent

import (
	"strings"
	"testing"
)

func TestToolOutputBudgetLimitLowUsage(t *testing.T) {
	b := ToolOutputBudget{
		MaxBytes:      32768,
		MinBytes:      4096,
		ContextWindow: 100000,
		PromptTokens:  30000, // 30% usage
	}
	limit := b.Limit()
	if limit != b.MaxBytes {
		t.Errorf("Limit at 30%% usage = %d, want %d (full max)", limit, b.MaxBytes)
	}
}

func TestToolOutputBudgetLimitHighUsage(t *testing.T) {
	b := ToolOutputBudget{
		MaxBytes:      32768,
		MinBytes:      4096,
		ContextWindow: 100000,
		PromptTokens:  80000, // 80% usage
	}
	limit := b.Limit()
	if limit >= b.MaxBytes {
		t.Errorf("Limit at 80%% usage = %d, should be less than max %d", limit, b.MaxBytes)
	}
	if limit <= b.MinBytes {
		t.Errorf("Limit at 80%% usage = %d, should be more than min %d", limit, b.MinBytes)
	}
}

func TestToolOutputBudgetLimitCriticalUsage(t *testing.T) {
	b := ToolOutputBudget{
		MaxBytes:      32768,
		MinBytes:      4096,
		ContextWindow: 100000,
		PromptTokens:  96000, // 96% usage
	}
	limit := b.Limit()
	if limit != b.MinBytes {
		t.Errorf("Limit at 96%% usage = %d, want min %d", limit, b.MinBytes)
	}
}

func TestToolOutputBudgetLimitUnknown(t *testing.T) {
	b := ToolOutputBudget{
		MaxBytes: 32768,
		MinBytes: 4096,
		// ContextWindow and PromptTokens = 0 (unknown)
	}
	limit := b.Limit()
	if limit != b.MaxBytes {
		t.Errorf("Limit with unknown context = %d, want max %d", limit, b.MaxBytes)
	}
}

func TestToolOutputBudgetUsagePercent(t *testing.T) {
	b := ToolOutputBudget{ContextWindow: 100000, PromptTokens: 75000}
	if p := b.UsagePercent(); p != 75 {
		t.Errorf("UsagePercent = %d, want 75", p)
	}
	b = ToolOutputBudget{ContextWindow: 0, PromptTokens: 0}
	if p := b.UsagePercent(); p != -1 {
		t.Errorf("UsagePercent with unknown = %d, want -1", p)
	}
}

func TestTruncateToolOutputWithBudgetNoTruncation(t *testing.T) {
	s := "short output"
	b := ToolOutputBudget{MaxBytes: 1024, MinBytes: 512}
	body, notice, limit := truncateToolOutputWithBudget(s, b)
	if body != s {
		t.Errorf("body should be unchanged for short output")
	}
	if notice != "" {
		t.Errorf("notice should be empty for short output, got %q", notice)
	}
	if limit != 1024 {
		t.Errorf("limit = %d, want 1024", limit)
	}
}

func TestTruncateToolOutputWithBudgetTruncation(t *testing.T) {
	s := strings.Repeat("x", 10000)
	b := ToolOutputBudget{MaxBytes: 1000, MinBytes: 500, ContextWindow: 100000, PromptTokens: 30000}
	body, notice, limit := truncateToolOutputWithBudget(s, b)
	if body == s {
		t.Error("body should be truncated for long output")
	}
	if notice == "" {
		t.Error("notice should be non-empty for truncated output")
	}
	if limit != 1000 {
		t.Errorf("limit = %d, want 1000", limit)
	}
	if !strings.Contains(body, "truncated") {
		t.Error("body should contain truncation marker")
	}
}

func TestTruncateToolOutputWithBudgetHighUsageNotice(t *testing.T) {
	s := strings.Repeat("x", 10000)
	b := ToolOutputBudget{MaxBytes: 1000, MinBytes: 500, ContextWindow: 100000, PromptTokens: 80000}
	_, notice, _ := truncateToolOutputWithBudget(s, b)
	if !strings.Contains(notice, "context at") {
		t.Errorf("notice should mention context usage at high pressure: %q", notice)
	}
	if !strings.Contains(notice, "narrower args") {
		t.Errorf("notice should suggest narrower args: %q", notice)
	}
}

func TestEstimateTokensRough(t *testing.T) {
	if n := estimateTokensRough(""); n != 0 {
		t.Errorf("estimateTokensRough('') = %d, want 0", n)
	}
	// "hello" is 5 bytes, 5 runes → byBytes=2, runes=5 → max=5
	if n := estimateTokensRough("hello"); n != 5 {
		t.Errorf("estimateTokensRough('hello') = %d, want 5", n)
	}
	// CJK string: 4 runes, 12 bytes → byBytes=3, runes=4 → max=4
	if n := estimateTokensRough("你好世界"); n != 4 {
		t.Errorf("estimateTokensRough('你好世界') = %d, want 4", n)
	}
}

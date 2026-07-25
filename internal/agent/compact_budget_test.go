package agent

import (
	"strings"
	"testing"

	"rexion/internal/provider"
)

// TestExtractPriorSummary verifies extraction of prior compaction summary text.
func TestExtractPriorSummary(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "some user message"},
		{Role: provider.RoleUser, Content: "<compaction-summary>\n## Goal\nBuild a feature\n## Decisions\nUse Go\n</compaction-summary>"},
		{Role: provider.RoleAssistant, Content: "working on it"},
	}

	got := extractPriorSummary(msgs)
	if !strings.Contains(got, "Build a feature") {
		t.Errorf("extractPriorSummary() = %q, want to contain 'Build a feature'", got)
	}
	if !strings.Contains(got, "Use Go") {
		t.Errorf("extractPriorSummary() = %q, want to contain 'Use Go'", got)
	}
}

func TestExtractPriorSummaryNone(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "some user message"},
		{Role: provider.RoleAssistant, Content: "working on it"},
	}
	got := extractPriorSummary(msgs)
	if got != "" {
		t.Errorf("extractPriorSummary() = %q, want empty", got)
	}
}

func TestExtractPriorSummaryNoCloseTag(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "<compaction-summary>\n## Goal\nBuild a feature"},
	}
	got := extractPriorSummary(msgs)
	if !strings.Contains(got, "Build a feature") {
		t.Errorf("extractPriorSummary() = %q, want to contain 'Build a feature'", got)
	}
}

func TestHasPriorSummary(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "normal message"},
	}
	if hasPriorSummary(msgs) {
		t.Error("hasPriorSummary should be false for messages without summary")
	}

	msgs = append(msgs, provider.Message{
		Role:    provider.RoleUser,
		Content: "<compaction-summary>\nsome summary\n</compaction-summary>",
	})
	if !hasPriorSummary(msgs) {
		t.Error("hasPriorSummary should be true for messages with summary")
	}
}

func TestSummaryBudgetTokens(t *testing.T) {
	tests := []struct {
		name    string
		window  int
		wantMin int
		wantMax int
	}{
		{"no window", 0, 1024, defaultSummaryBudget},
		{"tiny window 8k", 8192, 1024, 2048},
		{"small window 16k", 16384, 1024, 4096},
		{"large window 128k", 131072, defaultSummaryBudget, defaultSummaryBudget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Agent{contextWindow: tt.window}
			got := a.summaryBudgetTokens()
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("summaryBudgetTokens() = %d, want between %d and %d", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCompressedSummaryBudgetTokens(t *testing.T) {
	a := &Agent{contextWindow: 131072}
	normal := a.summaryBudgetTokens()
	compressed := a.compressedSummaryBudgetTokens()
	if compressed >= normal {
		t.Errorf("compressed budget %d should be less than normal %d", compressed, normal)
	}
	if compressed < 512 {
		t.Errorf("compressed budget %d should be at least 512", compressed)
	}
}

func TestSummarySystemPromptContainsFormat(t *testing.T) {
	// The summarySystemPrompt now contains %d for the budget, verify it formats.
	s := strings.Contains(summarySystemPrompt, "%d")
	if !s {
		t.Errorf("summarySystemPrompt should contain %%d placeholder for budget")
	}
}

func TestSummaryCompressedSystemPromptContainsFormat(t *testing.T) {
	s := strings.Contains(summaryCompressedSystemPrompt, "%d")
	if !s {
		t.Errorf("summaryCompressedSystemPrompt should contain %%d placeholder for budget")
	}
}

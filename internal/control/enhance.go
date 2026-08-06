package control

import (
	"context"
	"fmt"
	"strings"

	"rexion/internal/nilutil"
	"rexion/internal/provider"
)

// enhancePromptSystemPrompt steers the model toward rewriting a user's draft
// prompt into a clearer, more complete request — extending missing context,
// sharpening intent, and keeping the user's voice. The output is dropped back
// into the composer, so it must be a drop-in replacement of the draft, not a
// meta-explanation.
const enhancePromptSystemPrompt = `You improve a user's message draft before they send it to a coding agent.
Rewrite the draft so it is:
- clearer about what the user wants done (goal, expected result)
- more specific where the original is vague (target files, scope, constraints)
- extended with useful context the user likely meant but omitted
- free of filler, and kept in the same language as the original
Return ONLY the rewritten prompt text — no quotes, no "Here is", no commentary.`

// enhanceProvider is a single-turn text-enhancement backend. Controller keeps
// it as an interface so tests can stub it and frontends without a configured
// model degrade to a no-op.
type enhanceProvider interface {
	EnhancePrompt(ctx context.Context, draft string) (string, error)
}

// ProviderPromptEnhancer rewrites a user's draft via a one-shot provider
// completion (no agent loop, no tool calls) — the same lightweight pattern the
// auto-plan classifier uses.
type ProviderPromptEnhancer struct {
	prov provider.Provider
}

// NewProviderPromptEnhancer wraps a provider for prompt enhancement. A nil or
// typed-nil provider yields a nil enhancer, which callers treat as disabled.
func NewProviderPromptEnhancer(prov provider.Provider) *ProviderPromptEnhancer {
	if nilutil.IsNil(prov) {
		return nil
	}
	return &ProviderPromptEnhancer{prov: prov}
}

// EnhancePrompt streams a single completion and returns the rewritten text.
func (e *ProviderPromptEnhancer) EnhancePrompt(ctx context.Context, draft string) (string, error) {
	if e == nil || nilutil.IsNil(e.prov) {
		return "", fmt.Errorf("prompt enhancer is not initialized")
	}
	ch, err := e.prov.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: enhancePromptSystemPrompt},
			{Role: provider.RoleUser, Content: draft},
		},
		Temperature: 0.3,
		MaxTokens:   1024,
	})
	if err != nil {
		return "", err
	}

	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			return "", chunk.Err
		}
	}
	out := strings.TrimSpace(text.String())
	if out == "" {
		return "", fmt.Errorf("prompt enhancer returned an empty result")
	}
	return out, nil
}

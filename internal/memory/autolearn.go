// Package memory provides personal knowledge management utilities including
// automatic learning of user preferences from conversation content.
package memory

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// AutoLearnProposal represents a detected preference that can be saved to PKM.
type AutoLearnProposal struct {
	Type        string // "preference" | "contact" | "project" | "identity" | "tool" | "writing" | "global"
	TargetFile  string // "preferences.md" | "people.md" | "projects.md" | "writing_style.md" | "" for global
	Content     string // The detected statement to append
	Category    string // Subcategory for organization
}

// AutoLearner scans user messages for preference declarations.
type AutoLearner struct {
	mu             sync.Mutex
	suppressedTypes map[string]bool // Types suppressed for this conversation
	proposalCount  int              // Number of proposals this conversation (max 1 per turn)
}

// NewAutoLearner creates a fresh learner instance for a conversation.
func NewAutoLearner() *AutoLearner {
	return &AutoLearner{
		suppressedTypes: make(map[string]bool),
		proposalCount:   0,
	}
}

// Reset clears suppression state for a new conversation.
func (a *AutoLearner) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.suppressedTypes = make(map[string]bool)
	a.proposalCount = 0
}

// SuppressType marks a type as rejected by user for this conversation.
func (a *AutoLearner) SuppressType(typ string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.suppressedTypes[typ] = true
}

// IsSuppressed checks if a type was rejected this conversation.
func (a *AutoLearner) IsSuppressed(typ string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.suppressedTypes[typ]
}

// Scan analyzes user message text for preference declarations.
// It returns a proposal if a detectable pattern is found, respecting suppression rules.
func (a *AutoLearner) Scan(userMessage string) *AutoLearnProposal {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Only one proposal per conversation turn.
	if a.proposalCount >= 1 {
		return nil
	}

	text := strings.TrimSpace(userMessage)
	if text == "" {
		return nil
	}

	// Try each pattern type in priority order.
	if p := a.detectWritingStyle(text); p != nil {
		if !a.suppressedTypes[p.Type] {
			a.proposalCount++
			return p
		}
	}
	if p := a.detectPreference(text); p != nil {
		if !a.suppressedTypes[p.Type] {
			a.proposalCount++
			return p
		}
	}
	if p := a.detectContact(text); p != nil {
		if !a.suppressedTypes[p.Type] {
			a.proposalCount++
			return p
		}
	}
	if p := a.detectProject(text); p != nil {
		if !a.suppressedTypes[p.Type] {
			a.proposalCount++
			return p
		}
	}
	if p := a.detectIdentity(text); p != nil {
		if !a.suppressedTypes[p.Type] {
			a.proposalCount++
			return p
		}
	}
	if p := a.detectTool(text); p != nil {
		if !a.suppressedTypes[p.Type] {
			a.proposalCount++
			return p
		}
	}

	return nil
}

// ── Detection patterns ──────────────────────────────────────────────────────

var (
	// Writing style patterns.
	writingPatterns = []*regexp.Regexp{
		regexp.MustCompile(`我喜欢([^。\n]{2,30})的风格`),
		regexp.MustCompile(`我的写作风格是([^。\n]{2,30})`),
		regexp.MustCompile(`我偏好([^。\n]{2,30})的表述`),
		regexp.MustCompile(`写作时我喜欢([^。\n]{2,30})`),
	}

	// General preference patterns.
	preferencePatterns = []*regexp.Regexp{
		regexp.MustCompile(`我喜欢([^。\n]{2,30})`),
		regexp.MustCompile(`我偏好([^。\n]{2,30})`),
		regexp.MustCompile(`我不喜欢([^。\n]{2,30})`),
		regexp.MustCompile(`我习惯([^。\n]{2,30})`),
	}

	// Contact patterns.
	contactPatterns = []*regexp.Regexp{
		regexp.MustCompile(`([^\s]+)是我([^。\n]{2,20})`),
		regexp.MustCompile(`([^\s]+)负责([^。\n]{2,20})`),
		regexp.MustCompile(`([^\s]+)是([^。\n]{2,10})的([^。\n]{2,10})`),
	}

	// Project patterns.
	projectPatterns = []*regexp.Regexp{
		regexp.MustCompile(`我在做([^。\n]{2,30})项目`),
		regexp.MustCompile(`我负责([^。\n]{2,30})`),
		regexp.MustCompile(`我在开发([^。\n]{2,30})`),
		regexp.MustCompile(`我正在([^。\n]{2,30})`),
	}

	// Identity patterns.
	identityPatterns = []*regexp.Regexp{
		regexp.MustCompile(`我是([^。\n]{2,15})`),
		regexp.MustCompile(`我的职位是([^。\n]{2,15})`),
		regexp.MustCompile(`我从事([^。\n]{2,15})工作`),
	}

	// Tool preference patterns.
	toolPatterns = []*regexp.Regexp{
		regexp.MustCompile(`我喜欢用([^\s]+)`),
		regexp.MustCompile(`我用([^\s]+)写代码`),
		regexp.MustCompile(`我用([^\s]+)编辑`),
		regexp.MustCompile(`我的开发环境是([^。\n]{2,20})`),
	}

	// Cross-project (global) patterns. These describe conventions that span
	// every project the user works on, so they belong in the global memory
	// store rather than a per-project PKM file.
	globalPatterns = []*regexp.Regexp{
		regexp.MustCompile(`我的所有项目都用([^。\n]{2,30})`),
		regexp.MustCompile(`我所有的项目都([^。\n]{2,30})`),
		regexp.MustCompile(`我通常使用([^。\n]{2,30})`),
		regexp.MustCompile(`我一般用([^。\n]{2,30})`),
		regexp.MustCompile(`跨项目我都([^。\n]{2,30})`),
		regexp.MustCompile(`我所有的代码都([^。\n]{2,30})`),
	}
)

func (a *AutoLearner) detectWritingStyle(text string) *AutoLearnProposal {
	for _, p := range writingPatterns {
		if m := p.FindStringSubmatch(text); len(m) > 1 {
			return &AutoLearnProposal{
				Type:       "writing",
				TargetFile: "writing_style.md",
				Content:    m[0],
				Category:   "style",
			}
		}
	}
	return nil
}

func (a *AutoLearner) detectPreference(text string) *AutoLearnProposal {
	for _, p := range preferencePatterns {
		if m := p.FindStringSubmatch(text); len(m) > 1 {
			return &AutoLearnProposal{
				Type:       "preference",
				TargetFile: "preferences.md",
				Content:    m[0],
				Category:   "general",
			}
		}
	}
	return nil
}

func (a *AutoLearner) detectContact(text string) *AutoLearnProposal {
	for _, p := range contactPatterns {
		if m := p.FindStringSubmatch(text); len(m) > 1 {
			return &AutoLearnProposal{
				Type:       "contact",
				TargetFile: "people.md",
				Content:    m[0],
				Category:   "contact",
			}
		}
	}
	return nil
}

func (a *AutoLearner) detectProject(text string) *AutoLearnProposal {
	for _, p := range projectPatterns {
		if m := p.FindStringSubmatch(text); len(m) > 1 {
			return &AutoLearnProposal{
				Type:       "project",
				TargetFile: "projects.md",
				Content:    m[0],
				Category:   "active",
			}
		}
	}
	return nil
}

func (a *AutoLearner) detectIdentity(text string) *AutoLearnProposal {
	for _, p := range identityPatterns {
		if m := p.FindStringSubmatch(text); len(m) > 1 {
			return &AutoLearnProposal{
				Type:       "identity",
				TargetFile: "preferences.md",
				Content:    m[0],
				Category:   "identity",
			}
		}
	}
	return nil
}

func (a *AutoLearner) detectTool(text string) *AutoLearnProposal {
	for _, p := range toolPatterns {
		if m := p.FindStringSubmatch(text); len(m) > 1 {
			return &AutoLearnProposal{
				Type:       "tool",
				TargetFile: "preferences.md",
				Content:    m[0],
				Category:   "tools",
			}
		}
	}
	return nil
}

// detectGlobalPattern scans for cross-project conventions — statements that
// describe how the user works across every project, not just the current one
// (e.g. "我的所有项目都用 pnpm", "我通常使用 GitFlow"). A matching proposal
// carries Type="global" and an empty TargetFile, signalling to the controller
// that it should route through LearnGlobal (writing to GlobalStore) rather
// than AppendPKMFile (which targets a single PKM file).
func (a *AutoLearner) detectGlobalPattern(text string) *AutoLearnProposal {
	for _, p := range globalPatterns {
		if m := p.FindStringSubmatch(text); len(m) > 1 {
			return &AutoLearnProposal{
				Type:       "global",
				TargetFile: "", // global proposals skip PKM and go to GlobalStore
				Content:    strings.TrimSpace(m[0]),
				Category:   "cross-project",
			}
		}
	}
	return nil
}

// LearnGlobal writes a cross-project proposal into the GlobalStore rather than
// a PKM file. The proposal's Content becomes both the body and the index
// description; the Name is derived (slugified) from the Content so re-sending
// the same convention updates the same fact instead of creating duplicates.
// A nil proposal is a no-op (returns nil) so callers can pipe detectGlobalPattern
// straight through without a nil check.
func (a *AutoLearner) LearnGlobal(proposal *AutoLearnProposal, globalStore GlobalStore) error {
	if proposal == nil {
		return nil
	}
	if globalStore.Dir == "" {
		return fmt.Errorf("global memory store unavailable (no user config dir)")
	}
	content := strings.TrimSpace(proposal.Content)
	if content == "" {
		return fmt.Errorf("global proposal has empty content")
	}
	name := slug(content)
	if name == "" {
		return fmt.Errorf("global proposal yielded empty slug")
	}
	_, err := globalStore.Save(Memory{
		Name:        name,
		Title:       content,
		Description: content,
		Type:        TypeGlobal,
		Body:        content,
	})
	return err
}
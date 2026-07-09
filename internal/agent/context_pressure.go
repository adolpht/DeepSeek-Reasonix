package agent

import (
	"fmt"
	"strings"
)

// ContextPressureLevel categorizes the agent's context window utilization
// into actionable bands that influence tool selection guidance.
type ContextPressureLevel int

const (
	// ContextLoose means <50% usage — the agent can freely use exploratory
	// tools (read_file, ls, grep with broad patterns).
	ContextLoose ContextPressureLevel = iota
	// ContextModerate means 50–75% usage — the agent should prefer targeted
	// queries over broad exploration.
	ContextModerate
	// ContextTight means 75–90% usage — the agent must use precise tools
	// (grep with specific patterns, read_file with offset/limit) and avoid
	// large outputs.
	ContextTight
	// ContextCritical means >90% usage — the agent should minimize tool
	// output, use head/tail, and consider compacting or wrapping up.
	ContextCritical
)

// String returns a human-readable name for the pressure level.
func (l ContextPressureLevel) String() string {
	switch l {
	case ContextLoose:
		return "loose"
	case ContextModerate:
		return "moderate"
	case ContextTight:
		return "tight"
	case ContextCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// ContextPressure evaluates the current context window usage and returns a
// pressure level plus actionable tool-selection guidance.
type ContextPressure struct {
	Level          ContextPressureLevel
	UsagePercent   int // 0-100, or -1 if unknown
	Window         int
	PromptTokens   int
}

// AssessContext computes the current context pressure from the agent's
// context window and last known prompt token count.
func AssessContext(contextWindow, promptTokens int) ContextPressure {
	cp := ContextPressure{Window: contextWindow, PromptTokens: promptTokens}
	if contextWindow <= 0 || promptTokens <= 0 {
		cp.UsagePercent = -1
		cp.Level = ContextLoose
		return cp
	}
	cp.UsagePercent = int(float64(promptTokens) / float64(contextWindow) * 100)
	switch {
	case cp.UsagePercent >= 90:
		cp.Level = ContextCritical
	case cp.UsagePercent >= 75:
		cp.Level = ContextTight
	case cp.UsagePercent >= 50:
		cp.Level = ContextModerate
	default:
		cp.Level = ContextLoose
	}
	return cp
}

// ToolGuidance returns a one-line directive the agent can use to select
// tools appropriate for the current context pressure. Returns empty string
// when context is loose (no guidance needed).
func (cp ContextPressure) ToolGuidance() string {
	switch cp.Level {
	case ContextCritical:
		return "context is critical (" + fmt.Sprintf("%d%%", cp.UsagePercent) + "): use grep with specific patterns, read_file with offset/limit, avoid large outputs; consider compacting or wrapping up"
	case ContextTight:
		return "context is tight (" + fmt.Sprintf("%d%%", cp.UsagePercent) + "): prefer targeted queries (grep, read_file with offset/limit) over broad reads (ls, read_file without limit)"
	case ContextModerate:
		return "context is moderate (" + fmt.Sprintf("%d%%", cp.UsagePercent) + "): prefer specific search patterns over reading entire files"
	default:
		return ""
	}
}

// ShouldPreferGrep returns true when the agent should prefer grep over
// read_file for file exploration (moderate pressure or higher).
func (cp ContextPressure) ShouldPreferGrep() bool {
	return cp.Level >= ContextModerate
}

// ShouldUseOffsetLimit returns true when the agent should use offset/limit
// with read_file (tight pressure or higher).
func (cp ContextPressure) ShouldUseOffsetLimit() bool {
	return cp.Level >= ContextTight
}

// ShouldCompact returns true when compaction should be considered
// (critical pressure).
func (cp ContextPressure) ShouldCompact() bool {
	return cp.Level >= ContextCritical
}

// PreferredTools ranks tools by suitability for the current context pressure.
// When context is tight, targeted tools rank higher than broad ones.
func (cp ContextPressure) PreferredTools() string {
	switch cp.Level {
	case ContextCritical, ContextTight:
		return strings.Join([]string{
			"grep (with specific pattern)",
			"read_file (with offset+limit)",
			"ls (specific path)",
			"glob (narrow pattern)",
		}, " > ")
	case ContextModerate:
		return strings.Join([]string{
			"grep (with pattern)",
			"read_file (with limit)",
			"ls",
			"glob",
		}, " > ")
	default:
		return "all tools available (read_file, ls, grep, glob)"
	}
}

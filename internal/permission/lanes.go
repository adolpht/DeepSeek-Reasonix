package permission

import (
	"encoding/json"
	"strings"
)

// Lane is a path-scoped policy overlay. It applies a separate set of
// allow/ask/deny rules to tool calls whose subject (file path, command)
// falls under the Lane's Path pattern. Lanes let a project enforce
// different permission postures for different directories or command
// scopes — e.g. "src/" allows free edits, "prod/" requires approval,
// "deploy/*" is denied outright.
//
// Lane matching is purely lexical: Path is a glob matched against the
// subject's path prefix. A Lane with an empty Path matches all subjects
// (the global fallback lane). Lanes are evaluated in order; the first
// matching lane wins. If no lane matches, the base Policy rules apply.
type Lane struct {
	// Name is a human-readable label for diagnostics (e.g. "source", "prod").
	Name string `json:"name,omitempty"`

	// Path is a glob pattern matched against the call's subject. When empty,
	// the lane matches all subjects. Path matching is prefix-style: a lane
	// with Path "src/" matches subjects like "src/main.go" or "src/utils/helper.go".
	// For bash commands, the path portion (if any) is extracted and matched.
	Path string `json:"path,omitempty"`

	// Mode overrides the base Policy's fallback mode for calls in this lane.
	// Zero value (Allow) means "no override" — use the base Policy's mode.
	// Use a pointer or set OverrideMode=true to distinguish.
	OverrideMode bool     `json:"override_mode,omitempty"`
	Mode         Decision `json:"mode,omitempty"`

	// Allow/Ask/Deny are additional rules that apply only within this lane.
	// They are checked before the base Policy's rules.
	Allow []Rule `json:"allow,omitempty"`
	Ask   []Rule `json:"ask,omitempty"`
	Deny  []Rule `json:"deny,omitempty"`
}

// MultiPolicy extends Policy with path-scoped lanes. The base Policy rules
// always apply as the fallback; lane rules take precedence when a lane's
// Path pattern matches the call's subject.
type MultiPolicy struct {
	Policy
	Lanes []Lane
}

// NewMultiPolicy builds a MultiPolicy from a base Policy and a set of lanes.
func NewMultiPolicy(base Policy, lanes []Lane) MultiPolicy {
	return MultiPolicy{Policy: base, Lanes: lanes}
}

// Decide evaluates a tool call against the multi-lane policy. Lane rules
// take precedence over base rules: if a matching lane has a rule for the
// tool, the lane's decision is final. If the lane has no matching rule,
// the base Policy's Decide is used (with the lane's mode override if set).
func (mp MultiPolicy) Decide(toolName string, readOnly bool, args json.RawMessage) Decision {
	subject := Subject(args)

	// Find the first matching lane.
	for _, lane := range mp.Lanes {
		if !laneMatches(lane, subject) {
			continue
		}
		// Check lane rules in precedence order: deny > ask > allow.
		switch {
		case matchAny(lane.Deny, toolName, subject):
			return Deny
		case matchAny(lane.Ask, toolName, subject):
			return Ask
		case matchAny(lane.Allow, toolName, subject):
			return Allow
		}
		// No lane-specific rule matched. If the lane overrides the mode,
		// use the lane's mode for writers; readers always allow.
		if lane.OverrideMode {
			if readOnly {
				return Allow
			}
			return lane.Mode
		}
		// Otherwise fall through to base policy.
		break
	}
	return mp.Policy.Decide(toolName, readOnly, args)
}

// laneMatches checks whether a lane's path pattern matches the given subject.
// For file paths, the subject is matched directly. For bash commands, the
// path portion (if extractable) is matched. An empty Path matches all.
func laneMatches(lane Lane, subject string) bool {
	if lane.Path == "" {
		return true // global lane
	}
	// Try direct glob match first (for file path subjects).
	if matchGlob(lane.Path, subject) {
		return true
	}
	// Prefix match: lane.Path "src/" should match "src/main.go".
	if strings.HasPrefix(subject, lane.Path) {
		return true
	}
	// For bash commands, try to extract a path and match it.
	pathFromCmd := extractPathFromCommand(subject)
	if pathFromCmd != "" {
		if matchGlob(lane.Path, pathFromCmd) || strings.HasPrefix(pathFromCmd, lane.Path) {
			return true
		}
	}
	return false
}

// extractPathFromCommand tries to find a file path in a bash command string.
// It looks for arguments that look like paths (start with /, ./, ../, or
// contain a directory separator with a file extension). Returns "" if none found.
func extractPathFromCommand(cmd string) string {
	fields := strings.Fields(cmd)
	for _, f := range fields {
		// Skip flags and command names.
		if strings.HasPrefix(f, "-") {
			continue
		}
		// Check for path-like patterns.
		if strings.HasPrefix(f, "/") || strings.HasPrefix(f, "./") || strings.HasPrefix(f, "../") {
			return f
		}
		// Check for paths with separators.
		if strings.Contains(f, "/") && strings.Contains(f, ".") {
			return f
		}
	}
	return ""
}

// ParseLaneConfig parses a lane from config strings. Each lane is defined by
// its name, path pattern, and optional mode. Rules are added via the standard
// rule format.
type LaneConfig struct {
	Name   string   `json:"name" toml:"name"`
	Path   string   `json:"path" toml:"path"`
	Mode   string   `json:"mode" toml:"mode"`
	Allow  []string `json:"allow" toml:"allow"`
	Ask    []string `json:"ask" toml:"ask"`
	Deny   []string `json:"deny" toml:"deny"`
}

// BuildLanes converts LaneConfig entries to Lane structs.
func BuildLanes(configs []LaneConfig) []Lane {
	if len(configs) == 0 {
		return nil
	}
	lanes := make([]Lane, 0, len(configs))
	for _, c := range configs {
		lane := Lane{
			Name:         c.Name,
			Path:         c.Path,
			OverrideMode: c.Mode != "",
			Mode:         ParseDecision(c.Mode),
			Allow:        parseRules(c.Allow),
			Ask:          parseRules(c.Ask),
			Deny:         parseRules(c.Deny),
		}
		lanes = append(lanes, lane)
	}
	return lanes
}

package skill

import (
	"rexion/internal/tool"
)

// ProbeAvailability checks each skill's AllowedTools against the live tool
// registry and marks skills whose required tools are missing as Unavailable.
// Skills with no AllowedTools are always considered available.
//
// The returned slice is a fresh copy; the input is not mutated.
//
// MCP tools: if a tool name has the "mcp__" prefix but is absent from the
// registry, it means the corresponding plugin is not configured. The skill
// is marked unavailable. If a lazy/background placeholder exists for the
// MCP server, the placeholder counts as present (it will resolve on first
// call), so the skill stays available.
func ProbeAvailability(skills []Skill, reg *tool.Registry) []Skill {
	out := make([]Skill, len(skills))
	for i, sk := range skills {
		out[i] = sk
		if len(sk.AllowedTools) == 0 {
			continue // no tool deps → always available
		}
		var missing []string
		for _, name := range sk.AllowedTools {
			if _, ok := reg.Get(name); ok {
				continue // tool exists (real or placeholder)
			}
			missing = append(missing, name)
		}
		if len(missing) > 0 {
			out[i].Unavailable = true
			out[i].MissingDeps = missing
		}
	}
	return out
}

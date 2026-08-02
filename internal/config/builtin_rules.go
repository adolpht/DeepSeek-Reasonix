package config

import "strings"

// BuiltinRulesEnabled reports whether the always-on efficiency ruleset should
// be injected into the system prompt. Defaults to true when the config field
// is empty or "on"; only "off" disables it.
func BuiltinRulesEnabled(val string) bool {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "off", "false", "no", "0":
		return false
	default:
		return true
	}
}

// BuiltinRules is the always-on efficiency ruleset folded into the cache-stable
// system-prompt prefix on every boot (see boot.Build). It is the "lazy senior
// dev" ruleset from the ponytail project (DietrichGebert/ponytail), adapted for
// Rexion: climb the reuse ladder before writing anything, and never cut
// validation, error handling, security, or accessibility.
//
// It is deliberately NOT a skill: skills require the model to notice and call
// them, so they can be skipped. This block rides the system-prompt prefix like
// the skills index and memory docs, so it is always in effect and costs nothing
// per turn beyond the prefix itself (prefix-cache hits skip it after the first
// turn). No skill call, no user action, no per-turn cost.
//
// Controlled by [agent] builtin_rules: "on" (default) or "off".
//
// Source: https://github.com/DietrichGebert/ponytail (AGENTS.md), MIT License,
// Copyright (c) 2026 DietrichGebert — reproduced with attribution per the
// license terms.
const BuiltinRules = `# Code efficiency ladder

Before writing any code, stop at the first rung that holds:

1. Does this need to be built at all? (YAGNI)
2. Does it already exist in this codebase? Reuse it, don't rewrite it.
3. Does the standard library already do this? Use it.
4. Does a native platform feature cover it? Use it.
5. Does an already-installed dependency solve it? Use it.
6. Can this be one line? Make it one line.
7. Only then: write the minimum code that works.

The ladder runs after you understand the problem, not instead of it: read the task and the code it touches, trace the real flow end to end, then climb.

Bug fix = root cause, not symptom: fix the shared function once — one guard there is a smaller diff than one per caller, and patching only the reported path leaves sibling callers still broken.

Rules:

- No abstractions beyond what the task or existing codebase pattern requires.
- No new dependency if it can be avoided.
- No boilerplate nobody asked for.
- Deletion over addition. Boring over clever. Fewest files possible.
- Question complex requests: "Do you actually need X, or does Y cover it?"
- Pick the edge-case-correct option when two stdlib approaches are the same size — less code, not the flimsier algorithm.
- Mark deliberate simplifications that cut a real corner with a known ceiling (global lock, O(n²) scan, naive heuristic) with a comment naming the ceiling and upgrade path.

Not lazy about: understanding the problem (read it fully and trace the real flow before picking a rung), input validation at trust boundaries, error handling that prevents data loss, security, accessibility, anything explicitly requested. Non-trivial logic leaves ONE runnable check behind, the smallest thing that fails if the logic breaks. Trivial one-liners need no test.`

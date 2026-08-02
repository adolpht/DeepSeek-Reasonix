package config

import (
	"strings"
	"testing"
)

// TestBuiltinRulesShape guards the always-on ruleset: it must exist, carry the
// efficiency ladder and safety carve-outs, and stay small enough to ride the
// cache-stable prefix without bloat.
func TestBuiltinRulesShape(t *testing.T) {
	if BuiltinRules == "" {
		t.Fatal("BuiltinRules is empty — boot injects it unconditionally, so it must be non-empty")
	}

	// Signature phrases that the ruleset's effect depends on.
	for _, want := range []string{
		"YAGNI",
		"standard library",
		"one line",
		"minimum code that works",
		"root cause",
		"Not lazy about",
		"trust boundaries",
		"security",
		"accessibility",
	} {
		if !strings.Contains(BuiltinRules, want) {
			t.Errorf("BuiltinRules missing %q — ruleset semantics would be lost", want)
		}
	}

	// Verify overlap with base prompt was removed: these phrases from
	// DefaultSystemPrompt should NOT be redundantly restated.
	for _, redundant := range []string{
		"keep changes minimal",
		"understand the request before acting",
		"shortest working diff wins",
	} {
		if strings.Contains(BuiltinRules, redundant) {
			t.Errorf("BuiltinRules contains %q — already covered by DefaultSystemPrompt, remove the overlap", redundant)
		}
	}

	// Cap the block like the skills index (IndexMaxChars=4000): it is part of
	// the per-session prefix, so it must stay bounded.
	if n := len([]rune(BuiltinRules)); n > 3000 {
		t.Errorf("BuiltinRules is %d runes — exceeds the 3000-rune target; trim it", n)
	}
}

// TestBuiltinRulesEnabled verifies the config switch logic.
func TestBuiltinRulesEnabled(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{"", true},       // default: on
		{"on", true},
		{"ON", true},
		{"off", false},
		{"OFF", false},
		{"false", false},
		{"no", false},
		{"0", false},
		{"yes", true},    // unrecognized → default on
	} {
		got := BuiltinRulesEnabled(tc.input)
		if got != tc.want {
			t.Errorf("BuiltinRulesEnabled(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

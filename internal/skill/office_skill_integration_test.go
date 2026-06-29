package skill

import (
	"testing"
)

// TestOfficeSkillsBuiltin verifies the office-related skills are registered as
// built-ins with the correct run mode, allowed tools, and body content.
// This guards against regressions where an office skill loses its subagent
// isolation or MCP tool scoping.
func TestOfficeSkillsBuiltin(t *testing.T) {
	st := New(Options{DisableBuiltins: false, Stderr: nil})

	cases := []struct {
		name          string
		wantRunAs     RunAs
		mustHaveTool  string
		mustMentionIn string // substring expected in Body; empty skips the check
	}{
		{"weekly-report", RunSubagent, "mcp__office__write_docx", "git log"},
		{"meeting-minutes", RunSubagent, "mcp__office__write_docx", "议题"},
		{"contract-draft", RunSubagent, "mcp__office__render_template", "条款库"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sk, ok := st.Read(c.name)
			if !ok {
				t.Fatalf("built-in skill %q not loaded", c.name)
			}
			if sk.Scope != ScopeBuiltin {
				t.Errorf("Scope = %v, want builtin", sk.Scope)
			}
			if sk.RunAs != c.wantRunAs {
				t.Errorf("RunAs = %v, want %v", sk.RunAs, c.wantRunAs)
			}
			if sk.Description == "" {
				t.Error("Description is empty — skill won't appear in the model index")
			}
			if len(sk.AllowedTools) == 0 {
				t.Fatal("AllowedTools is empty — office skills must scope MCP tools")
			}
			found := false
			for _, tool := range sk.AllowedTools {
				if tool == c.mustHaveTool {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("AllowedTools = %v, missing %q", sk.AllowedTools, c.mustHaveTool)
			}
			if sk.Body == "" {
				t.Error("Body is empty")
			}
			if c.mustMentionIn != "" && !containsSubstring(sk.Body, c.mustMentionIn) {
				t.Errorf("Body must mention %q (it's the contract of the skill)", c.mustMentionIn)
			}
		})
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

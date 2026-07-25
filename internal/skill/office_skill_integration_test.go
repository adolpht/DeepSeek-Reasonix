package skill

import (
	"testing"
)

// TestMiniMaxOfficeSkillsBuiltin verifies the MiniMax office skills are
// materialized to disk and discoverable with the correct run mode and
// allowed tools. These skills are embedded at compile time and written
// to ~/.rexion/skills/ on first boot (MaterializeBuiltins).
func TestMiniMaxOfficeSkillsBuiltin(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: false, Stderr: nil})
	st.MaterializeBuiltins()

	cases := []struct {
		name         string
		wantRunAs    RunAs
		mustHaveTool string
	}{
		{"minimax-docx", RunSubagent, "write_docx"},
		{"minimax-xlsx", RunSubagent, "write_sheet"},
		{"minimax-pdf", RunSubagent, "write_pdf"},
		{"pptx-generator", RunSubagent, "mcp__slides__create_ppt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sk, ok := st.Read(c.name)
			if !ok {
				t.Fatalf("MiniMax skill %q not loaded after MaterializeBuiltins", c.name)
			}
			if sk.RunAs != c.wantRunAs {
				t.Errorf("RunAs = %v, want %v", sk.RunAs, c.wantRunAs)
			}
			if sk.Description == "" {
				t.Error("Description is empty — skill won't appear in the model index")
			}
			if len(sk.AllowedTools) == 0 {
				t.Fatal("AllowedTools is empty — office skills must scope tools")
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
		})
	}
}

// TestMiniMaxOfficeSkillsIdempotent verifies that MaterializeBuiltins is
// idempotent: running it twice does not error and does not overwrite
// user-customized skills.
func TestMiniMaxOfficeSkillsIdempotent(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: false, Stderr: nil})
	st.MaterializeBuiltins()
	st.MaterializeBuiltins() // Second call should be a no-op for existing skills

	sk, ok := st.Read("minimax-docx")
	if !ok {
		t.Fatal("minimax-docx not loaded after double MaterializeBuiltins")
	}
	if sk.Body == "" {
		t.Error("Body is empty after double materialization")
	}
}

// TestOldOfficeSkillsRemoved verifies that the old office skills
// (contract-draft, weekly-report, meeting-minutes) are no longer
// registered as builtins.
func TestOldOfficeSkillsRemoved(t *testing.T) {
	st := New(Options{HomeDir: t.TempDir(), DisableBuiltins: false, Stderr: nil})

	removed := []string{"contract-draft", "weekly-report", "meeting-minutes"}
	for _, name := range removed {
		t.Run(name, func(t *testing.T) {
			sk, ok := st.Read(name)
			if ok && sk.Scope == ScopeBuiltin {
				t.Errorf("old office skill %q still registered as builtin — should have been replaced by MiniMax skills", name)
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

package skill

import (
	"io"
	"testing"
)

// TestSheetSkillsLoad verifies the built-in sheet-analysis and sheet-clean skills
// are registered with the correct frontmatter. This guards against frontmatter
// typos (wrong key names, missing runAs/allowed-tools) that would silently make
// a skill load as inline or without tool scoping.
func TestSheetSkillsLoad(t *testing.T) {
	st := New(Options{DisableBuiltins: false, Stderr: io.Discard})

	cases := []struct {
		name         string
		wantRunAs    RunAs
		mustHaveTool string
	}{
		{"sheet-analysis", RunSubagent, "mcp__sheet__read_sheet"},
		{"sheet-clean", RunSubagent, "mcp__sheet__read_sheet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sk, ok := st.Read(c.name)
			if !ok {
				t.Fatalf("skill %q not found in builtin skills", c.name)
			}
			if sk.RunAs != c.wantRunAs {
				t.Errorf("RunAs = %v, want %v (check builtin definition)", sk.RunAs, c.wantRunAs)
			}
			if sk.Description == "" {
				t.Error("Description is empty — skill won't appear in the model index")
			}
			if len(sk.AllowedTools) == 0 {
				t.Fatal("AllowedTools is empty — check builtin definition")
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

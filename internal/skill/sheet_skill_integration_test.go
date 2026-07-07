package skill

import (
	"io"
	"path/filepath"
	"testing"
)

// TestSheetSkillsLoad verifies the real sheet-analysis and sheet-clean skill
// files under .rexion/commands/ parse correctly with the expected frontmatter.
// This is an integration test against the project's own skill files — it guards
// against frontmatter typos (wrong key names, missing runas/allowed-tools) that
// would silently make a skill load as inline or without tool scoping.
func TestSheetSkillsLoad(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	st := New(Options{ProjectRoot: root, DisableBuiltins: true, Stderr: io.Discard})

	cases := []struct {
		name        string
		wantRunAs   RunAs
		mustHaveTool string
	}{
		{"sheet-analysis", RunSubagent, "mcp__sheet__read_sheet"},
		{"sheet-clean", RunSubagent, "mcp__sheet__read_sheet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sk, ok := st.Read(c.name)
			if !ok {
				t.Fatalf("skill %q not loaded from %s/.rexion/commands/", c.name, root)
			}
			if sk.Scope != ScopeProject {
				t.Errorf("Scope = %v, want project", sk.Scope)
			}
			if sk.RunAs != c.wantRunAs {
				t.Errorf("RunAs = %v, want %v (check frontmatter 'runas:' key)", sk.RunAs, c.wantRunAs)
			}
			if sk.Description == "" {
				t.Error("Description is empty — skill won't appear in the model index")
			}
			if len(sk.AllowedTools) == 0 {
				t.Fatal("AllowedTools is empty — check frontmatter 'allowed-tools:' key")
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

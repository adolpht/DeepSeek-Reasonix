package permission

import (
	"encoding/json"
	"testing"
)

func TestLaneMatchesEmptyPath(t *testing.T) {
	lane := Lane{Name: "global", Path: ""}
	if !laneMatches(lane, "anything") {
		t.Error("empty Path lane should match all subjects")
	}
}

func TestLaneMatchesPrefix(t *testing.T) {
	lane := Lane{Name: "src", Path: "src/"}
	if !laneMatches(lane, "src/main.go") {
		t.Error("lane with Path 'src/' should match 'src/main.go'")
	}
	if !laneMatches(lane, "src/utils/helper.go") {
		t.Error("lane with Path 'src/' should match 'src/utils/helper.go'")
	}
	if laneMatches(lane, "prod/main.go") {
		t.Error("lane with Path 'src/' should NOT match 'prod/main.go'")
	}
}

func TestLaneMatchesGlob(t *testing.T) {
	lane := Lane{Name: "docs", Path: "*.md"}
	if !laneMatches(lane, "README.md") {
		t.Error("lane with Path '*.md' should match 'README.md'")
	}
	if !laneMatches(lane, "docs/guide.md") {
		t.Error("lane with Path '*.md' should match 'docs/guide.md'")
	}
	if laneMatches(lane, "main.go") {
		t.Error("lane with Path '*.md' should NOT match 'main.go'")
	}
}

func TestLaneMatchesCommandPath(t *testing.T) {
	lane := Lane{Name: "deploy", Path: "deploy/"}
	// A bash command that operates on a path under deploy/
	if !laneMatches(lane, "cat deploy/config.yaml") {
		t.Error("lane should match command with deploy/ path")
	}
	if laneMatches(lane, "cat src/main.go") {
		t.Error("lane should NOT match command with src/ path")
	}
}

func TestMultiPolicyLaneDenyOverridesBase(t *testing.T) {
	base := Policy{
		Mode:  Ask,
		Allow: []Rule{{Tool: "write_file"}},
	}
	lanes := []Lane{
		{
			Name: "prod",
			Path: "prod/",
			Deny: []Rule{{Tool: "write_file"}},
		},
	}
	mp := NewMultiPolicy(base, lanes)

	// write_file on a src/ path: base policy allows.
	d := mp.Decide("write_file", false, json.RawMessage(`{"file_path":"src/main.go"}`))
	if d != Allow {
		t.Errorf("write_file on src/ should be Allow (base), got %v", d)
	}

	// write_file on a prod/ path: lane denies.
	d = mp.Decide("write_file", false, json.RawMessage(`{"file_path":"prod/config.yaml"}`))
	if d != Deny {
		t.Errorf("write_file on prod/ should be Deny (lane), got %v", d)
	}
}

func TestMultiPolicyLaneAllowOverridesBase(t *testing.T) {
	base := Policy{
		Mode: Ask,
	}
	lanes := []Lane{
		{
			Name:         "src",
			Path:         "src/",
			OverrideMode: true,
			Mode:         Allow,
		},
	}
	mp := NewMultiPolicy(base, lanes)

	// write_file on src/: lane overrides mode to Allow.
	d := mp.Decide("write_file", false, json.RawMessage(`{"file_path":"src/main.go"}`))
	if d != Allow {
		t.Errorf("write_file on src/ should be Allow (lane override), got %v", d)
	}

	// write_file on prod/: no lane matches, base mode = Ask.
	d = mp.Decide("write_file", false, json.RawMessage(`{"file_path":"prod/config.yaml"}`))
	if d != Ask {
		t.Errorf("write_file on prod/ should be Ask (base mode), got %v", d)
	}
}

func TestMultiPolicyLaneAskOverridesBase(t *testing.T) {
	base := Policy{
		Mode:  Allow, // everything allowed by default
		Allow: []Rule{{Tool: "edit_file"}},
	}
	lanes := []Lane{
		{
			Name: "config",
			Path: "config/",
			Ask:  []Rule{{Tool: "edit_file"}},
		},
	}
	mp := NewMultiPolicy(base, lanes)

	// edit_file on config/: lane asks.
	d := mp.Decide("edit_file", false, json.RawMessage(`{"file_path":"config/settings.json"}`))
	if d != Ask {
		t.Errorf("edit_file on config/ should be Ask (lane), got %v", d)
	}

	// edit_file on src/: base allows.
	d = mp.Decide("edit_file", false, json.RawMessage(`{"file_path":"src/main.go"}`))
	if d != Allow {
		t.Errorf("edit_file on src/ should be Allow (base), got %v", d)
	}
}

func TestMultiPolicyNoLanesFallsBackToBase(t *testing.T) {
	base := Policy{
		Mode:  Ask,
		Allow: []Rule{{Tool: "write_file"}},
	}
	mp := NewMultiPolicy(base, nil)

	d := mp.Decide("write_file", false, json.RawMessage(`{"file_path":"src/main.go"}`))
	if d != Allow {
		t.Errorf("write_file with no lanes should be Allow (base), got %v", d)
	}
}

func TestMultiPolicyReadOnlyAlwaysAllows(t *testing.T) {
	base := Policy{
		Mode: Deny,
	}
	lanes := []Lane{
		{Name: "prod", Path: "prod/", OverrideMode: true, Mode: Deny},
	}
	mp := NewMultiPolicy(base, lanes)

	// read-only tool on prod/: should still allow (readers always pass).
	d := mp.Decide("read_file", true, json.RawMessage(`{"path":"prod/config.yaml"}`))
	if d != Allow {
		t.Errorf("read_file should always Allow, got %v", d)
	}
}

func TestMultiPolicyFirstMatchingLaneWins(t *testing.T) {
	base := Policy{Mode: Ask}
	lanes := []Lane{
		{Name: "src-allow", Path: "src/", OverrideMode: true, Mode: Allow},
		{Name: "src-deny", Path: "src/", OverrideMode: true, Mode: Deny}, // should never match
	}
	mp := NewMultiPolicy(base, lanes)

	d := mp.Decide("write_file", false, json.RawMessage(`{"file_path":"src/main.go"}`))
	if d != Allow {
		t.Errorf("first matching lane should win: expected Allow, got %v", d)
	}
}

func TestBuildLanes(t *testing.T) {
	configs := []LaneConfig{
		{
			Name:  "src",
			Path:  "src/",
			Mode:  "allow",
			Allow: []string{"write_file"},
			Ask:   []string{"bash(rm *)"},
			Deny:  []string{"bash(rm -rf *)"},
		},
	}
	lanes := BuildLanes(configs)
	if len(lanes) != 1 {
		t.Fatalf("BuildLanes returned %d lanes, want 1", len(lanes))
	}
	l := lanes[0]
	if l.Name != "src" {
		t.Errorf("Name = %q, want %q", l.Name, "src")
	}
	if l.Path != "src/" {
		t.Errorf("Path = %q, want %q", l.Path, "src/")
	}
	if !l.OverrideMode {
		t.Error("OverrideMode should be true when Mode is set")
	}
	if l.Mode != Allow {
		t.Errorf("Mode = %v, want Allow", l.Mode)
	}
	if len(l.Allow) != 1 || l.Allow[0].Tool != "write_file" {
		t.Errorf("Allow rules = %+v, want [write_file]", l.Allow)
	}
	if len(l.Ask) != 1 || l.Ask[0].Tool != "bash" {
		t.Errorf("Ask rules = %+v, want [bash]", l.Ask)
	}
	if len(l.Deny) != 1 || l.Deny[0].Tool != "bash" {
		t.Errorf("Deny rules = %+v, want [bash]", l.Deny)
	}
}

func TestBuildLanesEmpty(t *testing.T) {
	if lanes := BuildLanes(nil); lanes != nil {
		t.Errorf("BuildLanes(nil) = %+v, want nil", lanes)
	}
}

func TestExtractPathFromCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"cat src/main.go", "src/main.go"},
		{"cat /etc/passwd", "/etc/passwd"},
		{"cat ./config.yaml", "./config.yaml"},
		{"ls -la", ""},
		{"go build ./...", "./..."},
		{"rm -rf deploy/", ""}, // no extension, not a path-like prefix
	}
	for _, tt := range tests {
		got := extractPathFromCommand(tt.cmd)
		if got != tt.want {
			t.Errorf("extractPathFromCommand(%q) = %q, want %q", tt.cmd, got, tt.want)
		}
	}
}

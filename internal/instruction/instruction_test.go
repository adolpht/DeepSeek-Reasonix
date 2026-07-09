package instruction

import (
	"strings"
	"testing"

	"rexion/internal/memory"
)

func TestExtractHostChecksFromStructuredSection(t *testing.T) {
	docs := []memory.Source{{
		Path:  "AGENTS.md",
		Scope: memory.ScopeProject,
		Body: strings.Join([]string{
			"# Project rules",
			"## Rexion host checks",
			"- verify: go test ./internal/...",
			"* verify: git diff --check",
			"- verify: go test ./internal/...",
			"- note: ignored",
			"## Other",
			"- verify: ignored after section",
		}, "\n"),
	}}

	checks := ExtractHostChecks(docs)
	if len(checks) != 2 {
		t.Fatalf("checks len = %d, want 2: %#v", len(checks), checks)
	}
	if checks[0].Command != "go test ./internal/..." || checks[0].SourcePath != "AGENTS.md" || checks[0].Line != 3 {
		t.Fatalf("first check = %#v", checks[0])
	}
	if checks[1].Command != "git diff --check" || checks[1].SourcePath != "AGENTS.md" || checks[1].Line != 4 {
		t.Fatalf("second check = %#v", checks[1])
	}
}

func TestExtractHostChecksIgnoresOrdinaryGuidance(t *testing.T) {
	docs := []memory.Source{{
		Path: "Rexion.md",
		Body: "Always run go test before committing.\n\n- verify: go test ./...",
	}}

	if checks := ExtractHostChecks(docs); len(checks) != 0 {
		t.Fatalf("ordinary guidance should not create hard checks: %#v", checks)
	}
}

func TestExtractHostChecksIsCaseInsensitive(t *testing.T) {
	docs := []memory.Source{{
		Path: "Rexion.md",
		Body: "## Rexion HOST checks\n- verify: go test ./...",
	}}

	checks := ExtractHostChecks(docs)
	if len(checks) != 1 || checks[0].Command != "go test ./..." {
		t.Fatalf("case-insensitive heading not extracted: %#v", checks)
	}
}

func TestExtractReadinessLevelBasic(t *testing.T) {
	// No readiness setting → empty string
	docs := []memory.Source{{
		Path: "Rexion.md",
		Body: "## Rexion host checks\n- verify: go test ./...",
	}}
	if got := ExtractReadinessLevel(docs); got != "" {
		t.Fatalf("ExtractReadinessLevel = %q, want empty", got)
	}
}

func TestExtractReadinessLevelVerified(t *testing.T) {
	docs := []memory.Source{{
		Path: "AGENTS.md",
		Body: "# Project rules\n## Rexion host checks\n- verify: go test ./...\n- readiness: verified\n",
	}}
	if got := ExtractReadinessLevel(docs); got != "verified" {
		t.Fatalf("ExtractReadinessLevel = %q, want %q", got, "verified")
	}
}

func TestExtractReadinessLevelMergeReady(t *testing.T) {
	docs := []memory.Source{{
		Path: "AGENTS.md",
		Body: "## Rexion host checks\n- readiness: merge-ready\n",
	}}
	if got := ExtractReadinessLevel(docs); got != "merge-ready" {
		t.Fatalf("ExtractReadinessLevel = %q, want %q", got, "merge-ready")
	}
}

func TestExtractReadinessLevelCaseInsensitive(t *testing.T) {
	docs := []memory.Source{{
		Path: "AGENTS.md",
		Body: "## Rexion HOST checks\n- READINESS: verified\n",
	}}
	if got := ExtractReadinessLevel(docs); got != "verified" {
		t.Fatalf("ExtractReadinessLevel = %q, want %q", got, "verified")
	}
}

func TestExtractReadinessLevelOutsideSectionIgnored(t *testing.T) {
	docs := []memory.Source{{
		Path: "Rexion.md",
		Body: "- readiness: verified\n## Rexion host checks\n- verify: go test ./...\n",
	}}
	if got := ExtractReadinessLevel(docs); got != "" {
		t.Fatalf("ExtractReadinessLevel = %q, want empty (outside section)", got)
	}
}

func TestExtractReadinessLevelPickFirstOnly(t *testing.T) {
	// Multiple docs: only the first doc's setting is used
	docs := []memory.Source{
		{
			Path: "AGENTS.md",
			Body: "## Rexion host checks\n- readiness: merge-ready\n- verify: go test ./...\n",
		},
		{
			Path: "Rexion.local.md",
			Body: "## Rexion host checks\n- readiness: verified\n",
		},
	}
	if got := ExtractReadinessLevel(docs); got != "merge-ready" {
		t.Fatalf("ExtractReadinessLevel = %q, want %q", got, "merge-ready")
	}
}

func TestReadinessBullet(t *testing.T) {
	tests := []struct {
		line string
		val  string
		ok   bool
	}{
		{"- readiness: verified", "verified", true},
		{"* readiness: merge-ready", "merge-ready", true},
		{"- READINESS: verified", "verified", true},
		{"- verify: go test ./...", "", false},
		{"- readiness:", "", false},
		{"regular text", "", false},
	}
	for _, tt := range tests {
		val, ok := readinessBullet(tt.line)
		if ok != tt.ok || val != tt.val {
			t.Errorf("readinessBullet(%q) = (%q, %v), want (%q, %v)", tt.line, val, ok, tt.val, tt.ok)
		}
	}
}

package agent

import (
	"strings"
	"testing"
)

func TestAssessContextLoose(t *testing.T) {
	cp := AssessContext(100000, 30000) // 30%
	if cp.Level != ContextLoose {
		t.Errorf("Level = %v, want Loose", cp.Level)
	}
	if cp.UsagePercent != 30 {
		t.Errorf("UsagePercent = %d, want 30", cp.UsagePercent)
	}
	if cp.ToolGuidance() != "" {
		t.Errorf("ToolGuidance should be empty when loose, got %q", cp.ToolGuidance())
	}
}

func TestAssessContextModerate(t *testing.T) {
	cp := AssessContext(100000, 60000) // 60%
	if cp.Level != ContextModerate {
		t.Errorf("Level = %v, want Moderate", cp.Level)
	}
	if !cp.ShouldPreferGrep() {
		t.Error("ShouldPreferGrep should be true at moderate")
	}
	if cp.ShouldUseOffsetLimit() {
		t.Error("ShouldUseOffsetLimit should be false at moderate")
	}
}

func TestAssessContextTight(t *testing.T) {
	cp := AssessContext(100000, 80000) // 80%
	if cp.Level != ContextTight {
		t.Errorf("Level = %v, want Tight", cp.Level)
	}
	if !cp.ShouldPreferGrep() {
		t.Error("ShouldPreferGrep should be true at tight")
	}
	if !cp.ShouldUseOffsetLimit() {
		t.Error("ShouldUseOffsetLimit should be true at tight")
	}
}

func TestAssessContextCritical(t *testing.T) {
	cp := AssessContext(100000, 95000) // 95%
	if cp.Level != ContextCritical {
		t.Errorf("Level = %v, want Critical", cp.Level)
	}
	if !cp.ShouldCompact() {
		t.Error("ShouldCompact should be true at critical")
	}
}

func TestAssessContextUnknown(t *testing.T) {
	cp := AssessContext(0, 0)
	if cp.Level != ContextLoose {
		t.Errorf("Level = %v, want Loose (default for unknown)", cp.Level)
	}
	if cp.UsagePercent != -1 {
		t.Errorf("UsagePercent = %d, want -1", cp.UsagePercent)
	}
}

func TestContextPressureLevelString(t *testing.T) {
	tests := []struct {
		level ContextPressureLevel
		want  string
	}{
		{ContextLoose, "loose"},
		{ContextModerate, "moderate"},
		{ContextTight, "tight"},
		{ContextCritical, "critical"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestToolGuidanceCritical(t *testing.T) {
	cp := AssessContext(100000, 95000)
	g := cp.ToolGuidance()
	if !strings.Contains(g, "critical") {
		t.Errorf("guidance should mention 'critical': %q", g)
	}
	if !strings.Contains(g, "compacting") {
		t.Errorf("guidance should suggest compacting: %q", g)
	}
}

func TestToolGuidanceTight(t *testing.T) {
	cp := AssessContext(100000, 80000)
	g := cp.ToolGuidance()
	if !strings.Contains(g, "tight") {
		t.Errorf("guidance should mention 'tight': %q", g)
	}
	if !strings.Contains(g, "targeted") {
		t.Errorf("guidance should suggest targeted queries: %q", g)
	}
}

func TestPreferredTools(t *testing.T) {
	cp := AssessContext(100000, 95000) // critical
	pt := cp.PreferredTools()
	if !strings.Contains(pt, "grep") {
		t.Errorf("PreferredTools at critical should list grep first: %q", pt)
	}

	cp = AssessContext(100000, 30000) // loose
	pt = cp.PreferredTools()
	if !strings.Contains(pt, "all tools") {
		t.Errorf("PreferredTools at loose should say 'all tools': %q", pt)
	}
}

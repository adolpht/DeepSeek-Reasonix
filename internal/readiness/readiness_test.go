package readiness

import "testing"

func TestLevelString(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{Basic, "basic"},
		{Verified, "verified"},
		{MergeReady, "merge-ready"},
	}
	for _, tt := range tests {
		got := tt.level.String()
		if got != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"basic", Basic},
		{"", Basic},
		{"verified", Verified},
		{"merge-ready", MergeReady},
		{"merge_ready", MergeReady},
		{"mergeReady", MergeReady},
		{"unknown", Basic},
	}
	for _, tt := range tests {
		got := ParseLevel(tt.input)
		if got != tt.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestLevelMarshalText(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{Basic, "basic"},
		{Verified, "verified"},
		{MergeReady, "merge-ready"},
	}
	for _, tt := range tests {
		b, err := tt.level.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v): %v", tt.level, err)
		}
		if string(b) != tt.want {
			t.Errorf("MarshalText(%v) = %q, want %q", tt.level, b, tt.want)
		}
	}
}

func TestLevelUnmarshalText(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"basic", Basic},
		{"verified", Verified},
		{"merge-ready", MergeReady},
		{"merge_ready", MergeReady},
	}
	for _, tt := range tests {
		var l Level
		err := l.UnmarshalText([]byte(tt.input))
		if err != nil {
			t.Fatalf("UnmarshalText(%q): %v", tt.input, err)
		}
		if l != tt.want {
			t.Errorf("UnmarshalText(%q) = %v, want %v", tt.input, l, tt.want)
		}
	}
}

func TestMissingKindString(t *testing.T) {
	tests := []struct {
		kind MissingKind
		want string
	}{
		{MissingCompleteStep, "complete_step"},
		{MissingProjectCheck, "project_check"},
		{MissingTodoComplete, "todo_complete"},
	}
	for _, tt := range tests {
		got := tt.kind.String()
		if got != tt.want {
			t.Errorf("MissingKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestCheckResult(t *testing.T) {
	// Verify struct construction works correctly
	cr := CheckResult{
		Applies:  true,
		Level:    Verified,
		Satisfied: false,
		Reason:   "missing complete_step",
		MissingItems: []MissingItem{
			{Kind: MissingCompleteStep, Description: "call complete_step after the latest write"},
		},
	}
	if !cr.Applies {
		t.Error("Applies should be true")
	}
	if cr.Level != Verified {
		t.Error("Level should be Verified")
	}
	if cr.Satisfied {
		t.Error("Satisfied should be false")
	}
	if len(cr.MissingItems) != 1 {
		t.Errorf("MissingItems should have 1 item, got %d", len(cr.MissingItems))
	}
}

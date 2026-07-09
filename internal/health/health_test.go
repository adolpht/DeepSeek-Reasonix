package health

import (
	"testing"
)

func TestStatusString(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{Healthy, "healthy"},
		{Warning, "warning"},
		{Critical, "critical"},
	}
	for _, tt := range tests {
		got := tt.s.String()
		if got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestTrackerHealthy(t *testing.T) {
	tr := NewTracker()
	r := tr.Report(50) // 50% context usage
	if !r.IsHealthy() {
		t.Errorf("expected healthy, got %s", r.Status)
	}
	if r.Summary() != "session healthy" {
		t.Errorf("Summary() = %q, want %q", r.Summary(), "session healthy")
	}
}

func TestTrackerWarningContext(t *testing.T) {
	tr := NewTracker()
	r := tr.Report(75) // 75% context usage (warning threshold = 70)
	if r.Status != Warning {
		t.Errorf("expected warning, got %s", r.Status)
	}
	if len(r.Warnings) == 0 {
		t.Error("expected at least one warning")
	}
}

func TestTrackerCriticalContext(t *testing.T) {
	tr := NewTracker()
	r := tr.Report(95) // 95% context usage (critical threshold = 90)
	if r.Status != Critical {
		t.Errorf("expected critical, got %s", r.Status)
	}
}

func TestTrackerCompactionStuck(t *testing.T) {
	tr := NewTracker()
	tr.SetCompactionStuck(true)
	r := tr.Report(50)
	if r.Status != Critical {
		t.Errorf("expected critical when compaction stuck, got %s", r.Status)
	}
	if !r.CompactionStuck {
		t.Error("CompactionStuck should be true")
	}
}

func TestTrackerErrorRate(t *testing.T) {
	tr := NewTracker()
	// 4 failures out of 5 = 80% error rate (critical threshold)
	for i := 0; i < 4; i++ {
		tr.RecordToolCall(false)
	}
	tr.RecordToolCall(true)
	r := tr.Report(30)
	if r.Status != Critical {
		t.Errorf("expected critical for high error rate, got %s", r.Status)
	}
	if r.ToolCallsTotal != 5 {
		t.Errorf("ToolCallsTotal = %d, want 5", r.ToolCallsTotal)
	}
	if r.ToolCallsFailed != 4 {
		t.Errorf("ToolCallsFailed = %d, want 4", r.ToolCallsFailed)
	}
}

func TestTrackerWarningErrorRate(t *testing.T) {
	tr := NewTracker()
	// 2 failures out of 4 = 50% error rate (warning threshold)
	tr.RecordToolCall(false)
	tr.RecordToolCall(false)
	tr.RecordToolCall(true)
	tr.RecordToolCall(true)
	r := tr.Report(30)
	if r.Status != Warning {
		t.Errorf("expected warning for moderate error rate, got %s", r.Status)
	}
}

func TestTrackerLoopDetected(t *testing.T) {
	tr := NewTracker()
	tr.SetLoopDetected(true)
	r := tr.Report(30)
	if r.Status != Warning {
		t.Errorf("expected warning when loop detected, got %s", r.Status)
	}
	if !r.LoopDetected {
		t.Error("LoopDetected should be true")
	}
}

func TestTrackerReset(t *testing.T) {
	tr := NewTracker()
	tr.RecordToolCall(false)
	tr.RecordToolCall(true)
	tr.SetLoopDetected(true)
	tr.Reset()
	r := tr.Report(30)
	if r.ToolCallsTotal != 0 {
		t.Errorf("ToolCallsTotal after Reset = %d, want 0", r.ToolCallsTotal)
	}
	if r.LoopDetected {
		t.Error("LoopDetected should be false after Reset")
	}
}

func TestTrackerSuccessRate(t *testing.T) {
	tr := NewTracker()
	// No calls yet.
	r := tr.Report(30)
	if r.ToolSuccessRate != -1 {
		t.Errorf("ToolSuccessRate with no calls = %f, want -1", r.ToolSuccessRate)
	}
	// 3 success, 1 failure = 75% success rate.
	tr.RecordToolCall(true)
	tr.RecordToolCall(true)
	tr.RecordToolCall(true)
	tr.RecordToolCall(false)
	r = tr.Report(30)
	if r.ToolSuccessRate != 0.75 {
		t.Errorf("ToolSuccessRate = %f, want 0.75", r.ToolSuccessRate)
	}
}

func TestReportSummary(t *testing.T) {
	tr := NewTracker()
	tr.SetCompactionStuck(true)
	r := tr.Report(50)
	s := r.Summary()
	if s == "session healthy" {
		t.Errorf("Summary() should not be healthy when compaction stuck, got %q", s)
	}
}

func TestTrackerUnknownContext(t *testing.T) {
	tr := NewTracker()
	r := tr.Report(-1) // unknown context usage
	if r.Status != Healthy {
		t.Errorf("expected healthy with unknown context, got %s", r.Status)
	}
}

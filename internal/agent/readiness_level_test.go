package agent

import (
	"testing"

	"rexion/internal/evidence"
	"rexion/internal/readiness"
)

// TestFinalReadinessCheckBasic verifies the Basic level matches the original
// behaviour: no check when there are no project checks, no todo receipts, and
// no incomplete todos.
func TestFinalReadinessCheckBasic(t *testing.T) {
	a := &Agent{evidence: evidence.NewLedger(), readinessLevel: readiness.Basic}
	got := a.finalReadinessCheck()
	if got.applies {
		t.Error("Basic level should not apply when there are no project checks or todos")
	}
	if got.level != readiness.Basic {
		t.Errorf("level = %v, want Basic", got.level)
	}
}

// TestFinalReadinessCheckBasicIncompleteTodos verifies that Basic level still
// blocks on incomplete todos (original behaviour).
func TestFinalReadinessCheckBasicIncompleteTodos(t *testing.T) {
	led := evidence.NewLedger()
	led.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "do thing", Status: "in_progress"},
		},
	})
	a := &Agent{evidence: led, readinessLevel: readiness.Basic}
	got := a.finalReadinessCheck()
	if !got.applies {
		t.Error("Basic level should apply when there are incomplete todos")
	}
	if got.reason == "" {
		t.Error("reason should be non-empty for incomplete todos")
	}
}

// TestFinalReadinessCheckVerifiedNoWrite verifies that Verified level with no
// writes is satisfied (nothing to verify).
func TestFinalReadinessCheckVerifiedNoWrite(t *testing.T) {
	led := evidence.NewLedger()
	a := &Agent{evidence: led, readinessLevel: readiness.Verified}
	got := a.finalReadinessCheck()
	if !got.applies {
		t.Error("Verified level should always apply")
	}
	if !got.satisfied {
		t.Error("Verified with no writes should be satisfied")
	}
}

// TestFinalReadinessCheckVerifiedWriteNoCompleteStep verifies that Verified
// level blocks when there is a write but no complete_step after it.
func TestFinalReadinessCheckVerifiedWriteNoCompleteStep(t *testing.T) {
	led := evidence.NewLedger()
	led.Record(evidence.Receipt{
		ToolName: "write_file",
		Success:  true,
		Write:    true,
		Paths:    []string{"test.go"},
	})
	a := &Agent{evidence: led, readinessLevel: readiness.Verified}
	got := a.finalReadinessCheck()
	if !got.applies {
		t.Error("Verified level should always apply")
	}
	if got.satisfied {
		t.Error("Verified with write but no complete_step should NOT be satisfied")
	}
	if !got.missingCompleteStep {
		t.Error("missingCompleteStep should be true")
	}
	if got.reason == "" {
		t.Error("reason should be non-empty")
	}
}

// TestFinalReadinessCheckVerifiedWriteWithCompleteStep verifies that Verified
// level is satisfied when complete_step follows a write.
func TestFinalReadinessCheckVerifiedWriteWithCompleteStep(t *testing.T) {
	led := evidence.NewLedger()
	led.Record(evidence.Receipt{
		ToolName: "write_file",
		Success:  true,
		Write:    true,
		Paths:    []string{"test.go"},
	})
	led.Record(evidence.Receipt{
		ToolName: "complete_step",
		Success:  true,
		Step:     "write test.go",
	})
	a := &Agent{evidence: led, readinessLevel: readiness.Verified}
	got := a.finalReadinessCheck()
	if !got.applies {
		t.Error("Verified level should always apply")
	}
	if !got.satisfied {
		t.Errorf("Verified with write + complete_step should be satisfied, reason=%q", got.reason)
	}
}

// TestFinalReadinessCheckVerifiedIncompleteTodos verifies that Verified level
// blocks on incomplete todos even without writes.
func TestFinalReadinessCheckVerifiedIncompleteTodos(t *testing.T) {
	led := evidence.NewLedger()
	led.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "do thing", Status: "in_progress"},
		},
	})
	a := &Agent{evidence: led, readinessLevel: readiness.Verified}
	got := a.finalReadinessCheck()
	if !got.applies {
		t.Error("Verified level should always apply")
	}
	if got.satisfied {
		t.Error("Verified with incomplete todos should NOT be satisfied")
	}
	if got.incompleteTodos != 1 {
		t.Errorf("incompleteTodos = %d, want 1", got.incompleteTodos)
	}
}

// TestFinalReadinessCheckMergeReadyNoWrite verifies that MergeReady with no
// writes and no todos is satisfied.
func TestFinalReadinessCheckMergeReadyNoWrite(t *testing.T) {
	led := evidence.NewLedger()
	a := &Agent{evidence: led, readinessLevel: readiness.MergeReady}
	got := a.finalReadinessCheck()
	if !got.applies {
		t.Error("MergeReady level should always apply")
	}
	if !got.satisfied {
		t.Error("MergeReady with no writes and no todos should be satisfied")
	}
}

// TestFinalReadinessCheckMergeReadyWriteNoCompleteStep verifies that MergeReady
// blocks when there is a write but no complete_step.
func TestFinalReadinessCheckMergeReadyWriteNoCompleteStep(t *testing.T) {
	led := evidence.NewLedger()
	led.Record(evidence.Receipt{
		ToolName: "write_file",
		Success:  true,
		Write:    true,
		Paths:    []string{"test.go"},
	})
	a := &Agent{evidence: led, readinessLevel: readiness.MergeReady}
	got := a.finalReadinessCheck()
	if got.satisfied {
		t.Error("MergeReady with write but no complete_step should NOT be satisfied")
	}
	if !got.missingCompleteStep {
		t.Error("missingCompleteStep should be true")
	}
}

// TestFinalReadinessCheckMergeReadyIncompleteTodos verifies that MergeReady
// blocks on incomplete todos.
func TestFinalReadinessCheckMergeReadyIncompleteTodos(t *testing.T) {
	led := evidence.NewLedger()
	led.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "task A", Status: "completed"},
			{Content: "task B", Status: "in_progress"},
		},
	})
	a := &Agent{evidence: led, readinessLevel: readiness.MergeReady}
	got := a.finalReadinessCheck()
	if got.satisfied {
		t.Error("MergeReady with incomplete todos should NOT be satisfied")
	}
	if got.incompleteTodos != 1 {
		t.Errorf("incompleteTodos = %d, want 1", got.incompleteTodos)
	}
}

// TestFinalReadinessCheckMergeReadyAllSatisfied verifies that MergeReady is
// satisfied when all conditions are met: write + complete_step + no incomplete
// todos.
func TestFinalReadinessCheckMergeReadyAllSatisfied(t *testing.T) {
	led := evidence.NewLedger()
	led.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "task A", Status: "completed"},
		},
	})
	led.Record(evidence.Receipt{
		ToolName: "write_file",
		Success:  true,
		Write:    true,
		Paths:    []string{"test.go"},
	})
	led.Record(evidence.Receipt{
		ToolName: "complete_step",
		Success:  true,
		Step:     "write test.go",
	})
	a := &Agent{evidence: led, readinessLevel: readiness.MergeReady}
	got := a.finalReadinessCheck()
	if !got.satisfied {
		t.Errorf("MergeReady with all conditions met should be satisfied, reason=%q", got.reason)
	}
}

// TestFinalReadinessCheckNilEvidence verifies that nil evidence returns a
// zero-value check regardless of level.
func TestFinalReadinessCheckNilEvidence(t *testing.T) {
	for _, level := range []readiness.Level{readiness.Basic, readiness.Verified, readiness.MergeReady} {
		a := &Agent{evidence: nil, readinessLevel: level}
		got := a.finalReadinessCheck()
		if got.applies {
			t.Errorf("level %v: nil evidence should not apply", level)
		}
	}
}

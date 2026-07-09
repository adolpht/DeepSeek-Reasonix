// Package health provides session health monitoring for the agent. It tracks
// context usage, tool call success rates, loop detection, and compaction
// status, then reports a structured HealthReport that frontends can display
// or that the controller can act on (e.g., auto-compact, warn the user).
package health

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Status represents the overall health of a session.
type Status int

const (
	// Healthy means the session is running normally.
	Healthy Status = iota
	// Warning means the session is approaching a problem (e.g., high context
	// usage, elevated error rate) but can still function.
	Warning
	// Critical means the session is in trouble (e.g., context exhausted,
	// tool loops detected) and needs intervention.
	Critical
)

// String returns a human-readable status name.
func (s Status) String() string {
	switch s {
	case Healthy:
		return "healthy"
	case Warning:
		return "warning"
	case Critical:
		return "critical"
	default:
		return "unknown"
	}
}

// Report is a snapshot of the session's health at a point in time.
type Report struct {
	// Status is the overall health status.
	Status Status `json:"status"`

	// ContextUsagePercent is the estimated percentage of the context window
	// currently in use (0–100). -1 if unknown.
	ContextUsagePercent int `json:"context_usage_percent"`

	// CompactionStuck is true when auto-compaction has failed to reduce
	// the context enough and has given up.
	CompactionStuck bool `json:"compaction_stuck"`

	// ToolSuccessRate is the fraction of tool calls that succeeded in the
	// current turn (0.0–1.0). -1 if no calls yet.
	ToolSuccessRate float64 `json:"tool_success_rate"`

	// ToolCallsTotal is the total number of tool calls in the current turn.
	ToolCallsTotal int `json:"tool_calls_total"`

	// ToolCallsFailed is the number of failed tool calls in the current turn.
	ToolCallsFailed int `json:"tool_calls_failed"`

	// LoopDetected is true when the storm breaker or repeat-success guard
	// has triggered, indicating the model is stuck in a loop.
	LoopDetected bool `json:"loop_detected"`

	// Warnings lists specific health warnings (e.g., "context at 85%").
	Warnings []string `json:"warnings,omitempty"`

	// Timestamp is when this report was generated.
	Timestamp time.Time `json:"timestamp"`
}

// Tracker monitors session health metrics. It is safe for concurrent use.
type Tracker struct {
	mu sync.Mutex

	// Configuration thresholds.
	warningContextPercent int // default 70
	criticalContextPercent int // default 90
	warningErrorRate float64 // default 0.5
	criticalErrorRate float64 // default 0.8

	// Metrics (reset per turn).
	toolCallsTotal  atomic.Int64
	toolCallsFailed atomic.Int64

	// State from agent.
	compactionStuck atomic.Bool
	loopDetected    atomic.Bool
}

// NewTracker creates a health tracker with default thresholds.
func NewTracker() *Tracker {
	return &Tracker{
		warningContextPercent:  70,
		criticalContextPercent: 90,
		warningErrorRate:       0.5,
		criticalErrorRate:      0.8,
	}
}

// RecordToolCall records a tool call outcome (success or failure).
func (t *Tracker) RecordToolCall(success bool) {
	t.toolCallsTotal.Add(1)
	if !success {
		t.toolCallsFailed.Add(1)
	}
}

// SetCompactionStuck updates the compaction stuck flag.
func (t *Tracker) SetCompactionStuck(stuck bool) {
	t.compactionStuck.Store(stuck)
}

// SetLoopDetected updates the loop detection flag.
func (t *Tracker) SetLoopDetected(detected bool) {
	t.loopDetected.Store(detected)
}

// Reset clears per-turn metrics. Called at the start of each user turn.
func (t *Tracker) Reset() {
	t.toolCallsTotal.Store(0)
	t.toolCallsFailed.Store(0)
	t.loopDetected.Store(false)
}

// Report generates a health snapshot. contextUsagePercent is -1 if unknown.
func (t *Tracker) Report(contextUsagePercent int) Report {
	t.mu.Lock()
	defer t.mu.Unlock()

	r := Report{
		ContextUsagePercent: contextUsagePercent,
		CompactionStuck:     t.compactionStuck.Load(),
		LoopDetected:        t.loopDetected.Load(),
		Timestamp:           time.Now(),
	}

	total := t.toolCallsTotal.Load()
	failed := t.toolCallsFailed.Load()
	r.ToolCallsTotal = int(total)
	r.ToolCallsFailed = int(failed)

	if total > 0 {
		r.ToolSuccessRate = float64(total-failed) / float64(total)
	} else {
		r.ToolSuccessRate = -1
	}

	// Determine status.
	r.Status = Healthy
	var warnings []string

	// Context usage.
	if contextUsagePercent >= 0 {
		if contextUsagePercent >= t.criticalContextPercent {
			r.Status = Critical
			warnings = append(warnings, fmt.Sprintf("context usage at %d%%", contextUsagePercent))
		} else if contextUsagePercent >= t.warningContextPercent {
			if r.Status < Warning {
				r.Status = Warning
			}
			warnings = append(warnings, fmt.Sprintf("context usage at %d%%", contextUsagePercent))
		}
	}

	// Compaction stuck.
	if r.CompactionStuck {
		if r.Status < Critical {
			r.Status = Critical
		}
		warnings = append(warnings, "compaction stuck: context window too small")
	}

	// Error rate.
	if total >= 3 {
		errorRate := float64(failed) / float64(total)
		if errorRate >= t.criticalErrorRate {
			if r.Status < Critical {
				r.Status = Critical
			}
			warnings = append(warnings, fmt.Sprintf("tool error rate %.0f%%", errorRate*100))
		} else if errorRate >= t.warningErrorRate {
			if r.Status < Warning {
				r.Status = Warning
			}
			warnings = append(warnings, fmt.Sprintf("tool error rate %.0f%%", errorRate*100))
		}
	}

	// Loop detected.
	if r.LoopDetected {
		if r.Status < Warning {
			r.Status = Warning
		}
		warnings = append(warnings, "loop detected: model repeating the same action")
	}

	r.Warnings = warnings
	return r
}

// IsHealthy is a convenience method that returns true when status is Healthy.
func (r Report) IsHealthy() bool {
	return r.Status == Healthy
}

// Summary returns a one-line human-readable summary of the report.
func (r Report) Summary() string {
	if r.IsHealthy() {
		return "session healthy"
	}
	status := r.Status.String()
	if len(r.Warnings) > 0 {
		return fmt.Sprintf("%s: %s", status, r.Warnings[0])
	}
	return status
}

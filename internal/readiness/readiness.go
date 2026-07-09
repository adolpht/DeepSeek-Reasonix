// Package readiness provides a tiered quality gate for the agent's final-answer
// readiness check. It upgrades the ad-hoc string-based check with a structured
// ReadinessLevel that can be configured per project via AGENTS.md or Rexion.md.
//
// Levels:
//
//   - Basic: model gave a visible final answer (existing behaviour).
//   - Verified: Basic + complete_step called after the latest write (if any).
//   - MergeReady: Verified + all project verification commands passed after the
//     latest write + no incomplete todos + base branch is fresh.
//
// The level is checked in Agent.finalReadinessCheck and enforced before the
// model's final answer is accepted.
package readiness

// Level represents the quality bar a final answer must clear.
type Level int

const (
	// Basic requires only that the model produced a visible final answer.
	// This matches the pre-existing behaviour when no readiness level is
	// configured.
	Basic Level = iota

	// Verified requires that the model called complete_step after the latest
	// write operation, providing host-observable evidence that the work was
	// verified.
	Verified

	// MergeReady is the highest bar: Verified + all project-configured
	// verification commands (e.g. "go test ./...") passed after the latest
	// write + no incomplete todo items remain.
	MergeReady
)

// String returns a human-readable name for the level.
func (l Level) String() string {
	switch l {
	case Basic:
		return "basic"
	case Verified:
		return "verified"
	case MergeReady:
		return "merge-ready"
	default:
		return "unknown"
	}
}

// MarshalText implements encoding.TextMarshaler for config serialization.
func (l Level) MarshalText() ([]byte, error) {
	return []byte(l.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler for config deserialization.
func (l *Level) UnmarshalText(text []byte) error {
	switch string(text) {
	case "basic", "":
		*l = Basic
	case "verified":
		*l = Verified
	case "merge-ready", "merge_ready", "mergeReady":
		*l = MergeReady
	default:
		*l = Basic
	}
	return nil
}

// ParseLevel maps a config string to a Level. Unknown/empty defaults to Basic.
func ParseLevel(s string) Level {
	var l Level
	_ = l.UnmarshalText([]byte(s))
	return l
}

// CheckResult is the structured outcome of a readiness check against a Level.
type CheckResult struct {
	// Applies is true when the level imposes requirements beyond "visible
	// answer". When false, the check is a no-op (Basic level).
	Applies bool

	// Level is the readiness level that was checked.
	Level Level

	// Satisfied is true when all requirements for the level are met.
	Satisfied bool

	// Reason is a human-readable explanation of what is missing, when
	// Satisfied is false. Empty when satisfied.
	Reason string

	// MissingItems lists the specific unmet requirements.
	MissingItems []MissingItem
}

// MissingItem describes one unmet readiness requirement.
type MissingItem struct {
	// Kind categorizes the missing item for structured handling.
	Kind MissingKind

	// Description is a human-readable explanation of what is missing.
	Description string
}

// MissingKind categorizes a missing readiness requirement.
type MissingKind int

const (
	// MissingCompleteStep means the model did not call complete_step after
	// the latest write operation.
	MissingCompleteStep MissingKind = iota

	// MissingProjectCheck means a project-configured verification command
	// (e.g., "go test ./...") was not run after the latest write.
	MissingProjectCheck

	// MissingTodoComplete means the latest todo_write has items that are
	// not marked as completed.
	MissingTodoComplete
)

// String returns a human-readable name for the missing kind.
func (k MissingKind) String() string {
	switch k {
	case MissingCompleteStep:
		return "complete_step"
	case MissingProjectCheck:
		return "project_check"
	case MissingTodoComplete:
		return "todo_complete"
	default:
		return "unknown"
	}
}

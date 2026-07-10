// Package workflow provides storage and management for agent workflow DAGs.
// A workflow is a directed acyclic graph of nodes (skills, prompts, tools,
// conditions, parallel blocks) connected by edges that defines an automated
// multi-step agent process.
package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// WorkflowNode represents a single step in a workflow DAG.
type WorkflowNode struct {
	ID               string `json:"id"`                       // Unique node ID (e.g., "node-1")
	Label            string `json:"label"`                    // User-facing name (e.g., "Analyze Code")
	Kind             string `json:"kind"`                     // "skill" | "prompt" | "tool" | "condition" | "parallel"
	Config           string `json:"config"`                   // JSON config for the node (skill name, prompt template, tool args, condition expr)
	Model            string `json:"model,omitempty"`           // Override model for this node (empty = inherit)
	Effort           string `json:"effort,omitempty"`          // Override effort for this node
	RequireApproval  bool   `json:"requireApproval,omitempty"` // P3: gate this node behind ApprovalModal before running
	PositionX        int    `json:"positionX"`                 // Canvas X position
	PositionY        int    `json:"positionY"`                 // Canvas Y position
}

// WorkflowEdge represents a directed connection between two nodes.
type WorkflowEdge struct {
	ID     string `json:"id"`                          // Unique edge ID
	Source string `json:"source"`                      // Source node ID
	Target string `json:"target"`                      // Target node ID
	Label  string `json:"label,omitempty"`             // Edge label (e.g., "yes"/"no" for condition branches)
}

// TriggerType defines how a workflow is activated. Mirrors recipe.TriggerType
// so a single-node workflow can replace a Recipe (Recipe→Workflow migration).
type TriggerType string

const (
	// TriggerManual means the workflow runs only when the user clicks Run.
	TriggerManual TriggerType = "manual"
	// TriggerCron schedules the workflow via a cron expression.
	TriggerCron TriggerType = "cron"
	// TriggerEvent fires the workflow when an external event (e.g. mail_received) matches.
	TriggerEvent TriggerType = "event"
)

// TriggerConfig holds configuration for non-manual trigger types.
// For TriggerCron, CronExpr is the cron expression.
// For TriggerEvent, EventType is the event name and MatchRules are filtering rules
// (e.g. {"sender": "boss@company.com", "subject": "urgent"}).
type TriggerConfig struct {
	CronExpr   string            `json:"cronExpr,omitempty"`
	EventType  string            `json:"eventType,omitempty"`
	MatchRules map[string]string `json:"matchRules,omitempty"`
}

// Workflow represents a complete agent workflow DAG.
type Workflow struct {
	Name          string         `json:"name"`          // Unique identifier (filename-safe)
	Description   string         `json:"description"`   // User-facing description
	Nodes         []WorkflowNode `json:"nodes"`         // DAG nodes
	Edges         []WorkflowEdge `json:"edges"`         // DAG edges
	Trigger       TriggerType    `json:"trigger,omitempty"`       // How this workflow is activated (default "manual")
	TriggerConfig TriggerConfig  `json:"triggerConfig,omitempty"` // Trigger-specific configuration
	ErrorStrategy ErrorStrategy  `json:"errorStrategy,omitempty"` // How to handle node failures: "continue" (default) | "stop"
	Version       int            `json:"version,omitempty"`       // P3: schema version for forward-compat migrations (current 1)
	AllowedSkills []string       `json:"allowedSkills,omitempty"` // P3: when non-empty, skill nodes may only invoke these skills
	CreatedAt     int64          `json:"createdAt"`     // Unix milliseconds
	UpdatedAt     int64          `json:"updatedAt"`     // Unix milliseconds
}

// CurrentWorkflowVersion is the workflow schema version this build understands.
// Older files (Version 0 / missing) are still loadable; the runtime treats
// them as Version 1. Bump when a breaking change needs an explicit migration.
const CurrentWorkflowVersion = 1

// ── Run state ──────────────────────────────────────────────

// RunStatus represents the current state of a workflow execution.
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusAborted   RunStatus = "aborted"
	RunStatusSkipped   RunStatus = "skipped"
)

// ErrorStrategy defines how the workflow runtime handles a node execution failure.
type ErrorStrategy string

const (
	// ErrorContinue skips the failed node and continues with the next reachable
	// node. The failed node's output is empty, so downstream ${ref} substitutions
	// resolve to "". This is the default (backward-compatible) behaviour.
	ErrorContinue ErrorStrategy = "continue"
	// ErrorStop aborts the entire workflow when any executable node fails. The
	// workflow status is set to "failed".
	ErrorStop ErrorStrategy = "stop"
)

// NodeRunState records the execution state of a single node within a run.
type NodeRunState struct {
	ID     string    `json:"id"`
	Label  string    `json:"label"`
	Kind   string    `json:"kind"`
	Status RunStatus `json:"status"` // running / completed / failed / skipped
	Error  string    `json:"error,omitempty"`
	Output string    `json:"output,omitempty"` // captured assistant reply (truncated for status)
}

// RunState tracks the execution state of a running or completed workflow.
// It is maintained in-memory by the desktop App and exposed to the frontend
// so the WorkflowEditor can render live progress.
type RunState struct {
	WorkflowName string         `json:"workflowName"`
	TabID        string         `json:"tabId"`
	Status       RunStatus      `json:"status"`
	Nodes        []NodeRunState `json:"nodes"`
	StartedAt    int64          `json:"startedAt"`  // Unix ms
	FinishedAt   int64          `json:"finishedAt"` // Unix ms, 0 while running
	Error        string         `json:"error,omitempty"`
}

// Store manages workflow persistence in ~/.rexion/workflows/.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// NewStore creates a workflow store backed by the given directory.
// If dir is empty, it defaults to ~/.rexion/workflows/.
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".rexion", "workflows")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create workflows dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Save persists a workflow to <dir>/<name>.json.
// If the workflow already exists, it is overwritten.
// The workflow is validated before saving; validation errors are returned
// without writing the file.
func (s *Store) Save(w Workflow) error {
	if err := Validate(w); err != nil {
		return fmt.Errorf("validation: %w", err)
	}
	w.UpdatedAt = time.Now().UnixMilli()
	if w.CreatedAt == 0 {
		w.CreatedAt = w.UpdatedAt
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, w.Name+".json")
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workflow: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write workflow file: %w", err)
	}
	return nil
}

// Load reads a workflow by name. Returns error if not found.
func (s *Store) Load(name string) (Workflow, error) {
	if name == "" {
		return Workflow{}, fmt.Errorf("workflow name is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Workflow{}, fmt.Errorf("workflow %q not found", name)
		}
		return Workflow{}, fmt.Errorf("read workflow file: %w", err)
	}
	var w Workflow
	if err := json.Unmarshal(data, &w); err != nil {
		return Workflow{}, fmt.Errorf("unmarshal workflow: %w", err)
	}
	return w, nil
}

// List returns all workflows, sorted by CreatedAt descending (newest first).
func (s *Store) List() ([]Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return make([]Workflow, 0), nil
		}
		return nil, fmt.Errorf("read workflows dir: %w", err)
	}

	var workflows []Workflow
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue // Skip unreadable files
		}
		var w Workflow
		if err := json.Unmarshal(data, &w); err != nil {
			continue // Skip malformed files
		}
		workflows = append(workflows, w)
	}

	if workflows == nil {
		workflows = make([]Workflow, 0)
	}

	// Sort by CreatedAt descending
	sort.Slice(workflows, func(i, j int) bool {
		return workflows[i].CreatedAt > workflows[j].CreatedAt
	})
	return workflows, nil
}

// Delete removes a workflow by name. Returns error if not found.
func (s *Store) Delete(name string) error {
	if name == "" {
		return fmt.Errorf("workflow name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, name+".json")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("workflow %q not found", name)
		}
		return fmt.Errorf("delete workflow file: %w", err)
	}
	return nil
}

// ListByTrigger returns workflows filtered by trigger type. Used by the cron
// scheduler bootstrap (to register cron-triggered workflows) and by the event
// dispatcher (to enumerate event-triggered workflows for matching).
func (s *Store) ListByTrigger(trigger TriggerType) ([]Workflow, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var filtered []Workflow
	for _, w := range all {
		if w.Trigger == trigger {
			filtered = append(filtered, w)
		}
	}
	if filtered == nil {
		filtered = make([]Workflow, 0)
	}
	return filtered, nil
}

// FindMatchingEventWorkflows returns event-triggered workflows whose EventType
// matches and whose MatchRules all satisfy the supplied context. MatchRules use
// substring matching (same semantics as recipe.Store.FindMatchingEventRecipes
// so a migrated Recipe→Workflow preserves its trigger behaviour). A workflow
// with no MatchRules matches every event of its EventType.
func (s *Store) FindMatchingEventWorkflows(eventType string, context map[string]string) ([]Workflow, error) {
	all, err := s.ListByTrigger(TriggerEvent)
	if err != nil {
		return nil, err
	}
	var matched []Workflow
	for _, w := range all {
		if w.TriggerConfig.EventType != eventType {
			continue
		}
		match := true
		for key, pattern := range w.TriggerConfig.MatchRules {
			val, ok := context[key]
			if !ok {
				match = false
				break
			}
			if !strings.Contains(val, pattern) {
				match = false
				break
			}
		}
		if match {
			matched = append(matched, w)
		}
	}
	if matched == nil {
		matched = make([]Workflow, 0)
	}
	return matched, nil
}

// ValidateWorkflow checks a workflow for structural problems without saving it.
// Returns nil when valid, or a ValidationErrors describing the problems.
// This is the read-only counterpart to Save's built-in validation — use it
// when the frontend wants to show validation feedback before the user clicks Save.
func (s *Store) ValidateWorkflow(wf Workflow) error {
	return Validate(wf)
}

// Duplicate creates a copy of an existing workflow under a new name. The new
// workflow's CreatedAt/UpdatedAt are reset to now, and the name is set to
// newName. If newName is empty, a default is derived by appending " (copy)" to
// the source name. Returns the new workflow's name on success.
func (s *Store) Duplicate(srcName, newName string) (string, error) {
	src, err := s.Load(srcName)
	if err != nil {
		return "", fmt.Errorf("load source workflow: %w", err)
	}
	if newName == "" {
		newName = srcName + " (copy)"
	}
	// Sanitise the new name.
	newName = strings.Map(func(r rune) rune {
		if strings.ContainsRune("/\\:*?\"<>|", r) {
			return '_'
		}
		return r
	}, newName)

	dup := src
	dup.Name = newName
	dup.CreatedAt = 0 // will be set by Save
	dup.UpdatedAt = 0
	if err := s.Save(dup); err != nil {
		return "", fmt.Errorf("save duplicated workflow: %w", err)
	}
	return newName, nil
}

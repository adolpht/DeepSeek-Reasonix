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
	ID        string `json:"id"`                  // Unique node ID (e.g., "node-1")
	Label     string `json:"label"`               // User-facing name (e.g., "Analyze Code")
	Kind      string `json:"kind"`                // "skill" | "prompt" | "tool" | "condition" | "parallel"
	Config    string `json:"config"`              // JSON config for the node (skill name, prompt template, tool args, condition expr)
	Model     string `json:"model,omitempty"`      // Override model for this node (empty = inherit)
	Effort    string `json:"effort,omitempty"`     // Override effort for this node
	PositionX int    `json:"positionX"`            // Canvas X position
	PositionY int    `json:"positionY"`            // Canvas Y position
}

// WorkflowEdge represents a directed connection between two nodes.
type WorkflowEdge struct {
	ID     string `json:"id"`                          // Unique edge ID
	Source string `json:"source"`                      // Source node ID
	Target string `json:"target"`                      // Target node ID
	Label  string `json:"label,omitempty"`             // Edge label (e.g., "yes"/"no" for condition branches)
}

// Workflow represents a complete agent workflow DAG.
type Workflow struct {
	Name        string         `json:"name"`        // Unique identifier (filename-safe)
	Description string         `json:"description"` // User-facing description
	Nodes       []WorkflowNode `json:"nodes"`       // DAG nodes
	Edges       []WorkflowEdge `json:"edges"`       // DAG edges
	CreatedAt   int64          `json:"createdAt"`   // Unix milliseconds
	UpdatedAt   int64          `json:"updatedAt"`   // Unix milliseconds
}

// Store manages workflow persistence in ~/.reasonix/workflows/.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// NewStore creates a workflow store backed by the given directory.
// If dir is empty, it defaults to ~/.reasonix/workflows/.
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".reasonix", "workflows")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create workflows dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Save persists a workflow to <dir>/<name>.json.
// If the workflow already exists, it is overwritten.
func (s *Store) Save(w Workflow) error {
	if w.Name == "" {
		return fmt.Errorf("workflow name is required")
	}
	if strings.ContainsAny(w.Name, "/\\:*?\"<>|") {
		return fmt.Errorf("workflow name contains invalid characters")
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

// Package recipe provides storage and management for automation recipes.
// A recipe is a reusable workflow configuration that can be triggered
// manually, on schedule, or by events (e.g., mail received).
package recipe

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

// TriggerType defines how a recipe is activated.
type TriggerType string

const (
	TriggerManual  TriggerType = "manual"  // User-initiated from UI
	TriggerCron    TriggerType = "cron"    // Scheduled via cron expression
	TriggerEvent   TriggerType = "event"   // Triggered by external event (e.g., mail_received)
)

// TriggerConfig holds configuration for different trigger types.
// For TriggerCron, CronExpr is the cron expression.
// For TriggerEvent, EventType is the event name and MatchRules are filtering rules.
type TriggerConfig struct {
	CronExpr   string            `json:"cronExpr,omitempty"`   // Cron expression for scheduled trigger
	EventType  string            `json:"eventType,omitempty"`  // e.g., "mail_received"
	MatchRules map[string]string `json:"matchRules,omitempty"` // e.g., {"sender": "boss@company.com", "subject": "urgent"}
}

// Recipe represents a reusable automation workflow.
type Recipe struct {
	Name          string        `json:"name"`          // Unique identifier (filename-safe)
	Description   string        `json:"description"`   // User-facing description
	Skill         string        `json:"skill"`         // Skill name to invoke (e.g., "weekly-report")
	Params        string        `json:"params"`        // Parameter template (may contain placeholders)
	Trigger       TriggerType   `json:"trigger"`       // How this recipe is triggered
	TriggerConfig TriggerConfig `json:"triggerConfig"` // Trigger-specific configuration
	CreatedAt     int64         `json:"createdAt"`     // Unix milliseconds
	UpdatedAt     int64         `json:"updatedAt"`     // Unix milliseconds
}

// Store manages recipe persistence in ~/.reasonix/recipes/.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// NewStore creates a recipe store backed by the given directory.
// If dir is empty, it defaults to ~/.reasonix/recipes/.
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".reasonix", "recipes")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create recipes dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Save persists a recipe to <dir>/<name>.json.
// If the recipe already exists, it is overwritten.
func (s *Store) Save(r Recipe) error {
	if r.Name == "" {
		return fmt.Errorf("recipe name is required")
	}
	if strings.ContainsAny(r.Name, "/\\:*?\"<>|") {
		return fmt.Errorf("recipe name contains invalid characters")
	}
	r.UpdatedAt = time.Now().UnixMilli()
	if r.CreatedAt == 0 {
		r.CreatedAt = r.UpdatedAt
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, r.Name+".json")
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal recipe: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write recipe file: %w", err)
	}
	return nil
}

// Load reads a recipe by name. Returns error if not found.
func (s *Store) Load(name string) (Recipe, error) {
	if name == "" {
		return Recipe{}, fmt.Errorf("recipe name is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Recipe{}, fmt.Errorf("recipe %q not found", name)
		}
		return Recipe{}, fmt.Errorf("read recipe file: %w", err)
	}
	var r Recipe
	if err := json.Unmarshal(data, &r); err != nil {
		return Recipe{}, fmt.Errorf("unmarshal recipe: %w", err)
	}
	return r, nil
}

// List returns all recipes, sorted by CreatedAt descending (newest first).
func (s *Store) List() ([]Recipe, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Empty directory
		}
		return nil, fmt.Errorf("read recipes dir: %w", err)
	}

	var recipes []Recipe
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue // Skip unreadable files
		}
		var r Recipe
		if err := json.Unmarshal(data, &r); err != nil {
			continue // Skip malformed files
		}
		recipes = append(recipes, r)
	}

	// Sort by CreatedAt descending
	sort.Slice(recipes, func(i, j int) bool {
		return recipes[i].CreatedAt > recipes[j].CreatedAt
	})
	return recipes, nil
}

// Delete removes a recipe by name. Returns error if not found.
func (s *Store) Delete(name string) error {
	if name == "" {
		return fmt.Errorf("recipe name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, name+".json")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("recipe %q not found", name)
		}
		return fmt.Errorf("delete recipe file: %w", err)
	}
	return nil
}

// ListByTrigger returns recipes filtered by trigger type.
func (s *Store) ListByTrigger(trigger TriggerType) ([]Recipe, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var filtered []Recipe
	for _, r := range all {
		if r.Trigger == trigger {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// FindMatchingEventRecipes returns recipes that match a given event type and context.
// The context map contains event-specific attributes (e.g., "sender", "subject" for mail_received).
func (s *Store) FindMatchingEventRecipes(eventType string, context map[string]string) ([]Recipe, error) {
	all, err := s.ListByTrigger(TriggerEvent)
	if err != nil {
		return nil, err
	}
	var matched []Recipe
	for _, r := range all {
		if r.TriggerConfig.EventType != eventType {
			continue
		}
		// Check if all match rules are satisfied
		match := true
		for key, pattern := range r.TriggerConfig.MatchRules {
			val, ok := context[key]
			if !ok {
				match = false
				break
			}
			// Simple substring match (could be extended to regex)
			if !strings.Contains(val, pattern) {
				match = false
				break
			}
		}
		if match {
			matched = append(matched, r)
		}
	}
	return matched, nil
}
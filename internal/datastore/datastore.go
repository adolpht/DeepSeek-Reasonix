// Package datastore provides persistent storage for scheduled tasks,
// and notifications in the user's ~/.rexion/data/ directory, backed by
// SQLite (pure Go via modernc.org/sqlite).
package datastore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// ScheduledTask represents a recurring task that runs on a cron schedule.
type ScheduledTask struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Cron       string `json:"cron"`
	Skill      string `json:"skill"`
	Parameters string `json:"parameters"` // JSON-encoded parameters
	Enabled    bool   `json:"enabled"`
	LastRun    int64  `json:"lastRun"`   // unix milliseconds, 0 = never
	LastResult string `json:"lastResult"` // result of the most recent execution
	NextRun    int64  `json:"nextRun"`   // unix milliseconds, 0 = not scheduled
	CreatedAt  int64  `json:"createdAt"` // unix milliseconds
}

// TaskExecLog records a single execution of a scheduled task.
type TaskExecLog struct {
	ID        string `json:"id"`
	TaskName  string `json:"taskName"`
	Skill     string `json:"skill"`
	Result    string `json:"result"`
	RunAt     int64  `json:"runAt"`     // unix milliseconds
	Duration  int64  `json:"duration"`  // milliseconds, 0 = unknown
}

// Notification represents an in-app notification.
type Notification struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // task_complete | reminder | error
	Title     string `json:"title"`
	Body      string `json:"body"`
	Read      bool   `json:"read"`
	CreatedAt int64  `json:"createdAt"` // unix milliseconds
}

// Store manages persistent data for scheduled tasks and notifications.
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// DataDir returns the path to ~/.rexion/data/, creating it if needed.
func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("data dir: %w", err)
	}
	dir := filepath.Join(home, ".rexion", "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}
	return dir, nil
}

// Open creates or opens the data store. It runs migrations on first use.
func Open() (*Store, error) {
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, "Rexion.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open datastore: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set wal mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate datastore: %w", err)
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    cron        TEXT NOT NULL,
    skill       TEXT NOT NULL,
    parameters  TEXT NOT NULL DEFAULT '{}',
    enabled     INTEGER NOT NULL DEFAULT 1,
    last_run    INTEGER NOT NULL DEFAULT 0,
    last_result TEXT NOT NULL DEFAULT '',
    next_run    INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS notifications (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    read       INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS task_exec_logs (
    id        TEXT PRIMARY KEY,
    task_name TEXT NOT NULL,
    skill     TEXT NOT NULL,
    result    TEXT NOT NULL DEFAULT '',
    run_at    INTEGER NOT NULL,
    duration  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_task_exec_logs_task_name ON task_exec_logs(task_name);
CREATE INDEX IF NOT EXISTS idx_task_exec_logs_run_at ON task_exec_logs(run_at);
`
	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}
	// Migration: add last_result column if it doesn't exist (upgrade from older schema).
	s.db.Exec("ALTER TABLE scheduled_tasks ADD COLUMN last_result TEXT NOT NULL DEFAULT ''")
	return nil
}

// --- ScheduledTask CRUD ---

func (s *Store) ListScheduledTasks() ([]ScheduledTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query("SELECT id, name, cron, skill, parameters, enabled, last_run, last_result, next_run, created_at FROM scheduled_tasks ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduledTask
	for rows.Next() {
		var t ScheduledTask
		var enabled int
		if err := rows.Scan(&t.ID, &t.Name, &t.Cron, &t.Skill, &t.Parameters, &enabled, &t.LastRun, &t.LastResult, &t.NextRun, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Enabled = enabled != 0
		out = append(out, t)
	}
	if out == nil {
		out = []ScheduledTask{}
	}
	return out, rows.Err()
}

func (s *Store) CreateScheduledTask(t ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"INSERT INTO scheduled_tasks (id, name, cron, skill, parameters, enabled, last_run, last_result, next_run, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		t.ID, t.Name, t.Cron, t.Skill, t.Parameters, boolToInt(t.Enabled), t.LastRun, t.LastResult, t.NextRun, t.CreatedAt,
	)
	return err
}

func (s *Store) UpdateScheduledTask(t ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		"UPDATE scheduled_tasks SET name=?, cron=?, skill=?, parameters=?, enabled=?, last_run=?, last_result=?, next_run=? WHERE id=?",
		t.Name, t.Cron, t.Skill, t.Parameters, boolToInt(t.Enabled), t.LastRun, t.LastResult, t.NextRun, t.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scheduled task %q not found", t.ID)
	}
	return nil
}

func (s *Store) DeleteScheduledTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec("DELETE FROM scheduled_tasks WHERE id=?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scheduled task %q not found", id)
	}
	return nil
}

// --- TaskExecLog CRUD ---

func (s *Store) AddTaskExecLog(l TaskExecLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"INSERT INTO task_exec_logs (id, task_name, skill, result, run_at, duration) VALUES (?, ?, ?, ?, ?, ?)",
		l.ID, l.TaskName, l.Skill, l.Result, l.RunAt, l.Duration,
	)
	if err != nil {
		return err
	}
	// Prune: keep at most 100 logs per task.
	s.db.Exec(`DELETE FROM task_exec_logs WHERE task_name=? AND id NOT IN (
		SELECT id FROM task_exec_logs WHERE task_name=? ORDER BY run_at DESC LIMIT 100
	)`, l.TaskName, l.TaskName)
	return nil
}

func (s *Store) ListTaskExecLogs(taskName string, limit int) ([]TaskExecLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query("SELECT id, task_name, skill, result, run_at, duration FROM task_exec_logs WHERE task_name=? ORDER BY run_at DESC LIMIT ?", taskName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskExecLog
	for rows.Next() {
		var l TaskExecLog
		if err := rows.Scan(&l.ID, &l.TaskName, &l.Skill, &l.Result, &l.RunAt, &l.Duration); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	if out == nil {
		out = []TaskExecLog{}
	}
	return out, rows.Err()
}

// --- Notification CRUD ---

func (s *Store) GetNotifications() ([]Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query("SELECT id, kind, title, body, read, created_at FROM notifications ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		var read int
		if err := rows.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &read, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.Read = read != 0
		out = append(out, n)
	}
	if out == nil {
		out = []Notification{}
	}
	return out, rows.Err()
}

// GetUnreadNotifications returns only unread notifications.
func (s *Store) GetUnreadNotifications() ([]Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query("SELECT id, kind, title, body, read, created_at FROM notifications WHERE read=0 ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		var read int
		if err := rows.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &read, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.Read = read != 0
		out = append(out, n)
	}
	if out == nil {
		out = []Notification{}
	}
	return out, rows.Err()
}

func (s *Store) CreateNotification(n Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"INSERT INTO notifications (id, kind, title, body, read, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		n.ID, n.Kind, n.Title, n.Body, boolToInt(n.Read), n.CreatedAt,
	)
	return err
}

func (s *Store) MarkNotificationRead(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec("UPDATE notifications SET read=1 WHERE id=?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("notification %q not found", id)
	}
	return nil
}

// AddNotification is a convenience that creates a new unread notification
// with an auto-generated ID and the current timestamp.
func (s *Store) AddNotification(kind, title, body string) error {
	return s.CreateNotification(Notification{
		ID:        NewID(),
		Kind:      ValidateNotificationKind(kind),
		Title:     title,
		Body:      body,
		Read:      false,
		CreatedAt: time.Now().UnixMilli(),
	})
}

// --- Helpers ---

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// NewID generates a unique ID for a data entity.
func NewID() string {
	return fmt.Sprintf("%d", time.Now().UnixMilli())
}

// ValidateNotificationKind returns a normalized notification kind or "reminder".
func ValidateNotificationKind(k string) string {
	switch k {
	case "task_complete", "reminder", "error":
		return k
	default:
		return "reminder"
	}
}

// ParseJSONObject validates and returns a JSON object string. Returns "{}" if
// the input is empty or invalid.
func ParseJSONObject(s string) string {
	if s == "" {
		return "{}"
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return "{}"
	}
	out, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(out)
}

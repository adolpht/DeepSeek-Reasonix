package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// todoItem represents a single todo entry in the Reasonix database.
type todoItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	DueDate     string `json:"due_date,omitempty"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
	Source      string `json:"source,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// dbPath returns the path to the Reasonix SQLite database. It checks
// REASONIX_HOME first, then falls back to ~/.reasonix/.
func dbPath() string {
	if home := os.Getenv("REASONIX_HOME"); home != "" {
		return filepath.Join(home, "data.db")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".reasonix", "data.db")
	}
	return filepath.Join(home, ".reasonix", "data.db")
}

// openDB opens (and initialises) the Reasonix SQLite database. It creates the
// todos table if it does not exist.
func openDB() (*sql.DB, error) {
	path := dbPath()

	// Ensure the parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create database directory %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}

	// Create the todos table if it does not exist.
	const createSQL = `
CREATE TABLE IF NOT EXISTS todos (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    description TEXT DEFAULT '',
    due_date    TEXT DEFAULT '',
    priority    TEXT DEFAULT 'medium',
    status      TEXT DEFAULT 'pending',
    source      TEXT DEFAULT 'reasonix',
    created_at  TEXT DEFAULT '',
    updated_at  TEXT DEFAULT ''
);
`
	if _, err := db.Exec(createSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("create todos table: %w", err)
	}

	return db, nil
}

// --- list_todo ---

func runListTodo(args map[string]any) (any, error) {
	status := argStringDefault(args, "status", "")
	limit := argIntDefault(args, "limit", 20)
	if limit < 1 {
		limit = 20
	}

	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	const query = `
SELECT id, title, description, due_date, priority, status, source, created_at, updated_at
FROM todos
WHERE (? = '' OR status = ?)
ORDER BY due_date ASC
LIMIT ?`

	rows, err := db.Query(query, status, status, limit)
	if err != nil {
		return nil, fmt.Errorf("query todos: %w", err)
	}
	defer rows.Close()

	var items []todoItem
	for rows.Next() {
		var item todoItem
		if err := rows.Scan(&item.ID, &item.Title, &item.Description,
			&item.DueDate, &item.Priority, &item.Status,
			&item.Source, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan todo row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate todo rows: %w", err)
	}

	if len(items) == 0 {
		return "No todo items found.", nil
	}

	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal todos: %w", err)
	}
	return string(b), nil
}

// --- add_todo ---

func runAddTodo(args map[string]any) (any, error) {
	title, err := argString(args, "title")
	if err != nil {
		return nil, err
	}
	description := argStringDefault(args, "description", "")
	dueDate := argStringDefault(args, "due_date", "")
	priority := argStringDefault(args, "priority", "medium")

	// Validate priority.
	switch priority {
	case "low", "medium", "high":
		// ok
	default:
		return nil, fmt.Errorf("invalid priority %q; must be \"low\", \"medium\", or \"high\"", priority)
	}

	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	id := fmt.Sprintf("todo_%d", time.Now().UnixNano())
	now := time.Now().Format(time.RFC3339)

	const insertSQL = `
INSERT INTO todos (id, title, description, due_date, priority, status, source, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, 'pending', 'reasonix', ?, ?)`

	if _, err := db.Exec(insertSQL, id, title, description, dueDate, priority, now, now); err != nil {
		return nil, fmt.Errorf("insert todo: %w", err)
	}

	return fmt.Sprintf("Todo %q added (id: %s, priority: %s, status: pending).", title, id, priority), nil
}

// --- update_todo ---

func runUpdateTodo(args map[string]any) (any, error) {
	id, err := argString(args, "id")
	if err != nil {
		return nil, err
	}

	status := argStringDefault(args, "status", "")
	title := argStringDefault(args, "title", "")
	dueDate := argStringDefault(args, "due_date", "")

	// Validate status if provided.
	if status != "" {
		switch status {
		case "pending", "done":
			// ok
		default:
			return nil, fmt.Errorf("invalid status %q; must be \"pending\" or \"done\"", status)
		}
	}

	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Build dynamic UPDATE statement based on provided fields.
	var setClauses []string
	var setArgs []any
	if status != "" {
		setClauses = append(setClauses, "status = ?")
		setArgs = append(setArgs, status)
	}
	if title != "" {
		setClauses = append(setClauses, "title = ?")
		setArgs = append(setArgs, title)
	}
	if dueDate != "" {
		setClauses = append(setClauses, "due_date = ?")
		setArgs = append(setArgs, dueDate)
	}

	if len(setClauses) == 0 {
		return nil, fmt.Errorf("at least one of status, title, or due_date must be provided")
	}

	now := time.Now().Format(time.RFC3339)
	setClauses = append(setClauses, "updated_at = ?")
	setArgs = append(setArgs, now)

	query := "UPDATE todos SET " + setClauses[0]
	for _, clause := range setClauses[1:] {
		query += ", " + clause
	}
	query += " WHERE id = ?"
	setArgs = append(setArgs, id)

	result, err := db.Exec(query, setArgs...)
	if err != nil {
		return nil, fmt.Errorf("update todo: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return nil, fmt.Errorf("todo item with id %q not found", id)
	}

	return fmt.Sprintf("Todo %s updated successfully.", id), nil
}

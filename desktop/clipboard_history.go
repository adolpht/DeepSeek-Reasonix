package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

	type ClipboardEntry struct {
		ID        int64  `json:"id"`
		Kind      string `json:"kind"`
		Content   string `json:"content"`
		Preview   string `json:"preview"`
		CreatedAt int64  `json:"createdAt"`
	}

type ClipboardHistory struct {
	db   *sql.DB
	mu   sync.RWMutex
	path string
}

func NewClipboardHistory(path string) (*ClipboardHistory, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("mkdir clipboard dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	createTable := `
	CREATE TABLE IF NOT EXISTS clipboard_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL,
		content TEXT NOT NULL,
		preview TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_clipboard_created_at ON clipboard_history(created_at DESC);
	`
	if _, err := db.Exec(createTable); err != nil {
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &ClipboardHistory{
		db:   db,
		path: path,
	}, nil
}

func (h *ClipboardHistory) RecordClipboard(kind, content string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if content == "" {
		return nil
	}

	preview := content
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	preview = strings.ReplaceAll(preview, "\n", " ")

	_, err := h.db.Exec(`
		INSERT INTO clipboard_history (kind, content, preview, created_at)
		VALUES (?, ?, ?, ?)
	`, kind, content, preview, time.Now().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert clipboard entry: %w", err)
	}

	return nil
}

func (h *ClipboardHistory) ListClipboard(limit, offset int) ([]ClipboardEntry, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	rows, err := h.db.Query(`
		SELECT id, kind, content, preview, created_at
		FROM clipboard_history
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query clipboard: %w", err)
	}
	defer rows.Close()

	var entries []ClipboardEntry
	for rows.Next() {
		var entry ClipboardEntry
		var createdAt string
		if err := rows.Scan(&entry.ID, &entry.Kind, &entry.Content, &entry.Preview, &createdAt); err != nil {
			return nil, fmt.Errorf("scan clipboard entry: %w", err)
		}
		t, _ := time.Parse(time.RFC3339, createdAt)
		entry.CreatedAt = t.Unix()
		entries = append(entries, entry)
	}

	return entries, nil
}

func (h *ClipboardHistory) SearchClipboard(query string) ([]ClipboardEntry, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	rows, err := h.db.Query(`
		SELECT id, kind, content, preview, created_at
		FROM clipboard_history
		WHERE content LIKE ? OR preview LIKE ?
		ORDER BY created_at DESC
		LIMIT 50
	`, "%"+query+"%", "%"+query+"%")
	if err != nil {
		return nil, fmt.Errorf("search clipboard: %w", err)
	}
	defer rows.Close()

	var entries []ClipboardEntry
	for rows.Next() {
		var entry ClipboardEntry
		var createdAt string
		if err := rows.Scan(&entry.ID, &entry.Kind, &entry.Content, &entry.Preview, &createdAt); err != nil {
			return nil, fmt.Errorf("scan clipboard entry: %w", err)
		}
		t, _ := time.Parse(time.RFC3339, createdAt)
		entry.CreatedAt = t.Unix()
		entries = append(entries, entry)
	}

	return entries, nil
}

func (h *ClipboardHistory) ClearOldClipboard(days int) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	_, err := h.db.Exec(`
		DELETE FROM clipboard_history WHERE created_at < ?
	`, cutoff.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("clear old clipboard: %w", err)
	}

	return nil
}

func (h *ClipboardHistory) EnforceMaxEntries(max int) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var count int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM clipboard_history`).Scan(&count); err != nil {
		return fmt.Errorf("count clipboard entries: %w", err)
	}

	if count <= max {
		return nil
	}

	rows, err := h.db.Query(`
		SELECT id FROM clipboard_history
		ORDER BY created_at ASC
		LIMIT ?
	`, count-max)
	if err != nil {
		return fmt.Errorf("query old entries: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan old entry id: %w", err)
		}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	_, err = h.db.Exec(`DELETE FROM clipboard_history WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return fmt.Errorf("delete old entries: %w", err)
	}

	return nil
}

func (h *ClipboardHistory) Close() error {
	return h.db.Close()
}
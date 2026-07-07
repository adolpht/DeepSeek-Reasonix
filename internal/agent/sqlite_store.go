package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"rexion/internal/provider"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store backed by a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a SQLite database at path and initialises
// the schema. WAL mode and a busy timeout are set for safe concurrent access.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Enable WAL for concurrent readers + one writer, and set a busy timeout so
	// contending writers wait instead of failing immediately.
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
	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *SQLiteStore) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS threads (
    id          TEXT PRIMARY KEY,
    title       TEXT,
    model       TEXT NOT NULL,
    workspace   TEXT,
    scope       TEXT DEFAULT 'project',
    parent_id   TEXT,
    fork_turn   INTEGER,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    FOREIGN KEY (parent_id) REFERENCES threads(id)
);

CREATE TABLE IF NOT EXISTS turns (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    thread_id   TEXT NOT NULL,
    turn_num    INTEGER NOT NULL,
    created_at  DATETIME NOT NULL,
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE,
    UNIQUE(thread_id, turn_num)
);

CREATE TABLE IF NOT EXISTS items (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_id     INTEGER NOT NULL,
    item_order  INTEGER NOT NULL,
    kind        TEXT NOT NULL,
    content     TEXT,
    tool_name   TEXT,
    tool_args   TEXT,
    tool_output TEXT,
    tool_error  TEXT,
    reasoning   TEXT,
    duration_ms INTEGER,
    created_at  DATETIME NOT NULL,
    FOREIGN KEY (turn_id) REFERENCES turns(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS compaction_archives (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    thread_id   TEXT NOT NULL,
    turn_start  INTEGER NOT NULL,
    turn_end    INTEGER NOT NULL,
    summary     TEXT NOT NULL,
    archive_data TEXT NOT NULL,
    created_at  DATETIME NOT NULL,
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS messages (
    thread_id   TEXT PRIMARY KEY,
    data        TEXT NOT NULL,
    updated_at  DATETIME NOT NULL,
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_threads_workspace ON threads(workspace);
CREATE INDEX IF NOT EXISTS idx_threads_updated ON threads(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_items_turn ON items(turn_id);
CREATE INDEX IF NOT EXISTS idx_items_kind ON items(kind);
`
	_, err := s.db.Exec(schema)
	return err
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// CreateThread inserts a new thread record.
func (s *SQLiteStore) CreateThread(ctx context.Context, t *Thread) error {
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO threads (id, title, model, workspace, scope, parent_id, fork_turn, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Title, t.Model, t.Workspace, t.Scope, nullString(t.ParentID), nullInt(t.ForkTurn), t.CreatedAt, t.UpdatedAt,
	)
	return err
}

// GetThread returns a thread by ID.
func (s *SQLiteStore) GetThread(ctx context.Context, id string) (*Thread, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, model, workspace, scope, parent_id, fork_turn, created_at, updated_at
		 FROM threads WHERE id = ?`, id)
	var t Thread
	var parentID sql.NullString
	var forkTurn sql.NullInt64
	if err := row.Scan(&t.ID, &t.Title, &t.Model, &t.Workspace, &t.Scope, &parentID, &forkTurn, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	t.ParentID = parentID.String
	if forkTurn.Valid {
		t.ForkTurn = int(forkTurn.Int64)
	}
	return &t, nil
}

// ListThreads returns threads matching the given options.
func (s *SQLiteStore) ListThreads(ctx context.Context, opts ListOpts) ([]*Thread, error) {
	query := `SELECT id, title, model, workspace, scope, parent_id, fork_turn, created_at, updated_at
	          FROM threads WHERE 1=1`
	var args []any
	if opts.Workspace != "" {
		query += " AND workspace = ?"
		args = append(args, opts.Workspace)
	}
	if opts.Scope != "" {
		query += " AND scope = ?"
		args = append(args, opts.Scope)
	}
	query += " ORDER BY updated_at DESC"
	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Thread
	for rows.Next() {
		var t Thread
		var parentID sql.NullString
		var forkTurn sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Title, &t.Model, &t.Workspace, &t.Scope, &parentID, &forkTurn, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.ParentID = parentID.String
		if forkTurn.Valid {
			t.ForkTurn = int(forkTurn.Int64)
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// UpdateThread updates an existing thread record.
func (s *SQLiteStore) UpdateThread(ctx context.Context, t *Thread) error {
	t.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE threads SET title=?, model=?, workspace=?, scope=?, parent_id=?, fork_turn=?, updated_at=?
		 WHERE id=?`,
		t.Title, t.Model, t.Workspace, t.Scope, nullString(t.ParentID), nullInt(t.ForkTurn), t.UpdatedAt, t.ID,
	)
	return err
}

// DeleteThread removes a thread and all associated data (cascading).
func (s *SQLiteStore) DeleteThread(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM threads WHERE id=?`, id)
	return err
}

// writeStructuredTurnsItems projects msgs into the turns/items tables for
// offline execution-flow tracing. A new turn begins at each user message after
// the first; assistant and tool messages accumulate into the current turn.
// turn_num starts at startTurn and increments per user turn. The messages blob
// remains the runtime source of truth — this projection serves offline analysis
// (replay, per-turn inspection). provider.Message carries no tool_error or
// duration, so those columns stay empty here.
func writeStructuredTurnsItems(ctx context.Context, tx *sql.Tx, threadID string, msgs []provider.Message, startTurn int) error {
	turnNum := startTurn
	turnID := 0
	turnInserted := false
	itemOrder := 0
	now := time.Now().UTC()

	ensureTurn := func() error {
		if turnInserted {
			return nil
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO turns (thread_id, turn_num, created_at) VALUES (?, ?, ?)`,
			threadID, turnNum, now)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		turnID = int(id)
		turnInserted = true
		itemOrder = 0
		return nil
	}

	addItem := func(kind, content, toolName, toolArgs, toolOutput, reasoning string) error {
		if err := ensureTurn(); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO items (turn_id, item_order, kind, content, tool_name, tool_args, tool_output, tool_error, reasoning, duration_ms, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, NULL, ?)`,
			turnID, itemOrder, kind, content, toolName, toolArgs, toolOutput, reasoning, now)
		if err != nil {
			return err
		}
		itemOrder++
		return nil
	}

	for _, m := range msgs {
		switch m.Role {
		case provider.RoleUser:
			if turnInserted {
				turnNum++
				turnInserted = false
			}
			if err := addItem("user", m.Content, "", "", "", ""); err != nil {
				return err
			}
		case provider.RoleAssistant:
			if err := addItem("assistant", m.Content, "", "", "", m.ReasoningContent); err != nil {
				return err
			}
			for _, tc := range m.ToolCalls {
				if err := addItem("tool_call", "", tc.Name, tc.Arguments, "", ""); err != nil {
					return err
				}
			}
		case provider.RoleTool:
			if err := addItem("tool_result", m.Content, m.Name, "", m.Content, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

// AppendMessages appends messages to the existing message list for a thread.
func (s *SQLiteStore) AppendMessages(ctx context.Context, threadID string, msgs []provider.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existing string
	if err := tx.QueryRowContext(ctx, `SELECT data FROM messages WHERE thread_id=?`, threadID).Scan(&existing); err != nil {
		if err == sql.ErrNoRows {
			existing = "[]"
		} else {
			return err
		}
	}

	var list []provider.Message
	if err := json.Unmarshal([]byte(existing), &list); err != nil {
		return fmt.Errorf("unmarshal existing messages: %w", err)
	}
	list = append(list, msgs...)
	data, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("marshal messages: %w", err)
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO messages (thread_id, data, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(thread_id) DO UPDATE SET data=excluded.data, updated_at=excluded.updated_at`,
		threadID, string(data), now,
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE threads SET updated_at=? WHERE id=?`, now, threadID); err != nil {
		return err
	}

	// Project the appended messages into structured turns/items for offline
	// tracing. turn_num continues from the highest existing turn for this thread.
	var maxTurn int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(turn_num),0) FROM turns WHERE thread_id=?`, threadID).Scan(&maxTurn); err != nil {
		return err
	}
	if err := writeStructuredTurnsItems(ctx, tx, threadID, msgs, maxTurn+1); err != nil {
		return err
	}

	return tx.Commit()
}

// GetMessages returns all messages for a thread.
func (s *SQLiteStore) GetMessages(ctx context.Context, threadID string) ([]provider.Message, error) {
	var data string
	if err := s.db.QueryRowContext(ctx, `SELECT data FROM messages WHERE thread_id=?`, threadID).Scan(&data); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var msgs []provider.Message
	if err := json.Unmarshal([]byte(data), &msgs); err != nil {
		return nil, fmt.Errorf("unmarshal messages: %w", err)
	}
	return msgs, nil
}

// ReplaceMessages replaces the full message list for a thread.
func (s *SQLiteStore) ReplaceMessages(ctx context.Context, threadID string, msgs []provider.Message) error {
	data, err := json.Marshal(msgs)
	if err != nil {
		return fmt.Errorf("marshal messages: %w", err)
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO messages (thread_id, data, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(thread_id) DO UPDATE SET data=excluded.data, updated_at=excluded.updated_at`,
		threadID, string(data), now,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET updated_at=? WHERE id=?`, now, threadID); err != nil {
		return err
	}
	// Rebuild structured turns/items from the new list. Deleting turns cascades
	// to items (FK ON DELETE CASCADE), so the projection is rebuilt wholesale.
	if _, err := tx.ExecContext(ctx, `DELETE FROM turns WHERE thread_id=?`, threadID); err != nil {
		return err
	}
	if err := writeStructuredTurnsItems(ctx, tx, threadID, msgs, 1); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveArchive stores a compaction archive.
func (s *SQLiteStore) SaveArchive(ctx context.Context, a *Archive) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO compaction_archives (thread_id, turn_start, turn_end, summary, archive_data, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		a.ThreadID, a.TurnStart, a.TurnEnd, a.Summary, a.Data, a.CreatedAt,
	)
	return err
}

// LoadArchive returns the most recent archive for a thread starting at fromTurn.
func (s *SQLiteStore) LoadArchive(ctx context.Context, threadID string, fromTurn int) (*Archive, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT thread_id, turn_start, turn_end, summary, archive_data, created_at
		 FROM compaction_archives
		 WHERE thread_id=? AND turn_start<=?
		 ORDER BY created_at DESC LIMIT 1`,
		threadID, fromTurn,
	)
	var a Archive
	if err := row.Scan(&a.ThreadID, &a.TurnStart, &a.TurnEnd, &a.Summary, &a.Data, &a.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullInt(n int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(n), Valid: n != 0}
}

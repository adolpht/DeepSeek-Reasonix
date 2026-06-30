package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MigrateJSONLToSQLite migrates all JSONL sessions in dir into the SQLite store.
// On success each .jsonl file is renamed to .jsonl.migrated so the migration is
// not repeated. BranchMeta sidecars are preserved but the primary data lives in
// SQLite from now on.
func MigrateJSONLToSQLite(ctx context.Context, dir string, sqliteStore *SQLiteStore) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("migrate: read dir: %w", err)
	}

	var migrated, skipped, failed int
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		// Skip already-migrated files
		if strings.HasSuffix(e.Name(), ".migrated") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := migrateOne(ctx, path, sqliteStore); err != nil {
			slog.Warn("migrate: failed", "path", path, "err", err)
			failed++
			continue
		}
		// Rename the original .jsonl to .jsonl.migrated
		migratedPath := path + ".migrated"
		if err := os.Rename(path, migratedPath); err != nil {
			slog.Warn("migrate: rename failed", "path", path, "err", err)
			failed++
			continue
		}
		// Also rename the sidecar .meta file if it exists
		metaPath := BranchMetaPath(path)
		if _, statErr := os.Stat(metaPath); statErr == nil {
			_ = os.Rename(metaPath, metaPath+".migrated")
		}
		migrated++
	}
	if migrated > 0 || failed > 0 {
		slog.Info("migrate: done", "migrated", migrated, "skipped", skipped, "failed", failed)
	}
	if failed > 0 {
		return fmt.Errorf("migrate: %d sessions failed", failed)
	}
	return nil
}

func migrateOne(ctx context.Context, path string, store *SQLiteStore) error {
	id := BranchID(path)

	// Load messages from JSONL
	sess, err := LoadSession(path)
	if err != nil {
		return fmt.Errorf("load jsonl: %w", err)
	}
	if !sess.HasContent() {
		return nil // skip empty sessions
	}

	// Load BranchMeta for thread metadata
	meta, ok, metaErr := LoadBranchMeta(path)
	if metaErr != nil {
		return fmt.Errorf("load meta: %w", metaErr)
	}
	if !ok {
		info, statErr := os.Stat(path)
		when := time.Now().UTC()
		if statErr == nil {
			when = info.ModTime().UTC()
		}
		meta = BranchMeta{ID: id, CreatedAt: when, UpdatedAt: when}
	}
	if meta.ID == "" {
		meta.ID = id
	}

	// Extract title from first user message
	title := meta.TopicTitle
	if title == "" {
		for _, m := range sess.Messages {
			if m.Role == "user" {
				s := strings.TrimSpace(m.Content)
				if r := []rune(s); len(r) > 80 {
					s = string(r[:77]) + "…"
				}
				title = s
				break
			}
		}
	}

	// Derive model from the filename pattern
	model := ""
	if parts := strings.SplitN(id, "-", 2); len(parts) >= 2 {
		model = parts[1]
	}

	thread := &Thread{
		ID:        meta.ID,
		Title:     title,
		Model:     model,
		Workspace: meta.WorkspaceRoot,
		Scope:     meta.DefaultScope(),
		ParentID:  meta.ParentID,
		ForkTurn:  meta.ForkTurn,
		CreatedAt: meta.CreatedAt,
		UpdatedAt: meta.UpdatedAt,
	}

	// Check if thread already exists (avoid duplicate on partial migration)
	existing, err := store.GetThread(ctx, thread.ID)
	if err != nil {
		return fmt.Errorf("check existing: %w", err)
	}
	if existing != nil {
		return nil // already migrated
	}

	if err := store.CreateThread(ctx, thread); err != nil {
		return fmt.Errorf("create thread: %w", err)
	}
	if err := store.ReplaceMessages(ctx, thread.ID, sess.Messages); err != nil {
		return fmt.Errorf("write messages: %w", err)
	}
	return nil
}

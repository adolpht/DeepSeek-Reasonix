package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/provider"
)

// JSONLStore implements Store backed by the existing JSONL file format.
type JSONLStore struct {
	dir string // session directory
}

// NewJSONLStore creates a JSONL-backed store rooted at dir.
func NewJSONLStore(dir string) (*JSONLStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("jsonl store: empty directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("jsonl store: create dir: %w", err)
	}
	return &JSONLStore{dir: dir}, nil
}

func (s *JSONLStore) sessionPath(id string) string {
	return filepath.Join(s.dir, id+".jsonl")
}

func (s *JSONLStore) archiveDir() string {
	return filepath.Join(s.dir, ".archive")
}

// Close is a no-op for JSONL (no resources to release).
func (s *JSONLStore) Close() error { return nil }

// CreateThread creates a new session file and writes its BranchMeta sidecar.
func (s *JSONLStore) CreateThread(ctx context.Context, t *Thread) error {
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	path := s.sessionPath(t.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Create empty JSONL file
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		return err
	}
	// Write sidecar meta
	meta := BranchMeta{
		ID:            t.ID,
		ParentID:      t.ParentID,
		ForkTurn:      t.ForkTurn,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
		Scope:         t.Scope,
		WorkspaceRoot: t.Workspace,
	}
	return SaveBranchMeta(path, meta)
}

// GetThread reads thread metadata from the BranchMeta sidecar.
func (s *JSONLStore) GetThread(ctx context.Context, id string) (*Thread, error) {
	path := s.sessionPath(id)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil {
		return nil, err
	}
	if !ok {
		info, statErr := os.Stat(path)
		when := time.Now().UTC()
		if statErr == nil {
			when = info.ModTime().UTC()
		}
		meta = BranchMeta{ID: id, CreatedAt: when, UpdatedAt: when}
	}
	t := &Thread{
		ID:        meta.ID,
		ParentID:  meta.ParentID,
		ForkTurn:  meta.ForkTurn,
		CreatedAt: meta.CreatedAt,
		UpdatedAt: meta.UpdatedAt,
		Scope:     meta.DefaultScope(),
		Workspace: meta.WorkspaceRoot,
		Title:     meta.TopicTitle,
	}
	return t, nil
}

// ListThreads returns threads from JSONL files in the session directory.
func (s *JSONLStore) ListThreads(ctx context.Context, opts ListOpts) ([]*Thread, error) {
	sessions, err := ListSessions(s.dir)
	if err != nil {
		return nil, err
	}
	var out []*Thread
	for _, si := range sessions {
		t := &Thread{
			ID:        BranchID(si.Path),
			Title:     si.Preview,
			Workspace: si.WorkspaceRoot,
			Scope:     si.Scope,
			CreatedAt: si.CreatedAt,
			UpdatedAt: si.LastActivityAt,
		}
		if opts.Workspace != "" && t.Workspace != opts.Workspace {
			continue
		}
		if opts.Scope != "" && t.Scope != opts.Scope {
			continue
		}
		out = append(out, t)
	}
	// Apply offset/limit
	if opts.Offset > 0 {
		if opts.Offset >= len(out) {
			return nil, nil
		}
		out = out[opts.Offset:]
	}
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

// UpdateThread updates the BranchMeta sidecar for a thread.
func (s *JSONLStore) UpdateThread(ctx context.Context, t *Thread) error {
	path := s.sessionPath(t.ID)
	meta, _, err := LoadBranchMeta(path)
	if err != nil {
		return err
	}
	meta.ID = t.ID
	meta.ParentID = t.ParentID
	meta.ForkTurn = t.ForkTurn
	meta.Scope = t.Scope
	meta.WorkspaceRoot = t.Workspace
	meta.TopicTitle = t.Title
	meta.UpdatedAt = time.Now().UTC()
	return SaveBranchMeta(path, meta)
}

// DeleteThread removes the JSONL file and its sidecar.
func (s *JSONLStore) DeleteThread(ctx context.Context, id string) error {
	path := s.sessionPath(id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	metaPath := BranchMetaPath(path)
	_ = os.Remove(metaPath)
	return nil
}

// AppendMessages appends messages to the JSONL file.
func (s *JSONLStore) AppendMessages(ctx context.Context, threadID string, msgs []provider.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	path := s.sessionPath(threadID)
	// Load existing, append, rewrite
	sess, err := LoadSession(path)
	if err != nil {
		return fmt.Errorf("load session for append: %w", err)
	}
	sess.Messages = append(sess.Messages, msgs...)
	return sess.Save(path)
}

// GetMessages reads all messages from the JSONL file.
func (s *JSONLStore) GetMessages(ctx context.Context, threadID string) ([]provider.Message, error) {
	path := s.sessionPath(threadID)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sess, err := LoadSession(path)
	if err != nil {
		return nil, err
	}
	return sess.Messages, nil
}

// ReplaceMessages rewrites the entire JSONL file with the given messages.
func (s *JSONLStore) ReplaceMessages(ctx context.Context, threadID string, msgs []provider.Message) error {
	path := s.sessionPath(threadID)
	sess := &Session{Messages: msgs}
	return sess.Save(path)
}

// SaveArchive writes archive data to a file in the archive directory.
func (s *JSONLStore) SaveArchive(ctx context.Context, a *Archive) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	dir := s.archiveDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%d-%d-%s.jsonl",
		a.ThreadID, a.TurnStart, a.TurnEnd,
		a.CreatedAt.Format("20060102-150405"))
	path := filepath.Join(dir, name)
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadArchive reads the most recent archive for a thread starting at fromTurn.
func (s *JSONLStore) LoadArchive(ctx context.Context, threadID string, fromTurn int) (*Archive, error) {
	dir := s.archiveDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	prefix := threadID + "-"
	var best *Archive
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var a Archive
		if err := json.Unmarshal(raw, &a); err != nil {
			continue
		}
		if a.TurnStart <= fromTurn {
			if best == nil || a.CreatedAt.After(best.CreatedAt) {
				best = &a
			}
		}
	}
	return best, nil
}

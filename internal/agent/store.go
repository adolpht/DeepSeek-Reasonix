package agent

import (
	"context"
	"time"

	"rexion/internal/provider"
)

// Thread represents a conversation thread (session).
type Thread struct {
	ID        string    // UUID or JSONL filename stem
	Title     string    // First user message summary
	Model     string    // Model used
	Workspace string    // Working directory
	Scope     string    // "project" | "global"
	ParentID  string    // Branch source thread
	ForkTurn  int       // Branch point turn number
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ListOpts controls thread listing.
type ListOpts struct {
	Workspace string
	Scope     string
	Limit     int
	Offset    int
}

// Archive stores compaction archive data.
type Archive struct {
	ThreadID  string
	TurnStart int
	TurnEnd   int
	Summary   string
	Data      string // Original messages JSON
	CreatedAt time.Time
}

// Store is the persistence backend for sessions.
type Store interface {
	// Thread operations
	CreateThread(ctx context.Context, t *Thread) error
	GetThread(ctx context.Context, id string) (*Thread, error)
	ListThreads(ctx context.Context, opts ListOpts) ([]*Thread, error)
	UpdateThread(ctx context.Context, t *Thread) error
	DeleteThread(ctx context.Context, id string) error

	// Message operations
	AppendMessages(ctx context.Context, threadID string, msgs []provider.Message) error
	GetMessages(ctx context.Context, threadID string) ([]provider.Message, error)
	ReplaceMessages(ctx context.Context, threadID string, msgs []provider.Message) error

	// Archive operations
	SaveArchive(ctx context.Context, a *Archive) error
	LoadArchive(ctx context.Context, threadID string, fromTurn int) (*Archive, error)

	// Lifecycle
	Close() error
}

// StoreConfig configures the session store backend.
type StoreConfig struct {
	Backend     string // "sqlite" | "jsonl"
	Path        string // SQLite database path
	Dir         string // JSONL directory
	AutoMigrate bool   // Auto-migrate from JSONL to SQLite
}

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NewStore creates the appropriate Store backend based on StoreConfig.
func NewStore(cfg StoreConfig) (Store, error) {
	switch cfg.Backend {
	case "sqlite":
		return NewSQLiteStore(cfg.Path)
	case "jsonl":
		return NewJSONLStore(cfg.Dir)
	default:
		return autoDetectStore(cfg)
	}
}

// autoDetectStore picks the backend by inspecting the filesystem:
//   - If a SQLite database already exists at cfg.Path, use SQLiteStore
//   - If only JSONL files exist in cfg.Dir, use JSONLStore (and optionally auto-migrate)
//   - Otherwise, create a new SQLiteStore
func autoDetectStore(cfg StoreConfig) (Store, error) {
	// Normalise paths
	sqlitePath := cfg.Path
	jsonlDir := cfg.Dir

	// Prefer SQLite if the database already exists
	if sqlitePath != "" {
		if _, err := os.Stat(sqlitePath); err == nil {
			store, err := NewSQLiteStore(sqlitePath)
			if err != nil {
				return nil, err
			}
			if cfg.AutoMigrate {
				if mErr := MigrateJSONLToSQLite(nil, jsonlDir, store); mErr != nil {
					_ = store.Close()
					return nil, fmt.Errorf("auto-migrate: %w", mErr)
				}
			}
			return store, nil
		}
	}

	// Check for JSONL files
	if jsonlDir != "" {
		hasJSONL := false
		entries, err := os.ReadDir(jsonlDir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") && !strings.HasSuffix(e.Name(), ".migrated") {
					hasJSONL = true
					break
				}
			}
		}
		if hasJSONL {
			if cfg.AutoMigrate && sqlitePath != "" {
				// Migrate JSONL → SQLite
				store, err := NewSQLiteStore(sqlitePath)
				if err != nil {
					return nil, err
				}
				if mErr := MigrateJSONLToSQLite(nil, jsonlDir, store); mErr != nil {
					_ = store.Close()
					return nil, fmt.Errorf("auto-migrate: %w", mErr)
				}
				return store, nil
			}
			// Fall back to JSONL backend
			return NewJSONLStore(jsonlDir)
		}
	}

	// Default: create a new SQLite store
	if sqlitePath != "" {
		return NewSQLiteStore(sqlitePath)
	}

	// Last resort: use JSONL directory
	if jsonlDir != "" {
		return NewJSONLStore(jsonlDir)
	}

	return nil, fmt.Errorf("store: no backend configured (set store.backend or store.path)")
}

// DefaultSQLitePath returns the default SQLite database path under the given
// session directory.
func DefaultSQLitePath(sessionDir string) string {
	return filepath.Join(sessionDir, "sessions.db")
}

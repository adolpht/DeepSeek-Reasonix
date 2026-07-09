// Package branchlock provides optimistic-lock style conflict detection for
// agent file writes. Before the agent writes to a file, it calls Check to
// record the file's current git HEAD hash. On subsequent writes, Verify
// compares the hash: if the file was modified externally (by the user or
// another process) since the agent last read or wrote it, a conflict is
// reported and the write is blocked.
//
// The mechanism is:
//  1. Agent reads a file → Lock.Record(path) captures the current git blob hash.
//  2. Agent writes to the file → Lock.Verify(path) checks the hash hasn't changed.
//  3. If changed, the write is blocked with a conflict message.
//  4. After a successful write, Lock.Record(path) updates the hash.
package branchlock

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Lock tracks file states for conflict detection. It is safe for concurrent use.
type Lock struct {
	mu     sync.Mutex
_hashes map[string]string // path → git blob hash (or "untracked" for non-git files)
	root   string           // git root directory
	enabled bool
}

// New creates a Lock for the given git root directory. If git is not available
// or the directory is not a git repo, the Lock is disabled (all operations
// are no-ops).
func New(root string) *Lock {
	l := &Lock{
		_hashes: make(map[string]string),
		root:    root,
		enabled: root != "" && isGitRepo(root),
	}
	return l
}

// IsEnabled reports whether conflict detection is active.
func (l *Lock) IsEnabled() bool {
	return l != nil && l.enabled
}

// Record captures the current state of a file. Called after a read or write
// to establish a baseline for future conflict checks.
func (l *Lock) Record(path string) {
	if !l.IsEnabled() {
		return
	}
	abs := l.absPath(path)
	hash := l.gitHash(abs)
	l.mu.Lock()
	l._hashes[abs] = hash
	l.mu.Unlock()
}

// Verify checks whether a file has changed since it was last recorded.
// Returns nil if the file is unchanged (or was never recorded, or the lock
// is disabled). Returns a Conflict error if the file's current hash differs
// from the recorded hash.
func (l *Lock) Verify(path string) error {
	if !l.IsEnabled() {
		return nil
	}
	abs := l.absPath(path)
	l.mu.Lock()
	oldHash, ok := l._hashes[abs]
	l.mu.Unlock()
	if !ok {
		// File was never recorded — no baseline to check against.
		return nil
	}
	currentHash := l.gitHash(abs)
	if currentHash != oldHash {
		return &Conflict{
			Path:    path,
			OldHash: oldHash,
			NewHash: currentHash,
		}
	}
	return nil
}

// Forget removes the recorded state for a file. Called when the agent is done
// with a file and no longer needs conflict protection.
func (l *Lock) Forget(path string) {
	if !l.IsEnabled() {
		return
	}
	abs := l.absPath(path)
	l.mu.Lock()
	delete(l._hashes, abs)
	l.mu.Unlock()
}

// Reset clears all recorded states.
func (l *Lock) Reset() {
	if !l.IsEnabled() {
		return
	}
	l.mu.Lock()
	l._hashes = make(map[string]string)
	l.mu.Unlock()
}

// Conflict describes a file that was modified externally since the agent last
// accessed it.
type Conflict struct {
	Path    string
	OldHash string
	NewHash string
}

func (c *Conflict) Error() string {
	return fmt.Sprintf("file %q was modified externally since the agent last read it (blob %s → %s); re-read the file before writing to avoid clobbering external changes", c.Path, c.OldHash, c.NewHash)
}

// absPath converts a path to absolute, relative to the git root.
func (l *Lock) absPath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(l.root, path))
}

// gitHash returns the git blob hash for a file, or "untracked" if the file
// is not tracked by git, or "missing" if the file doesn't exist.
func (l *Lock) gitHash(absPath string) string {
	rel, err := filepath.Rel(l.root, absPath)
	if err != nil {
		return "unknown"
	}
	// Use git hash-object to get the blob hash regardless of tracking status.
	cmd := exec.Command("git", "-C", l.root, "hash-object", "--", rel)
	out, err := cmd.Output()
	if err != nil {
		// File might not exist yet.
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "No such file") {
			return "missing"
		}
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// isGitRepo checks whether the directory is inside a git repository.
func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

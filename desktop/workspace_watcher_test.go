package main

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls cond until it returns true or the deadline passes. Returns
// false on timeout so callers can produce a descriptive failure.
func waitFor(t *testing.T, what string, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// newTestWatcher builds a workspaceWatcher that counts emits, plus a cleanup
// that closes it. Tests must not leak watchers (they hold OS resources).
func newTestWatcher(t *testing.T) (*workspaceWatcher, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	ww, err := newWorkspaceWatcher(func() { n.Add(1) })
	if err != nil {
		t.Fatalf("newWorkspaceWatcher: %v", err)
	}
	t.Cleanup(ww.close)
	return ww, &n
}

func TestWorkspaceWatcherEmitsOnFileCreateRemove(t *testing.T) {
	dir := t.TempDir()
	ww, emits := newTestWatcher(t)
	ww.watch(dir)

	// Create a file at the root.
	path := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, "emit after create", func() bool { return emits.Load() >= 1 }) {
		t.Fatal("no emit after creating a file")
	}

	// Remove it.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, "emit after remove", func() bool { return emits.Load() >= 2 }) {
		t.Fatal("no emit after removing a file")
	}
}

func TestWorkspaceWatcherFollowsNewSubdirectory(t *testing.T) {
	dir := t.TempDir()
	ww, emits := newTestWatcher(t)
	ww.watch(dir)
	emits.Store(0) // ignore any events from watch() itself

	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// A file created inside the new subdirectory must trigger an emit: the
	// watcher adds newly created dirs on Create events (fsnotify v1 has no
	// recursive watch).
	if err := os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, "emit for file in new subdir", func() bool { return emits.Load() >= 1 }) {
		t.Fatal("no emit for a file created inside a newly created subdirectory")
	}
}

func TestWorkspaceWatcherReRoots(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	ww, emits := newTestWatcher(t)
	ww.watch(dirA)
	emits.Store(0)

	// Re-point at a different root; a change in the old root must no longer
	// emit, while a change in the new root must.
	ww.watch(dirB)
	if err := os.WriteFile(filepath.Join(dirA, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Negative assertion: give the debounce window (100ms) plus margin to
	// prove the old root's events no longer emit.
	time.Sleep(400 * time.Millisecond)
	if emits.Load() != 0 {
		t.Fatal("emit fired for change in the old watched root after re-rooting")
	}
	if err := os.WriteFile(filepath.Join(dirB, "b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, "emit from new root", func() bool { return emits.Load() >= 1 }) {
		t.Fatal("no emit for change in the new root after re-rooting")
	}
}

func TestWorkspaceWatcherWatchIdempotent(t *testing.T) {
	dir := t.TempDir()
	ww, _ := newTestWatcher(t)
	ww.watch(dir)
	before := len(ww.watched)
	ww.watch(dir) // same root — must be a no-op
	if got := len(ww.watched); got != before {
		t.Fatalf("re-watching same root changed watch count: %d -> %d", before, got)
	}
}

func TestWorkspaceWatcherSkipsNoiseDirs(t *testing.T) {
	dir := t.TempDir()
	ww, _ := newTestWatcher(t)
	if err := os.Mkdir(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	ww.watch(dir)
	for p := range ww.watched {
		if filepath.Base(p) == "node_modules" {
			t.Fatalf("noise dir node_modules was watched: %s", p)
		}
	}
}

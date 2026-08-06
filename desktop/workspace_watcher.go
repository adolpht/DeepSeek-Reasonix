package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// workspaceWatcher watches the active tab's workspace root with fsnotify and
// calls emit on every filesystem change. It exists to catch changes that do NOT
// flow through the agent tool stream (e.g. files added/removed in the OS file
// manager, or by another program) so the frontend file tree auto-refreshes.
//
// Design notes:
//   - fsnotify v1 has no recursive watch; we add every directory under the root
//     ourselves (addTree) and dynamically add newly created subdirectories from
//     Create events.
//   - Only the *active* tab's workspace root is watched (see syncWorkspaceWatcher).
//     Switching tabs re-points the watcher at the new root.
//   - Emits are debounced: rapid bursts (e.g. `npm install`) coalesce into a
//     single emit. The frontend additionally debounces at 200ms.
//   - Known boundary (deliberate): network drives / some NAS file systems do
//     not deliver fsnotify events. If silent stalls are ever observed there,
//     add a low-frequency root-level polling fallback (e.g. 10s) — none is
//     implemented today.
type workspaceWatcher struct {
	mu      sync.Mutex
	w       *fsnotify.Watcher
	root    string            // currently watched root ("" = not watching)
	watched map[string]bool   // directories added to the OS watcher
	emit    func()            // called (debounced) when a change is detected
	done    chan struct{}     // closed by close() to stop the event loop
	closed  bool
}

// newWorkspaceWatcher creates the watcher and starts its event loop. emit is
// called (debounced) whenever a change is detected. Best-effort: callers may
// treat a nil result + error as "watching unavailable" and degrade gracefully.
func newWorkspaceWatcher(emit func()) (*workspaceWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	ww := &workspaceWatcher{
		w:       w,
		watched: make(map[string]bool),
		emit:    emit,
		done:    make(chan struct{}),
	}
	go ww.loop()
	return ww, nil
}

// close stops the event loop and releases the OS watcher. Safe to call once;
// later calls are no-ops.
func (ww *workspaceWatcher) close() {
	ww.mu.Lock()
	if ww.closed {
		ww.mu.Unlock()
		return
	}
	ww.closed = true
	close(ww.done)
	ww.mu.Unlock()
	_ = ww.w.Close()
}

// watch re-points the watcher at root. A no-op when root equals the current
// watched root, so callers can invoke it freely on every tab switch.
func (ww *workspaceWatcher) watch(root string) {
	ww.mu.Lock()
	defer ww.mu.Unlock()
	if ww.closed || ww.root == root {
		return
	}
	ww.root = root
	// Drop every existing OS watch before re-adding the new tree.
	for dir := range ww.watched {
		_ = ww.w.Remove(dir)
	}
	ww.watched = make(map[string]bool)
	ww.addTreeLocked(root)
}

// addTreeLocked adds the directory tree under dir to the OS watcher, skipping
// the same noise entries the file tree hides (workspaceNoiseNames / Dirs).
// Caller must hold ww.mu. Failures (permission, dir vanished mid-walk) are
// silently skipped; a later Create event re-adds dirs that appear.
func (ww *workspaceWatcher) addTreeLocked(dir string) {
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // dir may vanish mid-walk; skip
		}
		if !d.IsDir() {
			return nil
		}
		if path != dir {
			rel, rerr := filepath.Rel(ww.root, path)
			if rerr != nil {
				rel = filepath.Base(path)
			}
			parentRel := filepath.ToSlash(filepath.Dir(rel))
			if skipWorkspaceEntry(parentRel, d.Name(), true) {
				return filepath.SkipDir
			}
		}
		if err := ww.w.Add(path); err == nil {
			ww.watched[path] = true
		}
		return nil
	})
}

// loop drains fsnotify events until close(). Changes are debounced: a burst of
// events within debounceWindow coalesces into a single emit.
const debounceWindow = 100 * time.Millisecond

func (ww *workspaceWatcher) loop() {
	var pending bool
	var timerC <-chan time.Time
	for {
		select {
		case <-ww.done:
			return
		case ev, ok := <-ww.w.Events:
			if !ok {
				return
			}
			ww.handleEvent(ev)
			if !pending {
				pending = true
				t := time.NewTimer(debounceWindow)
				timerC = t.C
			}
		case <-timerC:
			pending = false
			timerC = nil
			if ww.emit != nil {
				ww.emit()
			}
		case err, ok := <-ww.w.Errors:
			if !ok {
				return
			}
			log.Printf("workspace watcher: %v", err)
		}
	}
}

// handleEvent reacts to a single fsnotify event: newly created directories are
// added to the watcher so files inside them are covered too.
func (ww *workspaceWatcher) handleEvent(ev fsnotify.Event) {
	if ev.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Write) == 0 {
		return
	}
	if ev.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			ww.mu.Lock()
			ww.addTreeLocked(ev.Name)
			ww.mu.Unlock()
		}
	}
	if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		// fsnotify drops watches on deleted dirs; prune our bookkeeping so a
		// later re-watch doesn't try to Remove paths the OS no longer knows.
		ww.mu.Lock()
		for dir := range ww.watched {
			if dir == ev.Name || strings.HasPrefix(dir, ev.Name+string(os.PathSeparator)) {
				delete(ww.watched, dir)
			}
		}
		ww.mu.Unlock()
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// callbackTask is a pending callback retry: the result of a processed IM
// command that could not be pushed back to the originating platform. The
// background worker retries it with exponential backoff until success or
// maxAttempts is exhausted.
type callbackTask struct {
	CommandID string    `json:"command_id"`
	Result    string    `json:"result"`
	ChatID    string    `json:"chat_id,omitempty"` // for dead-target marking on permanent failure
	Platform  string    `json:"platform,omitempty"`
	Attempts  int       `json:"attempts"`
	NextRetry time.Time `json:"next_retry"`
	LastError string    `json:"last_error,omitempty"`
	EnqueuedAt time.Time `json:"enqueued_at"`
}

// callbackBackoffSchedule is the exponential backoff between retry attempts.
// After the last entry, the task is dropped and the session marked failed.
var callbackBackoffSchedule = []time.Duration{
	30 * time.Second,
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	1 * time.Hour,
}

// maxCallbackAttempts = len(backoff schedule). A task is dropped after this
// many total attempts.
func maxCallbackAttempts() int { return len(callbackBackoffSchedule) }

// callbackRetryQueue holds pending callback tasks and runs a background
// worker that retries them. Tasks are persisted to callbacks.json so they
// survive plugin restarts.
type callbackRetryQueue struct {
	mu     sync.Mutex
	tasks  []*callbackTask
	stopCh chan struct{}
	done   chan struct{}
}

var callbackRetry = &callbackRetryQueue{
	stopCh: make(chan struct{}),
	done:   make(chan struct{}),
}

// callbackRetryEnabled returns true when the retry mechanism is active.
// Controlled by IM_CALLBACK_RETRY env (default "on"). When off, failed
// pushes are returned as errors immediately without enqueueing retries —
// useful for emergency rollback without recompiling.
func callbackRetryEnabled() bool {
	return envString("IM_CALLBACK_RETRY", "on") == "on"
}

// Enqueue adds a failed callback to the retry queue. If the retry mechanism
// is disabled (IM_CALLBACK_RETRY=off), this is a no-op.
func (q *callbackRetryQueue) Enqueue(commandID, result, chatID, platform string, lastErr error) {
	if !callbackRetryEnabled() {
		return
	}
	q.mu.Lock()
	// Avoid duplicate enqueue for the same command
	for _, t := range q.tasks {
		if t.CommandID == commandID {
			q.mu.Unlock()
			return
		}
	}
	now := time.Now()
	task := &callbackTask{
		CommandID:  commandID,
		Result:     result,
		ChatID:     chatID,
		Platform:   platform,
		NextRetry:  now.Add(callbackBackoffSchedule[0]),
		EnqueuedAt: now,
	}
	if lastErr != nil {
		task.LastError = lastErr.Error()
	}
	q.tasks = append(q.tasks, task)
	q.mu.Unlock()
	q.persist()
	log.Printf("callback_retry: enqueued %s (chat=%s, platform=%s, next_retry=%s)",
		commandID, chatID, platform, time.Until(task.NextRetry).Round(time.Second))
}

// StartWorker launches the background goroutine that retries pending callbacks.
// Should be called once from main() at startup, after loadState().
func (q *callbackRetryQueue) StartWorker() {
	go q.worker()
}

// Stop signals the worker to exit and blocks until it has stopped.
func (q *callbackRetryQueue) Stop() {
	select {
	case <-q.stopCh: // already closed
		return
	default:
		close(q.stopCh)
	}
	<-q.done
}

func (q *callbackRetryQueue) worker() {
	defer close(q.done)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	sweepTicker := time.NewTicker(10 * time.Minute) // periodic cleanup
	defer sweepTicker.Stop()
	for {
		select {
		case <-q.stopCh:
			return
		case <-ticker.C:
			q.processDue()
		case <-sweepTicker.C:
			// Sweep expired webhook cache entries and stale dead-target marks
			dingWebhookCache.Sweep()
			deadTargets.Sweep(24 * time.Hour)
		}
	}
}

// processDue retries all tasks whose NextRetry time has passed.
func (q *callbackRetryQueue) processDue() {
	now := time.Now()
	q.mu.Lock()
	due := make([]*callbackTask, 0)
	remaining := make([]*callbackTask, 0, len(q.tasks))
	for _, t := range q.tasks {
		if !t.NextRetry.After(now) {
			due = append(due, t)
		} else {
			remaining = append(remaining, t)
		}
	}
	q.tasks = remaining
	q.mu.Unlock()

	if len(due) == 0 {
		return
	}

	// Re-queue tasks that still need more attempts; drop exhausted ones.
	for _, t := range due {
		q.retryOne(t)
	}
	q.persist()
}

// retryOne attempts a single callback retry. The command has already been
// removed from the pending queue by runMarkCommandDone (to prevent the IM
// watcher from re-fetching it), so this function only needs to push the
// result and transition the session state accordingly:
//   - success  → session done
//   - dead     → session failed, target marked dead
//   - exhausted → session failed
//   - retryable → re-enqueue with backoff, session stays callback_pending
func (q *callbackRetryQueue) retryOne(t *callbackTask) {
	cmd := queue.get(t.CommandID)
	if cmd == nil {
		// Command was removed (e.g. state reset); drop the task.
		log.Printf("callback_retry: drop %s (command not found)", t.CommandID)
		return
	}

	res := retryCallback(cmd, t.Result)
	if res.Success {
		// Push succeeded — finalize the session (command already marked done).
		sessionStore.markSessionDone(t.CommandID, t.Result)
		if t.ChatID != "" {
			deadTargets.Clear(t.Platform, t.ChatID)
		}
		log.Printf("callback_retry: success %s after %d attempts", t.CommandID, t.Attempts+1)
		return
	}

	t.Attempts++
	t.LastError = res.Error

	// Permanent error: mark target dead, drop task, fail session.
	if res.isDead() {
		deadTargets.MarkDead(t.Platform, t.ChatID)
		sessionStore.markSessionFailed(t.CommandID, res.Error)
		log.Printf("callback_retry: drop %s (dead_target: %s)", t.CommandID, res.Error)
		return
	}

	// Exhausted retries: drop task, fail session.
	if t.Attempts >= maxCallbackAttempts() {
		sessionStore.markSessionFailed(t.CommandID, fmt.Sprintf("callback retry exhausted after %d attempts: %s", t.Attempts, res.Error))
		log.Printf("callback_retry: drop %s (exhausted %d attempts)", t.CommandID, t.Attempts)
		return
	}

	// Retryable: schedule next attempt with backoff.
	backoff := callbackBackoffSchedule[min(t.Attempts, len(callbackBackoffSchedule)-1)]
	t.NextRetry = time.Now().Add(backoff)
	q.mu.Lock()
	q.tasks = append(q.tasks, t)
	q.mu.Unlock()
	log.Printf("callback_retry: re-enqueue %s (attempt %d, next in %s, err=%s)",
		t.CommandID, t.Attempts, backoff.Round(time.Second), res.Error)
}

// --- persistence ---

// callbacksFile returns the path to the callback retry queue persistence file.
func callbacksFile() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	dir := filepath.Join(home, ".reasonix", "im-plugin")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "callbacks.json")
}

func (q *callbackRetryQueue) persist() {
	q.mu.Lock()
	snapshot := make([]*callbackTask, len(q.tasks))
	copy(snapshot, q.tasks)
	q.mu.Unlock()
	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		log.Printf("callback_retry: persist marshal: %v", err)
		return
	}
	if err := os.WriteFile(callbacksFile(), b, 0o644); err != nil {
		log.Printf("callback_retry: persist write: %v", err)
	}
}

func (q *callbackRetryQueue) load() {
	b, err := os.ReadFile(callbacksFile())
	if err != nil {
		return // first run
	}
	var tasks []*callbackTask
	if err := json.Unmarshal(b, &tasks); err != nil {
		log.Printf("callback_retry: load parse %s: %v", callbacksFile(), err)
		return
	}
	q.mu.Lock()
	// Filter out tasks whose commands no longer exist (stale).
	kept := make([]*callbackTask, 0, len(tasks))
	for _, t := range tasks {
		if queue.get(t.CommandID) != nil {
			// Reset next retry to "due soon" so we don't wait the full original backoff after a restart.
			t.NextRetry = time.Now().Add(30 * time.Second)
			kept = append(kept, t)
		}
	}
	q.tasks = kept
	q.mu.Unlock()
	log.Printf("callback_retry: loaded %d pending retry tasks from %s", len(kept), callbacksFile())
}

// PendingCount returns the number of tasks currently waiting (for diagnostics).
func (q *callbackRetryQueue) PendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks)
}

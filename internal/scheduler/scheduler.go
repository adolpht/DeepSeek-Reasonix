// Package scheduler provides a cron-based task scheduler that loads enabled
// scheduled tasks from the persistent data store and triggers agent execution
// on their cron schedules. It uses robfig/cron/v3 as the scheduling engine.
package scheduler

import (
	"fmt"
	"log"
	"sync"
	"time"

	"reasonix/internal/datastore"

	"github.com/robfig/cron/v3"
)

// Scheduler manages periodic task execution using cron expressions.
type Scheduler struct {
	cron       *cron.Cron
	mu         sync.Mutex
	entries    map[string]cron.EntryID // name -> entryID
	store      TaskStore
	onExec     func(name string, skill string, params string) string
	onTaskStart func(name string, skill string)
	onTaskDone  func(name string, skill string, result string)
}

// TaskStore is the interface for persisting scheduled tasks. It is satisfied by
// datastore.Store; the indirection keeps this package free of the SQLite import.
type TaskStore interface {
	ListScheduledTasks() ([]datastore.ScheduledTask, error)
	UpdateScheduledTask(t datastore.ScheduledTask) error
	AddNotification(kind, title, body string) error
}

// ScheduledTask is a copy of the persistent model, kept here so callers don't
// need to import datastore directly.
type ScheduledTask = datastore.ScheduledTask

// NewScheduler creates a scheduler backed by store. onExec is called for each
// task tick; its return value is recorded as the execution result. If onExec is
// nil the scheduler still runs but skips execution. onTaskStart and onTaskDone
// are optional lifecycle callbacks called before and after execution.
func NewScheduler(store TaskStore, onExec func(name, skill, params string) string, onTaskStart func(name, skill string), onTaskDone func(name, skill, result string)) *Scheduler {
	if onExec == nil {
		onExec = func(_, _, _ string) string { return "" }
	}
	if onTaskStart == nil {
		onTaskStart = func(_, _ string) {}
	}
	if onTaskDone == nil {
		onTaskDone = func(_, _, _ string) {}
	}
	return &Scheduler{
		cron:        cron.New(cron.WithSeconds(), cron.WithLocation(time.Local)),
		entries:     make(map[string]cron.EntryID),
		store:       store,
		onExec:      onExec,
		onTaskStart: onTaskStart,
		onTaskDone:  onTaskDone,
	}
}

// Start launches the cron engine and loads all enabled tasks from the store.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks, err := s.store.ListScheduledTasks()
	if err != nil {
		log.Printf("scheduler: load tasks: %v", err)
		return
	}
	for _, t := range tasks {
		if !t.Enabled {
			continue
		}
		if err := s.registerLocked(t.Name, t.Cron, t.Skill, t.Parameters); err != nil {
			log.Printf("scheduler: skip task %q (%s): %v", t.Name, t.Cron, err)
		}
	}
	s.cron.Start()
}

// Stop gracefully stops the cron engine.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := s.cron.Stop()
	<-ctx.Done()
}

// Register adds a new task to the scheduler. If the cron expression is invalid
// the task is skipped and an error is returned.
func (s *Scheduler) Register(name, cronExpr, skill, params string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registerLocked(name, cronExpr, skill, params)
}

func (s *Scheduler) registerLocked(name, cronExpr, skill, params string) error {
	if _, ok := s.entries[name]; ok {
		s.cron.Remove(s.entries[name])
		delete(s.entries, name)
	}

	id, err := s.cron.AddFunc(cronExpr, func() {
		s.executeTask(name, skill, params)
	})
	if err != nil {
		return fmt.Errorf("invalid cron %q: %w", cronExpr, err)
	}
	s.entries[name] = id
	return nil
}

// Unregister removes a task from the scheduler by name.
func (s *Scheduler) Unregister(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.entries[name]
	if !ok {
		return fmt.Errorf("task %q not found", name)
	}
	s.cron.Remove(id)
	delete(s.entries, name)
	return nil
}

// Enable registers a task in the cron engine. The caller is expected to have
// already updated the enabled flag in the store.
func (s *Scheduler) Enable(name, cronExpr, skill, params string) error {
	return s.Register(name, cronExpr, skill, params)
}

// Disable removes a task from the cron engine without deleting it from the store.
func (s *Scheduler) Disable(name string) error {
	return s.Unregister(name)
}

// executeTask is the per-tick callback. It invokes onExec, records the
// execution time, and sends a completion notification.
func (s *Scheduler) executeTask(name, skill, params string) {
	s.onTaskStart(name, skill)
	now := time.Now()
	result := s.onExec(name, skill, params)

	s.mu.Lock()
	tasks, err := s.store.ListScheduledTasks()
	s.mu.Unlock()
	if err != nil {
		log.Printf("scheduler: post-exec list for %q: %v", name, err)
		return
	}

	for _, t := range tasks {
		if t.Name != name {
			continue
		}
		// Compute next run from the cron schedule.
		var nextRun int64
		sched, pErr := cron.ParseStandard(t.Cron)
		if pErr != nil {
			// Try the full syntax (with seconds).
			parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
			sched, pErr = parser.Parse(t.Cron)
		}
		if pErr == nil {
			nextRun = sched.Next(now).UnixMilli()
		}

		t.LastRun = now.UnixMilli()
		t.NextRun = nextRun

		s.mu.Lock()
		if uErr := s.store.UpdateScheduledTask(t); uErr != nil {
			log.Printf("scheduler: update last_run for %q: %v", name, uErr)
		}
		s.mu.Unlock()
		break
	}

	// Send notification.
	title := fmt.Sprintf("Task %q completed", name)
	body := result
	if body == "" {
		body = "Scheduled task executed successfully."
	}
	s.mu.Lock()
	if nErr := s.store.AddNotification("task_complete", title, body); nErr != nil {
		log.Printf("scheduler: notify for %q: %v", name, nErr)
	}
	s.mu.Unlock()
	s.onTaskDone(name, skill, result)
}

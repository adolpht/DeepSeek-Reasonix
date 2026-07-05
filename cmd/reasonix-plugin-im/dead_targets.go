package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// deadTargetRegistry marks (platform, chatID) pairs that have been permanently
// rejected by the IM platform (bot kicked out, chat deleted, user deactivated).
// Subsequent send attempts to a dead target are short-circuited to avoid
// burning quota on hopeless retries. Any successful send clears the mark,
// providing self-healing when a target becomes reachable again.
//
// Inspired by Hermes' gateway/dead_targets.py DeadTargetRegistry.
type deadTargetRegistry struct {
	mu   sync.Mutex
	dead map[string]time.Time // key -> marked_at
}

var deadTargets = &deadTargetRegistry{dead: make(map[string]time.Time)}

// deadTargetKey builds the map key for a (platform, chatID) pair.
func deadTargetKey(platform, chatID string) string {
	return fmt.Sprintf("%s:%s", platform, chatID)
}

// MarkDead records that the given target is permanently unreachable.
// A best-effort TTL of 24h is enforced by periodicSweep to allow recovery
// after long outages (e.g. a chat being recreated).
func (d *deadTargetRegistry) MarkDead(platform, chatID string) {
	if chatID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dead[deadTargetKey(platform, chatID)] = time.Now()
	log.Printf("dead_targets: marked %s:%s dead", platform, chatID)
}

// IsDead returns true if the target is currently marked dead.
func (d *deadTargetRegistry) IsDead(platform, chatID string) bool {
	if chatID == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.dead[deadTargetKey(platform, chatID)]
	return ok
}

// Clear removes the dead mark for a target. Called on any successful send
// so that transient "dead" marks (e.g. caused by a temporary platform
// outage misclassified as permanent) self-heal.
func (d *deadTargetRegistry) Clear(platform, chatID string) {
	if chatID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	key := deadTargetKey(platform, chatID)
	if _, ok := d.dead[key]; ok {
		delete(d.dead, key)
		log.Printf("dead_targets: cleared %s:%s (self-healed)", platform, chatID)
	}
}

// Snapshot returns a copy of the dead-target registry for persistence.
func (d *deadTargetRegistry) Snapshot() map[string]time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]time.Time, len(d.dead))
	for k, v := range d.dead {
		out[k] = v
	}
	return out
}

// Restore replaces the registry contents from a persisted snapshot.
func (d *deadTargetRegistry) Restore(m map[string]time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dead = make(map[string]time.Time, len(m))
	for k, v := range m {
		d.dead[k] = v
	}
}

// Sweep removes entries older than maxAge. Called periodically by the
// callback retry worker to allow long-dead targets to be retried again.
func (d *deadTargetRegistry) Sweep(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, marked := range d.dead {
		if marked.Before(cutoff) {
			delete(d.dead, k)
		}
	}
}

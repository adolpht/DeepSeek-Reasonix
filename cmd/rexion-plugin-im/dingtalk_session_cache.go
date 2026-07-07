package main

import (
	"sync"
	"time"
)

// sessionWebhookTTL is the validity window of a DingTalk SessionWebhook URL.
// DingTalk documents it as ~2 hours; we use a slightly conservative 1h55m so
// that a callback attempted right at the boundary still has a usable URL.
const sessionWebhookTTL = 1*time.Hour + 55*time.Minute

// sessionWebhookEntry caches the most recent SessionWebhook URL observed
// for a DingTalk chat. When a long-running task completes after the original
// message's SessionWebhook expired, pushResult falls back to this cache to
// find a fresher URL from any later message in the same chat.
//
// Inspired by Hermes' DingTalkAdapter._session_webhooks dict (chat_id -> url).
type sessionWebhookEntry struct {
	URL      string
	ExpireAt time.Time
	Updated  time.Time
}

// sessionWebhookCache is a thread-safe map from chatID -> sessionWebhookEntry.
// Entries expire after sessionWebhookTTL and are evicted lazily on access.
type sessionWebhookCache struct {
	mu     sync.RWMutex
	byChat map[string]sessionWebhookEntry
}

var dingWebhookCache = &sessionWebhookCache{byChat: make(map[string]sessionWebhookEntry)}

// Refresh stores or updates the SessionWebhook URL for a chatID.
// Called from handleDingTalkStreamMessage for every inbound DingTalk message.
func (c *sessionWebhookCache) Refresh(chatID, url string) {
	if chatID == "" || url == "" {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byChat[chatID] = sessionWebhookEntry{
		URL:      url,
		ExpireAt: now.Add(sessionWebhookTTL),
		Updated:  now,
	}
}

// Latest returns the freshest non-expired SessionWebhook URL for chatID,
// or "" if none is available (cache miss or all entries expired).
func (c *sessionWebhookCache) Latest(chatID string) string {
	if chatID == "" {
		return ""
	}
	c.mu.RLock()
	entry, ok := c.byChat[chatID]
	c.mu.RUnlock()
	if !ok {
		return ""
	}
	if time.Now().After(entry.ExpireAt) {
		return ""
	}
	return entry.URL
}

// LatestEntry returns the full entry (including expiry) for diagnostics.
func (c *sessionWebhookCache) LatestEntry(chatID string) (sessionWebhookEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.byChat[chatID]
	return e, ok
}

// Sweep removes expired entries. Called periodically by the callback retry
// worker to bound memory usage.
func (c *sessionWebhookCache) Sweep() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.byChat {
		if now.After(e.ExpireAt) {
			delete(c.byChat, k)
		}
	}
}

// Snapshot returns a copy of the cache for persistence.
func (c *sessionWebhookCache) Snapshot() map[string]sessionWebhookEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]sessionWebhookEntry, len(c.byChat))
	for k, v := range c.byChat {
		out[k] = v
	}
	return out
}

// Restore replaces the cache contents from a persisted snapshot.
func (c *sessionWebhookCache) Restore(m map[string]sessionWebhookEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byChat = make(map[string]sessionWebhookEntry, len(m))
	for k, v := range m {
		c.byChat[k] = v
	}
}

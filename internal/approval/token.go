// Package approval provides a one-time token mechanism for overriding permission
// gate denials. When the gate blocks a tool call, an ApprovalToken is generated
// and emitted to the frontend. The user can approve the operation out-of-band
// (e.g., clicking "Allow" in the UI), which calls TokenStore.Redeem with the
// token. On the next retry, the agent finds the redeemed token and proceeds
// without re-prompting.
//
// Tokens are cryptographically random, short-lived, and scoped to a specific
// (tool, subject) pair. They are stored in-memory only and never persisted.
package approval

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// DefaultTokenTTL is how long an approval token remains redeemable.
const DefaultTokenTTL = 5 * time.Minute

// Token represents a one-time approval credential.
type Token struct {
	// ID is the random hex-encoded token identifier (32 chars = 16 bytes).
	ID string `json:"id"`

	// Tool is the tool name this token authorizes.
	Tool string `json:"tool"`

	// Subject is the specific subject (e.g., command, file path) this token
	// authorizes. Empty means any subject for the tool.
	Subject string `json:"subject"`

	// CreatedAt is when the token was generated.
	CreatedAt time.Time `json:"created_at"`

	// ExpiresAt is when the token can no longer be redeemed.
	ExpiresAt time.Time `json:"expires_at"`

	// redeemed is set to true once the token is consumed.
	redeemed bool
}

// IsExpired reports whether the token has passed its expiration time.
func (t *Token) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// Matches checks whether this token authorizes the given tool+subject.
func (t *Token) Matches(tool, subject string) bool {
	if t.Tool != tool {
		return false
	}
	// Empty subject on the token means "any subject for this tool".
	if t.Subject == "" {
		return true
	}
	return t.Subject == subject
}

// Store manages in-memory approval tokens. It is safe for concurrent use.
type Store struct {
	mu     sync.Mutex
	tokens map[string]*Token // keyed by token ID
	ttl    time.Duration
}

// NewStore creates a token store with the given TTL for new tokens.
// If ttl <= 0, DefaultTokenTTL is used.
func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = DefaultTokenTTL
	}
	return &Store{
		tokens: make(map[string]*Token),
		ttl:    ttl,
	}
}

// Generate creates a new approval token for the given tool+subject pair.
// The token is random, time-bounded, and stored for later redemption.
func (s *Store) Generate(tool, subject string) (*Token, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	now := time.Now()
	t := &Token{
		ID:        hex.EncodeToString(b),
		Tool:      tool,
		Subject:   subject,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
	s.mu.Lock()
	s.tokens[t.ID] = t
	s.mu.Unlock()
	return t, nil
}

// Redeem marks a token as consumed. Returns the token if it exists and
// has not expired or been redeemed already. Returns nil otherwise.
// Redeem is idempotent: calling it twice on the same token returns nil
// on the second call.
func (s *Store) Redeem(id string) *Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[id]
	if !ok {
		return nil
	}
	if t.redeemed || t.IsExpired() {
		return nil
	}
	t.redeemed = true
	return t
}

// Lookup finds a token by ID without consuming it. Returns nil if not
// found or expired.
func (s *Store) Lookup(id string) *Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[id]
	if !ok || t.redeemed || t.IsExpired() {
		return nil
	}
	return t
}

// HasRedeemedToken checks whether there is a redeemed (consumed) token
// that authorizes the given tool+subject pair. This is the check the
// agent uses to decide whether to proceed without re-prompting.
func (s *Store) HasRedeemedToken(tool, subject string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tokens {
		if t.redeemed && !t.IsExpired() && t.Matches(tool, subject) {
			return true
		}
	}
	return false
}

// Purge removes expired and redeemed tokens to free memory.
func (s *Store) Purge() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, t := range s.tokens {
		if t.redeemed || t.IsExpired() {
			delete(s.tokens, id)
		}
	}
}

// Len returns the number of active (non-expired, non-redeemed) tokens.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, t := range s.tokens {
		if !t.redeemed && !t.IsExpired() {
			n++
		}
	}
	return n
}

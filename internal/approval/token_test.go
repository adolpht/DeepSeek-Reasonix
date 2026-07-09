package approval

import (
	"testing"
	"time"
)

func TestTokenMatches(t *testing.T) {
	tok := &Token{Tool: "bash", Subject: "rm -rf /tmp"}
	if !tok.Matches("bash", "rm -rf /tmp") {
		t.Error("should match exact tool+subject")
	}
	if tok.Matches("bash", "ls /tmp") {
		t.Error("should not match different subject")
	}
	if tok.Matches("write_file", "rm -rf /tmp") {
		t.Error("should not match different tool")
	}
}

func TestTokenWildcardSubject(t *testing.T) {
	tok := &Token{Tool: "bash", Subject: ""}
	if !tok.Matches("bash", "anything") {
		t.Error("empty subject should match any subject")
	}
	if tok.Matches("write_file", "anything") {
		t.Error("should not match different tool even with wildcard subject")
	}
}

func TestTokenExpiry(t *testing.T) {
	tok := &Token{ExpiresAt: time.Now().Add(-time.Second)}
	if !tok.IsExpired() {
		t.Error("token in the past should be expired")
	}
	tok.ExpiresAt = time.Now().Add(time.Hour)
	if tok.IsExpired() {
		t.Error("token in the future should not be expired")
	}
}

func TestStoreGenerateAndRedeem(t *testing.T) {
	s := NewStore(5 * time.Minute)
	tok, err := s.Generate("bash", "rm -rf /tmp")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if tok.Tool != "bash" || tok.Subject != "rm -rf /tmp" {
		t.Errorf("token = %+v, want tool=bash subject='rm -rf /tmp'", tok)
	}
	if len(tok.ID) != 32 {
		t.Errorf("token ID length = %d, want 32", len(tok.ID))
	}

	// Redeem should work once.
	redeemed := s.Redeem(tok.ID)
	if redeemed == nil {
		t.Error("first Redeem should return the token")
	}
	// Second redeem should fail.
	redeemed = s.Redeem(tok.ID)
	if redeemed != nil {
		t.Error("second Redeem should return nil (already consumed)")
	}
}

func TestStoreRedeemExpired(t *testing.T) {
	s := NewStore(1 * time.Millisecond)
	tok, _ := s.Generate("bash", "ls")
	time.Sleep(5 * time.Millisecond) // let it expire
	redeemed := s.Redeem(tok.ID)
	if redeemed != nil {
		t.Error("Redeem of expired token should return nil")
	}
}

func TestStoreHasRedeemedToken(t *testing.T) {
	s := NewStore(5 * time.Minute)
	tok, _ := s.Generate("bash", "rm -rf /tmp")

	// Not redeemed yet.
	if s.HasRedeemedToken("bash", "rm -rf /tmp") {
		t.Error("should not have redeemed token before Redeem")
	}

	// Redeem it.
	s.Redeem(tok.ID)

	// Now it should be found.
	if !s.HasRedeemedToken("bash", "rm -rf /tmp") {
		t.Error("should have redeemed token after Redeem")
	}

	// Different subject should not match.
	if s.HasRedeemedToken("bash", "ls /tmp") {
		t.Error("should not match different subject")
	}

	// Different tool should not match.
	if s.HasRedeemedToken("write_file", "rm -rf /tmp") {
		t.Error("should not match different tool")
	}
}

func TestStoreHasRedeemedTokenWildcard(t *testing.T) {
	s := NewStore(5 * time.Minute)
	tok, _ := s.Generate("bash", "") // wildcard subject
	s.Redeem(tok.ID)

	if !s.HasRedeemedToken("bash", "any command") {
		t.Error("wildcard subject should match any subject for the tool")
	}
}

func TestStoreLookup(t *testing.T) {
	s := NewStore(5 * time.Minute)
	tok, _ := s.Generate("bash", "ls")

	found := s.Lookup(tok.ID)
	if found == nil {
		t.Error("Lookup should find active token")
	}

	s.Redeem(tok.ID)
	found = s.Lookup(tok.ID)
	if found != nil {
		t.Error("Lookup should not find redeemed token")
	}
}

func TestStorePurge(t *testing.T) {
	s := NewStore(1 * time.Millisecond)

	// Generate and let expire.
	tok1, _ := s.Generate("bash", "ls")
	time.Sleep(5 * time.Millisecond)

	// Generate a fresh one.
	tok2, _ := s.Generate("bash", "pwd")

	// Redeem tok2.
	s.Redeem(tok2.ID)

	s.Purge()

	// tok1 should be purged (expired).
	if s.Lookup(tok1.ID) != nil {
		t.Error("expired token should be purged")
	}
	// tok2 should be purged (redeemed).
	if s.Lookup(tok2.ID) != nil {
		t.Error("redeemed token should be purged")
	}
}

func TestStoreLen(t *testing.T) {
	s := NewStore(5 * time.Minute)
	if s.Len() != 0 {
		t.Errorf("Len = %d, want 0", s.Len())
	}
	s.Generate("bash", "ls")
	s.Generate("bash", "pwd")
	if s.Len() != 2 {
		t.Errorf("Len = %d, want 2", s.Len())
	}
}

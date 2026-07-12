package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"rexion/internal/provider"
)

// serializedSessionVersion is the payload schema version. Bump when the shape
// changes in a backward-incompatible way; DeserializeSession should branch on
// this to migrate older exports.
const serializedSessionVersion = 1

// SerializedSession is the portable JSON shape for a session export. It carries
// both the thread metadata and the full message history so another device can
// reconstruct the conversation without access to the original store.
type SerializedSession struct {
	Version      int                 `json:"version"`
	SessionID    string              `json:"session_id"`
	Title        string              `json:"title,omitempty"`
	Model        string              `json:"model,omitempty"`
	Workspace    string              `json:"workspace,omitempty"`
	Scope        string              `json:"scope,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
	MessageCount int                 `json:"message_count"`
	Messages     []provider.Message  `json:"messages"`
}

// SerializeSession exports a session (thread metadata + full message history) as
// JSON. The bytes are self-contained: a remote device can reconstruct the
// conversation by feeding them to DeserializeSession. Returns an error if the
// thread doesn't exist or the store is unreadable.
func SerializeSession(store Store, sessionID string) ([]byte, error) {
	ctx := context.Background()
	thread, err := store.GetThread(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("serialize: get thread %s: %w", sessionID, err)
	}
	if thread == nil {
		return nil, fmt.Errorf("serialize: thread %s not found", sessionID)
	}
	msgs, err := store.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("serialize: get messages: %w", err)
	}
	out := SerializedSession{
		Version:      serializedSessionVersion,
		SessionID:    thread.ID,
		Title:        thread.Title,
		Model:        thread.Model,
		Workspace:    thread.Workspace,
		Scope:        thread.Scope,
		CreatedAt:    thread.CreatedAt,
		UpdatedAt:    thread.UpdatedAt,
		MessageCount: len(msgs),
		Messages:     msgs,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("serialize: marshal: %w", err)
	}
	return data, nil
}

// DeserializeSession imports a session from JSON produced by SerializeSession.
// A new thread is created with a fresh ID (so the import never overwrites a
// local session with the same original ID), the messages are appended, and the
// new session ID is returned. The original CreatedAt/UpdatedAt are preserved so
// the conversation timeline stays accurate across devices.
func DeserializeSession(store Store, data []byte) (string, error) {
	var s SerializedSession
	if err := json.Unmarshal(data, &s); err != nil {
		return "", fmt.Errorf("deserialize: unmarshal: %w", err)
	}
	if s.Version > serializedSessionVersion {
		return "", fmt.Errorf("deserialize: version %d is newer than supported %d", s.Version, serializedSessionVersion)
	}
	newID := newSyncSessionID()
	thread := &Thread{
		ID:        newID,
		Title:     s.Title,
		Model:     s.Model,
		Workspace: s.Workspace,
		Scope:     s.Scope,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
	ctx := context.Background()
	if err := store.CreateThread(ctx, thread); err != nil {
		return "", fmt.Errorf("deserialize: create thread: %w", err)
	}
	if len(s.Messages) > 0 {
		if err := store.AppendMessages(ctx, newID, s.Messages); err != nil {
			return "", fmt.Errorf("deserialize: append messages: %w", err)
		}
	}
	return newID, nil
}

// newSyncSessionID returns a unique session ID for an imported session. The
// format mirrors NewSessionPath's timestamp stem so it sorts naturally beside
// existing sessions, with a random suffix to avoid collisions when the same
// export is imported twice in the same second.
func newSyncSessionID() string {
	stamp := time.Now().UTC().Format("20060102-150405")
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-sync-%s", stamp, hex.EncodeToString(b))
}

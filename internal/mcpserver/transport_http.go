package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"
)

// runHTTP runs the MCP server using the Streamable HTTP transport.
// It exposes:
//   - POST /mcp  — JSON-RPC request endpoint
//   - GET /mcp   — SSE stream for server-initiated messages
//
// Session management is done via the Mcp-Session-Id header.
func (s *Server) runHTTP(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleHTTP)

	hs := &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(l net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("MCP HTTP server listening", "addr", s.addr)
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http server: %w", err)
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// handleHTTP handles all requests to the /mcp endpoint.
func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePost(w, r)
	case http.MethodGet:
		s.handleGet(w, r)
	case http.MethodDelete:
		s.handleDelete(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePost processes JSON-RPC requests via HTTP POST.
func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		http.Error(w, "content-type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		resp := errorResponse(nil, errParseError, "parse error: "+err.Error())
		writeHTTPResponse(w, resp, "")
		return
	}

	// Validate or assign session ID.
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		// For initialize requests, create a new session.
		sessionID = generateSessionID()
	} else if !s.sessions.IsValid(sessionID) {
		resp := errorResponse(req.ID, errInvalidRequest, "invalid session id")
		writeHTTPResponse(w, resp, "")
		return
	}

	// Register session on initialize.
	if req.Method == "initialize" {
		s.sessions.Register(sessionID)
	}

	resp := s.handleRequest(r.Context(), req)

	// For notifications, return 202 Accepted with no body.
	if req.IsNotification() {
		w.Header().Set("Mcp-Session-Id", sessionID)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	writeHTTPResponse(w, resp, sessionID)
}

// handleGet establishes an SSE stream for server-initiated messages.
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" || !s.sessions.IsValid(sessionID) {
		http.Error(w, "valid Mcp-Session-Id required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Mcp-Session-Id", sessionID)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Keep the connection alive until the client disconnects.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// handleDelete terminates a session.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID != "" {
		s.sessions.Remove(sessionID)
	}
	w.WriteHeader(http.StatusOK)
}

// writeHTTPResponse writes a JSON-RPC response as an HTTP response.
func writeHTTPResponse(w http.ResponseWriter, resp Response, sessionID string) {
	w.Header().Set("Content-Type", "application/json")
	if sessionID != "" {
		w.Header().Set("Mcp-Session-Id", sessionID)
	}
	w.WriteHeader(http.StatusOK)
	data, _ := json.Marshal(resp)
	w.Write(data)
}

// generateSessionID creates a new unique session identifier.
func generateSessionID() string {
	return fmt.Sprintf("reasonix-%d", sessionCounter.Add(1))
}

var sessionCounter atomic.Int64

// sessionManager tracks active MCP sessions.
type sessionManager struct {
	mu       sync.Mutex
	sessions map[string]struct{}
}

func newSessionManager() *sessionManager {
	return &sessionManager{
		sessions: make(map[string]struct{}),
	}
}

func (sm *sessionManager) Register(id string) {
	sm.mu.Lock()
	sm.sessions[id] = struct{}{}
	sm.mu.Unlock()
}

func (sm *sessionManager) IsValid(id string) bool {
	sm.mu.Lock()
	_, ok := sm.sessions[id]
	sm.mu.Unlock()
	return ok
}

func (sm *sessionManager) Remove(id string) {
	sm.mu.Lock()
	delete(sm.sessions, id)
	sm.mu.Unlock()
}

// sessions is the global session manager for the HTTP transport.
var sessions = newSessionManager()

package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// --- command queue ---

// pendingCommand represents a remote command received from an IM platform.
type pendingCommand struct {
	ID         string            `json:"id"`
	Platform   string            `json:"platform"`
	Content    string            `json:"content"`
	WebhookURL string            `json:"webhook_url,omitempty"`
	Extra      map[string]string `json:"extra,omitempty"`
	ReceivedAt time.Time         `json:"received_at"`
	Done       bool              `json:"done"`
}

// commandQueue is an in-memory queue of pending commands, protected by a mutex.
// It also supports a notification channel so consumers can block-wait for new
// commands instead of busy-polling.
type commandQueue struct {
	mu       sync.Mutex
	commands []*pendingCommand
	nextID   int
	// notify is closed and replaced each time a new command is enqueued,
	// giving poll_commands a way to block until something arrives.
	notifyMu sync.Mutex
	notify   chan struct{}
}

var queue = &commandQueue{
	notify: make(chan struct{}),
}

// signalNewCommand closes the current notify channel and creates a fresh one,
// waking any goroutine blocked in runPollCommands.
func (q *commandQueue) signalNewCommand() {
	q.notifyMu.Lock()
	defer q.notifyMu.Unlock()
	close(q.notify)
	q.notify = make(chan struct{})
}

// waitChan returns the current notification channel. Callers select on it
// to be woken when a new command arrives.
func (q *commandQueue) waitChan() <-chan struct{} {
	q.notifyMu.Lock()
	defer q.notifyMu.Unlock()
	return q.notify
}

func (q *commandQueue) add(platform, content, webhookURL string, extra map[string]string) *pendingCommand {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nextID++
	id := fmt.Sprintf("cmd-%d-%s", q.nextID, randomHex(4))
	cmd := &pendingCommand{
		ID:         id,
		Platform:   platform,
		Content:    content,
		WebhookURL: webhookURL,
		Extra:      extra,
		ReceivedAt: time.Now(),
		Done:       false,
	}
	q.commands = append(q.commands, cmd)
	log.Printf("queued command %s from %s: %q", id, platform, content)
	// Signal outside the lock would be fine too (notifyMu is separate),
	// but doing it here keeps the add atomic from the caller's perspective.
	q.signalNewCommand()
	return cmd
}

func (q *commandQueue) list(limit int) []*pendingCommand {
	q.mu.Lock()
	defer q.mu.Unlock()
	var result []*pendingCommand
	for _, cmd := range q.commands {
		if cmd.Done {
			continue
		}
		result = append(result, cmd)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func (q *commandQueue) get(id string) *pendingCommand {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, cmd := range q.commands {
		if cmd.ID == id {
			return cmd
		}
	}
	return nil
}

func (q *commandQueue) markDone(id string) *pendingCommand {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, cmd := range q.commands {
		if cmd.ID == id && !cmd.Done {
			cmd.Done = true
			return cmd
		}
	}
	return nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// --- bot server ---

// botServer holds the state of the running HTTP bot server.
type botServer struct {
	server    *http.Server
	port      int
	token     string
	platforms []string
}

var (
	botMu      sync.Mutex
	runningBot *botServer
)

// runStartBot starts the local HTTP bot server.
func runStartBot(port int, platforms []string, token string) (any, error) {
	botMu.Lock()
	defer botMu.Unlock()

	if runningBot != nil {
		return nil, fmt.Errorf("bot already running on port %d", runningBot.port)
	}

	mux := http.NewServeMux()

	activePlatforms := make(map[string]bool)
	for _, p := range platforms {
		activePlatforms[p] = true
	}

	if activePlatforms["wecom"] {
		mux.HandleFunc("/im/wecom", makeWeComHandler(token))
	}
	if activePlatforms["feishu"] {
		mux.HandleFunc("/im/feishu", makeFeishuHandler(token))
	}
	if activePlatforms["dingtalk"] {
		mux.HandleFunc("/im/dingtalk", makeDingTalkHandler(token))
	}

	// Health check endpoint
	mux.HandleFunc("/im/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	srv := &http.Server{
		Handler: mux,
	}

	// Listen on the specified port (0 = auto-assign)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("listen on port %d: %w", port, err)
	}

	// Resolve actual bound port (handles port=0 auto-assignment)
	actualPort := port
	if tcpAddr, ok := ln.Addr().(*net.TCPAddr); ok {
		actualPort = tcpAddr.Port
	}

	runningBot = &botServer{
		server:    srv,
		port:      actualPort,
		token:     token,
		platforms: platforms,
	}

	// Start serving in a goroutine
	go func() {
		log.Printf("bot server listening on :%d, platforms: %v", actualPort, platforms)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("bot server error: %v", err)
		}
		botMu.Lock()
		runningBot = nil
		botMu.Unlock()
	}()

	return fmt.Sprintf("IM bot started on port %d, platforms: %v", actualPort, platforms), nil
}

// runStopBot stops the local HTTP bot server.
func runStopBot() (any, error) {
	botMu.Lock()
	defer botMu.Unlock()

	if runningBot == nil {
		return "bot is not running", nil
	}

	port := runningBot.port
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runningBot.server.Shutdown(ctx); err != nil {
		return nil, fmt.Errorf("shutdown bot server: %w", err)
	}
	runningBot = nil
	return fmt.Sprintf("IM bot stopped (was on port %d)", port), nil
}

// runListPendingCommands returns pending commands from the queue.
func runListPendingCommands(limit int) (any, error) {
	cmds := queue.list(limit)
	if len(cmds) == 0 {
		return "no pending commands", nil
	}
	b, err := json.MarshalIndent(cmds, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal commands: %w", err)
	}
	return string(b), nil
}

// runMarkCommandDone marks a command as done and pushes the result back.
func runMarkCommandDone(commandID, result string) (any, error) {
	cmd := queue.markDone(commandID)
	if cmd == nil {
		return nil, fmt.Errorf("command %q not found or already done", commandID)
	}

	// Push result back to the originating platform
	if err := pushResult(cmd, result); err != nil {
		return nil, fmt.Errorf("command marked done but failed to push result: %w", err)
	}
	return fmt.Sprintf("command %s marked done, result pushed to %s", commandID, cmd.Platform), nil
}

// pushResult sends the execution result back to the originating IM platform.
// For webhook mode: posts to the group robot webhook URL (cmd.WebhookURL).
// For stream mode: uses SessionWebhook (DingTalk) or REST API reply (Feishu).
func pushResult(cmd *pendingCommand, result string) error {
	replyMode := ""
	if cmd.Extra != nil {
		replyMode = cmd.Extra["reply_mode"]
	}
	switch cmd.Platform {
	case "wecom":
		if cmd.WebhookURL == "" {
			return fmt.Errorf("no webhook URL for WeCom")
		}
		return sendWeComMessage(cmd.WebhookURL, result, "text")
	case "feishu":
		if replyMode == "stream" {
			return replyFeishuStream(cmd.Extra["message_id"], result)
		}
		if cmd.WebhookURL == "" {
			return fmt.Errorf("no webhook URL for Feishu")
		}
		return sendFeishuMessage(cmd.WebhookURL, result, "text")
	case "dingtalk":
		if replyMode == "stream" {
			// SessionWebhook URL is stored in cmd.WebhookURL
			return sendDingTalkSessionMessage(cmd.WebhookURL, result, "text")
		}
		if cmd.WebhookURL == "" {
			return fmt.Errorf("no webhook URL for DingTalk")
		}
		return sendDingTalkMessage(cmd.WebhookURL, result, "text")
	default:
		return fmt.Errorf("unsupported platform: %s", cmd.Platform)
	}
}

// --- environment variable helpers ---

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

// --- auto-start ---

// runAutoStart inspects environment variables and starts the appropriate IM
// connections automatically. Stream mode is preferred for DingTalk/Feishu
// (no public IP needed); Webhook mode is used for WeCom.
func runAutoStart() (any, error) {
	var results []string

	// DingTalk Stream: requires IM_DINGTALK_APP_KEY + IM_DINGTALK_APP_SECRET
	if appKey := os.Getenv("IM_DINGTALK_APP_KEY"); appKey != "" {
		appSecret := os.Getenv("IM_DINGTALK_APP_SECRET")
		if appSecret == "" {
			results = append(results, "dingtalk: SKIP (IM_DINGTALK_APP_SECRET not set)")
		} else {
			r, err := runStartDingTalkStream(appKey, appSecret)
			if err != nil {
				results = append(results, fmt.Sprintf("dingtalk: ERROR: %v", err))
			} else {
				results = append(results, fmt.Sprintf("dingtalk: %v", r))
			}
		}
	}

	// Feishu Stream: requires IM_FEISHU_APP_ID + IM_FEISHU_APP_SECRET
	if appID := os.Getenv("IM_FEISHU_APP_ID"); appID != "" {
		appSecret := os.Getenv("IM_FEISHU_APP_SECRET")
		if appSecret == "" {
			results = append(results, "feishu: SKIP (IM_FEISHU_APP_SECRET not set)")
		} else {
			r, err := runStartFeishuStream(appID, appSecret)
			if err != nil {
				results = append(results, fmt.Sprintf("feishu: ERROR: %v", err))
			} else {
				results = append(results, fmt.Sprintf("feishu: %v", r))
			}
		}
	}

	// WeCom Webhook: requires IM_WECOM_KEY (WeCom has no Stream mode)
	if wecomKey := os.Getenv("IM_WECOM_KEY"); wecomKey != "" {
		port := envInt("IM_BOT_PORT", 9876)
		token := envString("IM_BOT_TOKEN", "")
		r, err := runStartBot(port, []string{"wecom"}, token)
		if err != nil {
			results = append(results, fmt.Sprintf("wecom: ERROR: %v", err))
		} else {
			results = append(results, fmt.Sprintf("wecom: %v", r))
		}
	}

	if len(results) == 0 {
		return "auto_start: no IM credentials found in environment (set IM_DINGTALK_APP_KEY, IM_FEISHU_APP_ID, or IM_WECOM_KEY)", nil
	}
	return "auto_start:\n" + joinLines(results), nil
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += "  " + l
	}
	return result
}

// --- poll commands ---

// runPollCommands waits for new pending commands with a blocking timeout.
// If commands are already queued, returns immediately. Otherwise blocks
// until a new command arrives or the timeout expires.
func runPollCommands(timeoutSec, limit int) (any, error) {
	// Fast path: commands already available
	cmds := queue.list(limit)
	if len(cmds) > 0 {
		b, err := json.MarshalIndent(cmds, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal commands: %w", err)
		}
		return string(b), nil
	}

	// Slow path: wait for notification or timeout
	timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
	defer timer.Stop()

	select {
	case <-queue.waitChan():
		// New command arrived
	case <-timer.C:
		// Timeout
	}

	cmds = queue.list(limit)
	if len(cmds) == 0 {
		return "no pending commands (timed out)", nil
	}
	b, err := json.MarshalIndent(cmds, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal commands: %w", err)
	}
	return string(b), nil
}

// --- IM session management ---

// imSession represents a tracked IM conversation session for traceability.
type imSession struct {
	ID             string    `json:"id"`
	Platform       string    `json:"platform"`
	ConversationID string    `json:"conversation_id,omitempty"`
	SenderID       string    `json:"sender_id,omitempty"`
	SenderName     string    `json:"sender_name,omitempty"`
	CommandID      string    `json:"command_id"`
	Tags           []string  `json:"tags,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// imSessionStore tracks IM conversation sessions for traceability.
type imSessionStore struct {
	mu       sync.Mutex
	sessions []*imSession
	nextID   int
}

var sessionStore = &imSessionStore{}

func (s *imSessionStore) create(platform, conversationID, senderID, senderName, commandID string, tags []string) *imSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("im-sess-%d-%s", s.nextID, randomHex(4))
	sess := &imSession{
		ID:             id,
		Platform:       platform,
		ConversationID: conversationID,
		SenderID:       senderID,
		SenderName:     senderName,
		CommandID:      commandID,
		Tags:           tags,
		CreatedAt:      time.Now(),
	}
	s.sessions = append(s.sessions, sess)
	return sess
}

// runCreateIMSession creates a new IM session for traceability.
// If the command has extra info (conversation_id, sender), it is
// automatically populated from the pending command when not explicitly provided.
func runCreateIMSession(platform, conversationID, senderID, senderName, commandID string, tags []string) (any, error) {
	// Auto-fill from the pending command's Extra if available
	cmd := queue.get(commandID)
	if cmd != nil {
		if conversationID == "" && cmd.Extra != nil {
			conversationID = cmd.Extra["conversation_id"]
			if conversationID == "" {
				conversationID = cmd.Extra["chat_id"]
			}
		}
		if senderID == "" && cmd.Extra != nil {
			senderID = cmd.Extra["sender_staff_id"]
			if senderID == "" {
				senderID = cmd.Extra["sender_id"]
			}
		}
		if senderName == "" && cmd.Extra != nil {
			senderName = cmd.Extra["sender_nick"]
		}
	}

	sess := sessionStore.create(platform, conversationID, senderID, senderName, commandID, tags)
	b, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal session: %w", err)
	}
	log.Printf("created IM session %s for command %s (platform=%s, conv=%s, sender=%s)",
		sess.ID, commandID, platform, conversationID, senderName)
	return string(b), nil
}

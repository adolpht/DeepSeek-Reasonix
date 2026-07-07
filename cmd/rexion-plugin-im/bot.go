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
	"path/filepath"
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
	Result     string            `json:"result,omitempty"`
	DoneAt     *time.Time        `json:"done_at,omitempty"`
}

// commandQueue is an in-memory queue of pending commands, protected by a mutex.
// It also supports a notification channel so consumers can block-wait for new
// commands instead of busy-polling. State is persisted to disk so history
// survives plugin restarts.
type commandQueue struct {
	mu       sync.Mutex
	commands []*pendingCommand
	nextID   int
	// notify is closed and replaced each time a new command is enqueued,
	// giving poll_commands a way to block until something arrives.
	notifyMu sync.Mutex
	notify   chan struct{}
	// seenMsgIDs deduplicates inbound messages by their platform msg_id.
	// DingTalk/Feishu SDKs may redeliver on reconnect; without dedup the
	// same user message would be enqueued twice and processed twice.
	seenMsgIDs sync.Map // msg_id -> time.Time
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
	// Dedup by msg_id (DingTalk/Feishu SDKs may redeliver on reconnect).
	msgID := ""
	if extra != nil {
		msgID = extra["msg_id"]
	}
	if msgID != "" {
		if _, seen := q.seenMsgIDs.Load(msgID); seen {
			log.Printf("dedup: skipping duplicate msg_id=%s (platform=%s)", msgID, platform)
			// Return the existing pending command if still in the queue.
			q.mu.Lock()
			for _, c := range q.commands {
				if c.Extra != nil && c.Extra["msg_id"] == msgID && !c.Done {
					q.mu.Unlock()
					return c
				}
			}
			q.mu.Unlock()
			return nil // duplicate of an already-done command
		}
		q.seenMsgIDs.Store(msgID, time.Now())
		q.sweepSeenMsgIDs()
	}
	q.mu.Lock()
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
	q.mu.Unlock()
	// persistState + log + signal happen outside the lock so persistState's
	// snapshot can re-acquire q.mu without deadlocking.
	persistState()
	log.Printf("queued command %s from %s: %q", id, platform, content)
	q.signalNewCommand()
	return cmd
}

// sweepSeenMsgIDs is a best-effort cleanup of the dedup table. It keeps the
// last 10000 entries; when exceeded, entries older than the dedup window are
// evicted. Called lazily from add() so no separate goroutine is needed.
func (q *commandQueue) sweepSeenMsgIDs() {
	const maxEntries = 10000
	const dedupWindow = 10 * time.Minute
	count := 0
	cutoff := time.Now().Add(-dedupWindow)
	q.seenMsgIDs.Range(func(k, v any) bool {
		count++
		if t, ok := v.(time.Time); ok && t.Before(cutoff) {
			q.seenMsgIDs.Delete(k)
		}
		return true
	})
	if count > maxEntries {
		// Aggressive eviction: drop everything older than 1 minute.
		shortCutoff := time.Now().Add(-1 * time.Minute)
		q.seenMsgIDs.Range(func(k, v any) bool {
			if t, ok := v.(time.Time); ok && t.Before(shortCutoff) {
				q.seenMsgIDs.Delete(k)
			}
			return true
		})
	}
}

// list returns up to limit pending (not-done) commands, oldest first.
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

// listAll returns up to limit commands, optionally including done ones.
// Returns newest first (reverse insertion order) for the management UI.
func (q *commandQueue) listAll(includeDone bool, limit int) []*pendingCommand {
	q.mu.Lock()
	defer q.mu.Unlock()
	var result []*pendingCommand
	for i := len(q.commands) - 1; i >= 0; i-- {
		cmd := q.commands[i]
		if !includeDone && cmd.Done {
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

func (q *commandQueue) markDone(id, result string) *pendingCommand {
	q.mu.Lock()
	var found *pendingCommand
	for _, cmd := range q.commands {
		if cmd.ID == id && !cmd.Done {
			cmd.Done = true
			cmd.Result = result
			now := time.Now()
			cmd.DoneAt = &now
			found = cmd
			break
		}
	}
	q.mu.Unlock()
	if found != nil {
		persistState()
	}
	return found
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
//
// CRITICAL: the command is ALWAYS removed from the pending queue (marked done)
// regardless of push success. This prevents the IM Watcher from re-fetching
// the same pending command and triggering an infinite agent-turn loop that
// would exhaust memory.
//
// On push failure, the result is handed off to the callback retry queue for
// asynchronous delivery with exponential backoff. The session is marked
// "callback_pending" so its status is queryable, and transitions to "done"
// once the retry worker successfully pushes (or "failed" if retries are
// exhausted).
func runMarkCommandDone(commandID, result string) (any, error) {
	cmd := queue.get(commandID)
	if cmd == nil {
		return nil, fmt.Errorf("command %q not found", commandID)
	}
	if cmd.Done {
		return nil, fmt.Errorf("command %q already done", commandID)
	}

	// 1. ALWAYS remove the command from the pending queue first. This is the
	//    critical fix: if we leave it pending on push failure, the IM Watcher
	//    will re-fetch it next poll, triggering another agent turn that calls
	//    mark_command_done again, creating an infinite loop.
	queue.markDone(commandID, result)

	// 2. Attempt the push. On success, finalize the session.
	res := pushResult(cmd, result, "text")
	if res.Success {
		sessionStore.markSessionDone(commandID, result)
		if chatID := commandChatID(cmd); chatID != "" {
			deadTargets.Clear(cmd.Platform, chatID)
		}
		return fmt.Sprintf("command %s marked done, result pushed to %s", commandID, cmd.Platform), nil
	}

	// 3. Push failed — hand off to the retry queue and mark the session as
	//    callback_pending so the agent / UI can see it's awaiting delivery.
	//    The retry worker will transition it to done (on success) or failed
	//    (on exhaustion). The command itself is already removed from the
	//    pending queue so the watcher will NOT re-fetch it.
	chatID := commandChatID(cmd)
	if callbackRetryEnabled() {
		callbackRetry.Enqueue(commandID, result, chatID, cmd.Platform, fmt.Errorf("%s", res.Error))
		sessionStore.markSessionCallbackPending(commandID, result)
		log.Printf("mark_command_done: command %s marked done (push failed, enqueued for retry: %s)",
			commandID, res.Error)
		return fmt.Sprintf("command %s marked done, result queued for async retry (push failed: %s)",
			commandID, res.Error), nil
	}
	// Retry disabled (IM_CALLBACK_RETRY=off) — mark session failed immediately.
	sessionStore.markSessionFailed(commandID, fmt.Sprintf("push failed and retry disabled: %s", res.Error))
	return fmt.Sprintf("command %s marked done, push failed (retry disabled): %s", commandID, res.Error), nil
}

// commandChatID extracts the conversation/chat ID from a command's Extra
// field, used for dead-target bookkeeping.
func commandChatID(cmd *pendingCommand) string {
	if cmd == nil || cmd.Extra == nil {
		return ""
	}
	if id := cmd.Extra["conversation_id"]; id != "" {
		return id
	}
	if id := cmd.Extra["chat_id"]; id != "" {
		return id
	}
	return ""
}

// pushResult sends the execution result back to the originating IM platform.
// Returns a structured SendResult so the caller (mark_command_done or the
// callback retry worker) can decide retry / dead-target policy.
//
// For DingTalk Stream mode, if the original SessionWebhook (cmd.WebhookURL)
// is empty or expired, it falls back to the freshest URL from
// dingWebhookCache for the same chat (B2 fix for RC2.4).
func pushResult(cmd *pendingCommand, result, msgType string) SendResult {
	if msgType == "" {
		msgType = "text"
	}
	replyMode := ""
	if cmd.Extra != nil {
		replyMode = cmd.Extra["reply_mode"]
	}

	// Short-circuit dead targets to avoid burning quota on hopeless retries.
	if chatID := commandChatID(cmd); chatID != "" && deadTargets.IsDead(cmd.Platform, chatID) {
		return newSendFailure(fmt.Errorf("target %s:%s is marked dead", cmd.Platform, chatID), "dead_target")
	}

	switch cmd.Platform {
	case "wecom":
		if cmd.WebhookURL == "" {
			return newSendFailure(fmt.Errorf("no webhook URL for WeCom"), "auth_failed")
		}
		if err := sendWeComMessage(cmd.WebhookURL, result, msgType); err != nil {
			return newSendFailure(err, classifySendError(err))
		}
		return newSendSuccess("")
	case "feishu":
		if replyMode == "stream" {
			if err := replyFeishuStream(cmd.Extra["message_id"], result); err != nil {
				return newSendFailure(err, classifySendError(err))
			}
			return newSendSuccess("")
		}
		if cmd.WebhookURL == "" {
			return newSendFailure(fmt.Errorf("no webhook URL for Feishu"), "auth_failed")
		}
		if err := sendFeishuMessage(cmd.WebhookURL, result, msgType); err != nil {
			return newSendFailure(err, classifySendError(err))
		}
		return newSendSuccess("")
	case "dingtalk":
		if replyMode == "stream" {
			url := cmd.WebhookURL
			// Fallback: refresh from cache if the original URL is empty or looks expired.
			if url == "" {
				chatID := commandChatID(cmd)
				if chatID != "" {
					url = dingWebhookCache.Latest(chatID)
					if url != "" {
						log.Printf("pushResult: refreshed SessionWebhook for chat=%s from cache", chatID)
					}
				}
			}
			if url == "" {
				return newSendFailure(fmt.Errorf("no SessionWebhook URL for DingTalk (chat may have no recent inbound message)"), "expired_webhook")
			}
			return sendDingTalkSessionMessage(url, result, msgType)
		}
		if cmd.WebhookURL == "" {
			return newSendFailure(fmt.Errorf("no webhook URL for DingTalk"), "auth_failed")
		}
		return sendDingTalkMessage(cmd.WebhookURL, result, msgType)
	default:
		return newSendFailure(fmt.Errorf("unsupported platform: %s", cmd.Platform), "unknown")
	}
}

// retryCallback is called by the callback retry worker. It refreshes the
// DingTalk SessionWebhook from the cache before attempting the push, so that
// a stale URL stored on the command doesn't cause permanent failure.
func retryCallback(cmd *pendingCommand, result string) SendResult {
	replyMode := ""
	if cmd.Extra != nil {
		replyMode = cmd.Extra["reply_mode"]
	}
	// For DingTalk stream mode, always refresh the URL from cache.
	if cmd.Platform == "dingtalk" && replyMode == "stream" {
		if chatID := commandChatID(cmd); chatID != "" {
			if fresh := dingWebhookCache.Latest(chatID); fresh != "" {
				cmd.WebhookURL = fresh
			}
		}
	}
	return pushResult(cmd, result, "text")
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

// imSessionStatus tracks the lifecycle of an IM conversation session.
type imSessionStatus string

const (
	imStatusPending    imSessionStatus = "pending"
	imStatusProcessing imSessionStatus = "processing"
	imStatusDone       imSessionStatus = "done"
	imStatusFailed     imSessionStatus = "failed"
	// imStatusCallbackPending marks a session whose command was already removed
	// from the pending queue (so the IM watcher won't re-fetch it) but whose
	// result has not yet been delivered to the IM platform. The callback retry
	// worker transitions it to done (on success) or failed (on exhaustion).
	imStatusCallbackPending imSessionStatus = "callback_pending"
)

// imSession represents a tracked IM conversation session for traceability.
// It links an incoming IM command to its processing state and result, and
// optionally to the agent transcript (.jsonl) that handled it.
type imSession struct {
	ID             string          `json:"id"`
	Platform       string          `json:"platform"`
	ConversationID string          `json:"conversation_id,omitempty"`
	SenderID       string          `json:"sender_id,omitempty"`
	SenderName     string          `json:"sender_name,omitempty"`
	CommandID      string          `json:"command_id"`
	Content        string          `json:"content,omitempty"`
	Tags           []string        `json:"tags,omitempty"`
	Status         imSessionStatus `json:"status"`
	Result         string          `json:"result,omitempty"`
	AgentSession   string          `json:"agent_session,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	DoneAt         *time.Time      `json:"done_at,omitempty"`
}

// imSessionStore tracks IM conversation sessions for traceability.
type imSessionStore struct {
	mu       sync.Mutex
	sessions []*imSession
	nextID   int
}

var sessionStore = &imSessionStore{}

func (s *imSessionStore) create(platform, conversationID, senderID, senderName, commandID, content string, tags []string) *imSession {
	s.mu.Lock()
	s.nextID++
	id := fmt.Sprintf("im-sess-%d-%s", s.nextID, randomHex(4))
	now := time.Now()
	sess := &imSession{
		ID:             id,
		Platform:       platform,
		ConversationID: conversationID,
		SenderID:       senderID,
		SenderName:     senderName,
		CommandID:      commandID,
		Content:        content,
		Tags:           tags,
		Status:         imStatusProcessing,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.sessions = append(s.sessions, sess)
	s.mu.Unlock()
	// persistState outside the lock to avoid deadlock with snapshot.
	persistState()
	return sess
}

// list returns up to limit sessions, newest first. If statusFilter is empty,
// all statuses are returned; otherwise only matching sessions.
func (s *imSessionStore) list(statusFilter string, limit int) []*imSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []*imSession
	for i := len(s.sessions) - 1; i >= 0; i-- {
		sess := s.sessions[i]
		if statusFilter != "" && string(sess.Status) != statusFilter {
			continue
		}
		result = append(result, sess)
		if len(result) >= limit {
			break
		}
	}
	return result
}

// get returns a session by its session ID, or nil if not found.
func (s *imSessionStore) get(id string) *imSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess.ID == id {
			return sess
		}
	}
	return nil
}

// getByCommand returns the session linked to a command ID, or nil.
func (s *imSessionStore) getByCommand(commandID string) *imSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess.CommandID == commandID {
			return sess
		}
	}
	return nil
}

// markSessionDone updates the session linked to commandID with status=done
// and stores the result text. Called from markCommandDone.
func (s *imSessionStore) markSessionDone(commandID, result string) {
	s.mu.Lock()
	var changed bool
	for _, sess := range s.sessions {
		if sess.CommandID == commandID {
			sess.Status = imStatusDone
			sess.Result = result
			now := time.Now()
			sess.UpdatedAt = now
			sess.DoneAt = &now
			changed = true
			break
		}
	}
	s.mu.Unlock()
	if changed {
		persistState()
	}
}

// markSessionFailed marks the session linked to commandID as failed with the
// given error message. Called from the callback retry worker when retries are
// exhausted or a dead-target error is detected.
func (s *imSessionStore) markSessionFailed(commandID, errMsg string) {
	s.mu.Lock()
	var changed bool
	for _, sess := range s.sessions {
		if sess.CommandID == commandID {
			sess.Status = imStatusFailed
			sess.Result = errMsg
			now := time.Now()
			sess.UpdatedAt = now
			sess.DoneAt = &now
			changed = true
			break
		}
	}
	s.mu.Unlock()
	if changed {
		persistState()
	}
}

// markSessionCallbackPending transitions the session to callback_pending,
// meaning the command has been removed from the pending queue (so the IM
// watcher will not re-fetch it) but the result has not yet been delivered to
// the IM platform. The callback retry worker will transition it to done or
// failed once delivery succeeds or exhausts retries.
func (s *imSessionStore) markSessionCallbackPending(commandID, result string) {
	s.mu.Lock()
	var changed bool
	for _, sess := range s.sessions {
		if sess.CommandID == commandID {
			sess.Status = imStatusCallbackPending
			sess.Result = result
			sess.UpdatedAt = time.Now()
			changed = true
			break
		}
	}
	s.mu.Unlock()
	if changed {
		persistState()
	}
}

// delete removes a session by its ID. Returns true if found and removed.
func (s *imSessionStore) delete(id string) bool {
	s.mu.Lock()
	found := false
	for i, sess := range s.sessions {
		if sess.ID == id {
			s.sessions = append(s.sessions[:i], s.sessions[i+1:]...)
			found = true
			break
		}
	}
	s.mu.Unlock()
	if found {
		persistState()
	}
	return found
}

// clear removes all sessions. Returns the count of removed sessions.
func (s *imSessionStore) clear() int {
	s.mu.Lock()
	n := len(s.sessions)
	s.sessions = s.sessions[:0]
	s.mu.Unlock()
	if n > 0 {
		persistState()
	}
	return n
}

// runCreateIMSession creates a new IM session for traceability.
// If the command has extra info (conversation_id, sender), it is
// automatically populated from the pending command when not explicitly provided.
func runCreateIMSession(platform, conversationID, senderID, senderName, commandID string, tags []string) (any, error) {
	// Auto-fill from the pending command's Extra if available
	cmd := queue.get(commandID)
	content := ""
	if cmd != nil {
		content = cmd.Content
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

	sess := sessionStore.create(platform, conversationID, senderID, senderName, commandID, content, tags)
	b, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal session: %w", err)
	}
	log.Printf("created IM session %s for command %s (platform=%s, conv=%s, sender=%s)",
		sess.ID, commandID, platform, conversationID, senderName)
	return string(b), nil
}

// runListIMSessions returns tracked IM sessions, newest first.
// statusFilter: "" (all) | "pending" | "processing" | "done" | "failed"
func runListIMSessions(statusFilter string, limit int) (any, error) {
	if limit <= 0 {
		limit = 100
	}
	sessions := sessionStore.list(statusFilter, limit)
	if len(sessions) == 0 {
		return "no IM sessions", nil
	}
	b, err := json.Marshal(sessions)
	if err != nil {
		return nil, fmt.Errorf("marshal sessions: %w", err)
	}
	return string(b), nil
}

// runGetIMSession returns a single IM session by its ID, including its
// linked command (if still in the queue) for full context.
func runGetIMSession(sessionID string) (any, error) {
	sess := sessionStore.get(sessionID)
	if sess == nil {
		return nil, fmt.Errorf("IM session %q not found", sessionID)
	}
	type sessionDetail struct {
		*imSession
		Command *pendingCommand `json:"command,omitempty"`
	}
	detail := sessionDetail{imSession: sess}
	if sess.CommandID != "" {
		detail.Command = queue.get(sess.CommandID)
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return nil, fmt.Errorf("marshal session detail: %w", err)
	}
	return string(b), nil
}

// runDeleteIMSession deletes a single IM session by ID.
func runDeleteIMSession(sessionID string) (any, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	found := sessionStore.delete(sessionID)
	if !found {
		return nil, fmt.Errorf("IM session %q not found", sessionID)
	}
	log.Printf("deleted IM session %s", sessionID)
	return map[string]any{"deleted": true, "session_id": sessionID}, nil
}

// runClearIMSessions removes all IM sessions and returns the count cleared.
func runClearIMSessions() (any, error) {
	n := sessionStore.clear()
	log.Printf("cleared all IM sessions (count=%d)", n)
	return map[string]any{"cleared": n}, nil
}

// --- persistence ---

// persistedState is the on-disk shape of the plugin's in-memory state.
// Kept simple so a future schema migration can just add fields.
type persistedState struct {
	NextID   int               `json:"next_id"`
	Sessions []*imSession      `json:"sessions"`
	Commands []*pendingCommand `json:"commands"`
}

// stateFile returns the path to the plugin's persistent state file:
//
//	~/.rexion/im-plugin/state.json
//
// The directory is created on first call.
func stateFile() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	dir := filepath.Join(home, ".rexion", "im-plugin")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "state.json")
}

// persistState snapshots the queue + session store to disk. Best-effort:
// a write failure is logged but does not interrupt the in-memory operation
// (the IM bot must keep running even if the disk is full).
func persistState() {
	state := persistedState{
		Sessions: sessionStore.snapshot(),
		Commands: queue.snapshot(),
	}
	if len(state.Sessions) > 0 {
		state.NextID = sessionStore.peekNextID()
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		log.Printf("persistState: marshal: %v", err)
		return
	}
	if err := os.WriteFile(stateFile(), b, 0o644); err != nil {
		log.Printf("persistState: write: %v", err)
	}
}

// loadState restores queue + session store from disk at startup. Missing or
// corrupt state files are silently ignored (fresh start).
func loadState() {
	b, err := os.ReadFile(stateFile())
	if err != nil {
		return // first run — nothing to load
	}
	var state persistedState
	if err := json.Unmarshal(b, &state); err != nil {
		log.Printf("loadState: parse %s: %v", stateFile(), err)
		return
	}
	queue.restore(state.Commands, state.NextID)
	sessionStore.restore(state.Sessions)
	log.Printf("loadState: restored %d sessions, %d commands from %s",
		len(state.Sessions), len(state.Commands), stateFile())
}

// snapshot/restore helpers keep the mutex discipline inside each store.

func (q *commandQueue) snapshot() []*pendingCommand {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]*pendingCommand, len(q.commands))
	copy(out, q.commands)
	return out
}

func (q *commandQueue) restore(cmds []*pendingCommand, nextID int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.commands = append(q.commands[:0], cmds...)
	if nextID > q.nextID {
		q.nextID = nextID
	}
}

func (s *imSessionStore) snapshot() []*imSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*imSession, len(s.sessions))
	copy(out, s.sessions)
	return out
}

func (s *imSessionStore) peekNextID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextID
}

func (s *imSessionStore) restore(sessions []*imSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = append(s.sessions[:0], sessions...)
	// Recompute nextID from restored sessions in case the file was edited.
	maxID := 0
	for _, sess := range s.sessions {
		if sess.ID == "" {
			continue
		}
		// IDs look like "im-sess-<n>-<hex>"; extract <n>.
		var n int
		if _, err := fmt.Sscanf(sess.ID, "im-sess-%d-", &n); err == nil && n > maxID {
			maxID = n
		}
	}
	if maxID > s.nextID {
		s.nextID = maxID
	}
}

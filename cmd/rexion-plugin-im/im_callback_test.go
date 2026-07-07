package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- SendResult classification tests ---

func TestClassifySendError(t *testing.T) {
	cases := []struct {
		err  string
		want string
	}{
		{"session webhook expired", "expired_webhook"},
		{"bot kicked from group", "dead_target"},
		{"chat not exist", "dead_target"},
		{"401 unauthorized", "auth_failed"},
		{"403 forbidden", "auth_failed"},
		{"429 rate limit exceeded", "rate_limited"},
		{"request timeout", "timeout"},
		{"some weird error", "unknown"},
	}
	for _, c := range cases {
		got := classifySendError(strError(c.err))
		if got != c.want {
			t.Errorf("classifySendError(%q) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestClassifyDingTalkErrorCode(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{0, ""},
		{310000, "dead_target"},
		{300001, "auth_failed"},
		{130101, "rate_limited"},
		{99999, "unknown"},
	}
	for _, c := range cases {
		got := classifyDingTalkErrorCode(c.code)
		if got != c.want {
			t.Errorf("classifyDingTalkErrorCode(%d) = %q, want %q", c.code, got, c.want)
		}
	}
}

func TestSendResultIsRetryable(t *testing.T) {
	if !(SendResult{ErrorKind: "timeout"}).isRetryable() {
		t.Error("timeout should be retryable")
	}
	if !(SendResult{ErrorKind: "expired_webhook"}).isRetryable() {
		t.Error("expired_webhook should be retryable")
	}
	if (SendResult{ErrorKind: "dead_target"}).isRetryable() {
		t.Error("dead_target should NOT be retryable")
	}
	if (SendResult{ErrorKind: "auth_failed"}).isRetryable() {
		t.Error("auth_failed should NOT be retryable")
	}
}

func TestSendResultIsDead(t *testing.T) {
	if !(SendResult{ErrorKind: "dead_target"}).isDead() {
		t.Error("dead_target should be dead")
	}
	if (SendResult{ErrorKind: "timeout"}).isDead() {
		t.Error("timeout should NOT be dead")
	}
}

// strError is a tiny helper to wrap a string as an error for testing.
type strError string

func (s strError) Error() string { return string(s) }

// --- Dead target registry tests ---

func TestDeadTargetRegistryMarkClear(t *testing.T) {
	d := &deadTargetRegistry{dead: make(map[string]time.Time)}
	if d.IsDead("dingtalk", "chat1") {
		t.Error("chat1 should not be dead initially")
	}
	d.MarkDead("dingtalk", "chat1")
	if !d.IsDead("dingtalk", "chat1") {
		t.Error("chat1 should be dead after MarkDead")
	}
	if d.IsDead("dingtalk", "chat2") {
		t.Error("chat2 should not be dead")
	}
	d.Clear("dingtalk", "chat1")
	if d.IsDead("dingtalk", "chat1") {
		t.Error("chat1 should not be dead after Clear")
	}
}

func TestDeadTargetRegistryEmptyChatID(t *testing.T) {
	d := &deadTargetRegistry{dead: make(map[string]time.Time)}
	d.MarkDead("dingtalk", "")
	if d.IsDead("dingtalk", "") {
		t.Error("empty chatID should never be marked dead")
	}
}

func TestDeadTargetRegistrySweep(t *testing.T) {
	d := &deadTargetRegistry{dead: make(map[string]time.Time)}
	d.MarkDead("dingtalk", "old")
	// Manually backdate the entry
	d.mu.Lock()
	d.dead["dingtalk:old"] = time.Now().Add(-25 * time.Hour)
	d.mu.Unlock()
	d.Sweep(24 * time.Hour)
	if d.IsDead("dingtalk", "old") {
		t.Error("old entry should be swept")
	}
}

// --- SessionWebhook cache tests ---

func TestSessionWebhookCacheRefreshLatest(t *testing.T) {
	c := &sessionWebhookCache{byChat: make(map[string]sessionWebhookEntry)}
	if latest := c.Latest("chat1"); latest != "" {
		t.Errorf("expected empty for unknown chat, got %q", latest)
	}
	c.Refresh("chat1", "https://session1.example.com")
	if got := c.Latest("chat1"); got != "https://session1.example.com" {
		t.Errorf("expected session1 URL, got %q", got)
	}
	// Refresh with newer URL
	c.Refresh("chat1", "https://session2.example.com")
	if got := c.Latest("chat1"); got != "https://session2.example.com" {
		t.Errorf("expected session2 URL after refresh, got %q", got)
	}
}

func TestSessionWebhookCacheExpiry(t *testing.T) {
	c := &sessionWebhookCache{byChat: make(map[string]sessionWebhookEntry)}
	c.Refresh("chat1", "https://session.example.com")
	// Backdate the entry to simulate expiry
	c.mu.Lock()
	e := c.byChat["chat1"]
	e.ExpireAt = time.Now().Add(-1 * time.Minute)
	c.byChat["chat1"] = e
	c.mu.Unlock()
	if got := c.Latest("chat1"); got != "" {
		t.Errorf("expected empty for expired entry, got %q", got)
	}
}

func TestSessionWebhookCacheEmptyArgs(t *testing.T) {
	c := &sessionWebhookCache{byChat: make(map[string]sessionWebhookEntry)}
	c.Refresh("", "https://session.example.com") // should be no-op
	c.Refresh("chat1", "")                       // should be no-op
	if got := c.Latest("chat1"); got != "" {
		t.Errorf("expected empty after no-op refresh, got %q", got)
	}
}

func TestSessionWebhookCacheSweep(t *testing.T) {
	c := &sessionWebhookCache{byChat: make(map[string]sessionWebhookEntry)}
	c.Refresh("chat1", "https://session.example.com")
	c.mu.Lock()
	e := c.byChat["chat1"]
	e.ExpireAt = time.Now().Add(-1 * time.Minute)
	c.byChat["chat1"] = e
	c.mu.Unlock()
	c.Sweep()
	c.mu.RLock()
	_, ok := c.byChat["chat1"]
	c.mu.RUnlock()
	if ok {
		t.Error("expired entry should be swept")
	}
}

// --- Command queue dedup tests ---

func TestCommandQueueDedup(t *testing.T) {
	q := &commandQueue{notify: make(chan struct{})}
	// First message with msg_id=m1
	cmd1 := q.add("dingtalk", "hello", "https://hook.example.com", map[string]string{"msg_id": "m1"})
	if cmd1 == nil {
		t.Fatal("first add should return non-nil command")
	}
	// Duplicate with same msg_id
	cmd2 := q.add("dingtalk", "hello again", "https://hook.example.com", map[string]string{"msg_id": "m1"})
	if cmd2 != nil && cmd2.ID != cmd1.ID {
		t.Errorf("duplicate msg_id should return same command, got %q vs %q", cmd2.ID, cmd1.ID)
	}
	// Different msg_id should be enqueued normally
	cmd3 := q.add("dingtalk", "second message", "https://hook.example.com", map[string]string{"msg_id": "m2"})
	if cmd3 == nil {
		t.Fatal("different msg_id should return non-nil command")
	}
	// Verify only 2 commands in queue
	pending := q.list(10)
	if len(pending) != 2 {
		t.Errorf("expected 2 pending commands after dedup, got %d", len(pending))
	}
}

func TestCommandQueueNoDedupWithoutMsgID(t *testing.T) {
	q := &commandQueue{notify: make(chan struct{})}
	q.add("wecom", "msg1", "", nil)
	q.add("wecom", "msg2", "", nil)
	pending := q.list(10)
	if len(pending) != 2 {
		t.Errorf("expected 2 pending without msg_id dedup, got %d", len(pending))
	}
}

// --- DingTalk URL detection tests ---

func TestIsDingTalkGroupRobotWebhook(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://oapi.dingtalk.com/robot/send?access_token=xxx", true},
		{"https://oapi.dingtalk.com/robot/sendBySession?session=xxx", false},
		{"https://example.com/some/random/url", false},
		{"", false},
	}
	for _, c := range cases {
		got := isDingTalkGroupRobotWebhook(c.url)
		if got != c.want {
			t.Errorf("isDingTalkGroupRobotWebhook(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

// --- DingTalk send via mock servers ---

func TestSendDingTalkSessionMessageSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()
	res := sendDingTalkSessionMessage(srv.URL, "hello", "text")
	if !res.Success {
		t.Errorf("expected success, got error: %s (%s)", res.Error, res.ErrorKind)
	}
}

func TestSendDingTalkSessionMessageExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errcode":310000,"errmsg":"session webhook expired"}`))
	}))
	defer srv.Close()
	res := sendDingTalkSessionMessage(srv.URL, "hello", "text")
	if res.Success {
		t.Error("expected failure for expired session webhook")
	}
	if res.ErrorKind != "expired_webhook" {
		t.Errorf("expected expired_webhook, got %s", res.ErrorKind)
	}
}

func TestSendDingTalkSessionMessageEmptyURL(t *testing.T) {
	res := sendDingTalkSessionMessage("", "hello", "text")
	if res.Success {
		t.Error("expected failure for empty URL")
	}
	if res.ErrorKind != "expired_webhook" {
		t.Errorf("expected expired_webhook, got %s", res.ErrorKind)
	}
}

func TestSendDingTalkGroupRobotWithSign(t *testing.T) {
	t.Setenv("IM_DINGTALK_SECRET", "testsecret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify that timestamp and sign were appended
		ts := r.URL.Query().Get("timestamp")
		sign := r.URL.Query().Get("sign")
		if ts == "" || sign == "" {
			t.Errorf("expected timestamp and sign in URL, got ts=%q sign=%q", ts, sign)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()
	res := sendDingTalkMessage(srv.URL+"?access_token=testtoken", "hello", "text")
	if !res.Success {
		t.Errorf("expected success, got: %s (%s)", res.Error, res.ErrorKind)
	}
}

func TestSendDingTalkReplyRouting(t *testing.T) {
	// SessionWebhook URL → no signing
	t.Setenv("IM_DINGTALK_SECRET", "testsecret")
	sessionHits := 0
	sessSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionHits++
		if r.URL.Query().Get("sign") != "" {
			t.Error("SessionWebhook should NOT be signed")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errcode":0}`))
	}))
	defer sessSrv.Close()
	res := sendDingTalkReply(sessSrv.URL, "hi", "text", true)
	if !res.Success {
		t.Errorf("session reply failed: %s", res.Error)
	}
	if sessionHits != 1 {
		t.Errorf("expected 1 session hit, got %d", sessionHits)
	}

	// Group-robot URL → signing
	robotHits := 0
	robotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		robotHits++
		if r.URL.Query().Get("sign") == "" {
			t.Error("group-robot webhook should be signed")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errcode":0}`))
	}))
	defer robotSrv.Close()
	res = sendDingTalkReply(robotSrv.URL+"?access_token=tok", "hi", "text", false)
	if !res.Success {
		t.Errorf("robot reply failed: %s", res.Error)
	}
	if robotHits != 1 {
		t.Errorf("expected 1 robot hit, got %d", robotHits)
	}
}

// --- mark_command_done tests ---

// TestRunMarkCommandDoneAlwaysMarksDoneOnFailure verifies the critical fix:
// even when the push fails, the command MUST be removed from the pending
// queue (marked done). Otherwise the IM watcher would re-fetch it and trigger
// an infinite agent-turn loop that exhausts memory.
func TestRunMarkCommandDoneAlwaysMarksDoneOnFailure(t *testing.T) {
	// Reset queue + retry queue
	oldQueue := queue
	q := &commandQueue{notify: make(chan struct{})}
	queue = q
	defer func() {
		queue = oldQueue
		// Clean up retry queue for other tests
		callbackRetry.mu.Lock()
		callbackRetry.tasks = nil
		callbackRetry.mu.Unlock()
	}()

	// A DingTalk stream-mode command whose SessionWebhook always fails.
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	cmd := q.add("dingtalk", "do something", failSrv.URL, map[string]string{
		"reply_mode": "stream",
		"msg_id":     "test-msg-fail",
	})

	// mark_command_done should succeed (command marked done) even though push failed.
	result, err := runMarkCommandDone(cmd.ID, "result text")
	if err != nil {
		t.Fatalf("expected no error (command marked done regardless of push), got: %v", err)
	}
	resultStr, ok := result.(string)
	if !ok || !strings.Contains(resultStr, "marked done") {
		t.Errorf("expected 'marked done' in result, got: %v", result)
	}

	// CRITICAL: command MUST be marked done to prevent IM watcher re-fetch loop.
	cmdAfter := q.get(cmd.ID)
	if cmdAfter == nil {
		t.Fatal("command should still exist in queue")
	}
	if !cmdAfter.Done {
		t.Error("CRITICAL: command MUST be marked done even when push fails, " +
			"otherwise IM watcher re-fetches it and causes infinite loop")
	}

	// The command must NOT appear in the pending list (what poll_commands returns).
	pending := q.list(10)
	for _, p := range pending {
		if p.ID == cmd.ID {
			t.Fatal("CRITICAL: failed-push command must NOT be in pending list " +
				"(would cause IM watcher to re-trigger agent turn)")
		}
	}

	// Retry queue should have the task for async delivery.
	if callbackRetry.PendingCount() == 0 {
		t.Error("expected the failed push to be enqueued for retry")
	}
}

func TestRunMarkCommandDoneSuccessMarksDone(t *testing.T) {
	oldQueue := queue
	q := &commandQueue{notify: make(chan struct{})}
	queue = q
	defer func() { queue = oldQueue }()

	// Success server
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errcode":0}`))
	}))
	defer okSrv.Close()

	cmd := q.add("dingtalk", "do something", okSrv.URL, map[string]string{
		"reply_mode": "stream",
		"msg_id":     "test-msg-ok",
	})

	result, err := runMarkCommandDone(cmd.ID, "done!")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !strings.Contains(result.(string), "marked done") {
		t.Errorf("unexpected result: %v", result)
	}
	cmdAfter := q.get(cmd.ID)
	if cmdAfter == nil || !cmdAfter.Done {
		t.Error("command should be marked done after successful push")
	}
}

// --- reply_message tool test ---

func TestRunReplyMessage(t *testing.T) {
	oldQueue := queue
	q := &commandQueue{notify: make(chan struct{})}
	queue = q
	defer func() { queue = oldQueue }()

	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errcode":0}`))
	}))
	defer okSrv.Close()

	cmd := q.add("dingtalk", "hello", okSrv.URL, map[string]string{
		"reply_mode": "stream",
		"msg_id":     "test-reply-1",
	})

	result, err := runReplyMessage(cmd.ID, "intermediate progress", "text")
	if err != nil {
		t.Fatalf("reply_message failed: %v", err)
	}
	if !strings.Contains(result.(string), "reply sent") {
		t.Errorf("unexpected result: %v", result)
	}
	// reply_message should NOT mark the command done
	cmdAfter := q.get(cmd.ID)
	if cmdAfter.Done {
		t.Error("reply_message should not mark command done")
	}
}

func TestRunReplyMessageNotFound(t *testing.T) {
	_, err := runReplyMessage("nonexistent-cmd", "result", "text")
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
}

// --- callback retry queue test ---

func TestCallbackRetryEnqueueNoDup(t *testing.T) {
	q := &callbackRetryQueue{stopCh: make(chan struct{}), done: make(chan struct{})}
	q.Enqueue("cmd-1", "result", "chat1", "dingtalk", strError("timeout"))
	q.Enqueue("cmd-1", "result", "chat1", "dingtalk", strError("timeout")) // duplicate
	if q.PendingCount() != 1 {
		t.Errorf("expected 1 task (no dup), got %d", q.PendingCount())
	}
}

func TestCallbackRetryDisabledNoop(t *testing.T) {
	t.Setenv("IM_CALLBACK_RETRY", "off")
	q := &callbackRetryQueue{stopCh: make(chan struct{}), done: make(chan struct{})}
	q.Enqueue("cmd-1", "result", "chat1", "dingtalk", strError("timeout"))
	if q.PendingCount() != 0 {
		t.Errorf("expected 0 tasks when disabled, got %d", q.PendingCount())
	}
	t.Setenv("IM_CALLBACK_RETRY", "on")
}

// --- concurrency smoke test for sessionWebhookCache ---

func TestSessionWebhookCacheConcurrentAccess(t *testing.T) {
	c := &sessionWebhookCache{byChat: make(map[string]sessionWebhookEntry)}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			chatID := "chat"
			if n%2 == 0 {
				chatID = "chat2"
			}
			c.Refresh(chatID, "https://session.example.com")
			_ = c.Latest(chatID)
		}(i)
	}
	wg.Wait()
	// Should not panic / race
}

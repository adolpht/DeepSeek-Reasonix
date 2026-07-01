package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// --- command queue tests ---

func TestCommandQueueAdd(t *testing.T) {
	q := &commandQueue{}
	cmd := q.add("wecom", "hello world", "https://example.com/hook", nil)
	if cmd.ID == "" {
		t.Error("expected non-empty command ID")
	}
	if cmd.Platform != "wecom" {
		t.Errorf("platform = %q, want wecom", cmd.Platform)
	}
	if cmd.Content != "hello world" {
		t.Errorf("content = %q, want hello world", cmd.Content)
	}
	if cmd.Done {
		t.Error("new command should not be done")
	}
}

func TestCommandQueueList(t *testing.T) {
	q := &commandQueue{}
	q.add("wecom", "cmd1", "", nil)
	q.add("feishu", "cmd2", "", nil)
	q.add("dingtalk", "cmd3", "", nil)

	pending := q.list(10)
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending, got %d", len(pending))
	}

	// Mark one done
	q.markDone(pending[0].ID)
	pending = q.list(10)
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending after marking one done, got %d", len(pending))
	}
}

func TestCommandQueueListLimit(t *testing.T) {
	q := &commandQueue{}
	for i := 0; i < 5; i++ {
		q.add("wecom", "cmd", "", nil)
	}
	pending := q.list(3)
	if len(pending) != 3 {
		t.Errorf("expected 3 with limit, got %d", len(pending))
	}
}

func TestCommandQueueMarkDoneNotFound(t *testing.T) {
	q := &commandQueue{}
	cmd := q.markDone("nonexistent")
	if cmd != nil {
		t.Error("expected nil for nonexistent command")
	}
}

func TestCommandQueueMarkDoneTwice(t *testing.T) {
	q := &commandQueue{}
	q.add("wecom", "cmd", "", nil)
	pending := q.list(1)
	id := pending[0].ID

	first := q.markDone(id)
	if first == nil {
		t.Error("first markDone should succeed")
	}
	second := q.markDone(id)
	if second != nil {
		t.Error("second markDone should return nil (already done)")
	}
}

// --- WeCom handler tests ---

func TestWeComHandlerText(t *testing.T) {
	q := &commandQueue{}
	oldQueue := queue
	queue = q
	defer func() { queue = oldQueue }()

	handler := makeWeComHandler("")

	payload := `{"msgtype":"text","text":{"content":"hello from wecom"}}`
	req := httptest.NewRequest(http.MethodPost, "/im/wecom", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	pending := q.list(10)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending command, got %d", len(pending))
	}
	if pending[0].Platform != "wecom" {
		t.Errorf("platform = %q, want wecom", pending[0].Platform)
	}
	if pending[0].Content != "hello from wecom" {
		t.Errorf("content = %q, want hello from wecom", pending[0].Content)
	}
}

func TestWeComHandlerTokenVerification(t *testing.T) {
	q := &commandQueue{}
	oldQueue := queue
	queue = q
	defer func() { queue = oldQueue }()

	handler := makeWeComHandler("mytoken")

	// Without token
	req := httptest.NewRequest(http.MethodPost, "/im/wecom", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", w.Code)
	}

	// With correct token
	req = httptest.NewRequest(http.MethodPost, "/im/wecom?token=mytoken", strings.NewReader(`{"msgtype":"text","text":{"content":"hi"}}`))
	w = httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", w.Code)
	}
}

func TestWeComHandlerMethodNotAllowed(t *testing.T) {
	handler := makeWeComHandler("")
	req := httptest.NewRequest(http.MethodGet, "/im/wecom", nil)
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", w.Code)
	}
}

func TestExtractWeComContent(t *testing.T) {
	cb := &weComCallback{MsgType: "text"}
	cb.Text.Content = "  hello  "
	if got := extractWeComContent(cb); got != "hello" {
		t.Errorf("text content = %q, want hello", got)
	}

	cb2 := &weComCallback{MsgType: "markdown"}
	cb2.Markdown.Content = "## title"
	if got := extractWeComContent(cb2); got != "## title" {
		t.Errorf("markdown content = %q, want ## title", got)
	}

	cb3 := &weComCallback{MsgType: "image"}
	if got := extractWeComContent(cb3); got != "" {
		t.Errorf("unknown msgtype should return empty, got %q", got)
	}
}

// --- Feishu handler tests ---

func TestFeishuHandlerText(t *testing.T) {
	q := &commandQueue{}
	oldQueue := queue
	queue = q
	defer func() { queue = oldQueue }()

	handler := makeFeishuHandler("")

	contentJSON, _ := json.Marshal(map[string]string{"text": "hello from feishu"})
	payload, _ := json.Marshal(map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"event_id":   "evt1",
			"event_type": "im.message.receive_v1",
		},
		"event": map[string]any{
			"message": map[string]any{
				"message_id": "msg1",
				"content":    string(contentJSON),
				"msg_type":   "text",
			},
			"sender": map[string]any{
				"sender_id": map[string]any{"open_id": "ou1"},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/im/feishu", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	pending := q.list(10)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending command, got %d", len(pending))
	}
	if pending[0].Platform != "feishu" {
		t.Errorf("platform = %q, want feishu", pending[0].Platform)
	}
	if pending[0].Content != "hello from feishu" {
		t.Errorf("content = %q, want hello from feishu", pending[0].Content)
	}
}

func TestFeishuHandlerChallenge(t *testing.T) {
	handler := makeFeishuHandler("")

	payload := `{"challenge":"abc123","token":"verification_token"}`
	req := httptest.NewRequest(http.MethodPost, "/im/feishu", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "abc123") {
		t.Errorf("challenge response should contain abc123, got %q", body)
	}
}

func TestExtractFeishuContent(t *testing.T) {
	cb := &feishuCallback{}
	cb.Event.Message.MsgType = "text"
	cb.Event.Message.Content = `{"text":"hello feishu"}`
	if got := extractFeishuContent(cb); got != "hello feishu" {
		t.Errorf("content = %q, want hello feishu", got)
	}

	// Empty content
	cb2 := &feishuCallback{}
	cb2.Event.Message.Content = ""
	if got := extractFeishuContent(cb2); got != "" {
		t.Errorf("empty content should return empty, got %q", got)
	}
}

// --- DingTalk handler tests ---

func TestDingTalkHandlerText(t *testing.T) {
	q := &commandQueue{}
	oldQueue := queue
	queue = q
	defer func() { queue = oldQueue }()

	handler := makeDingTalkHandler("")

	payload := `{"msgtype":"text","text":{"content":"hello from dingtalk"},"msgId":"msg1","senderStaffId":"user1","senderNick":"TestUser"}`
	req := httptest.NewRequest(http.MethodPost, "/im/dingtalk", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	pending := q.list(10)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending command, got %d", len(pending))
	}
	if pending[0].Platform != "dingtalk" {
		t.Errorf("platform = %q, want dingtalk", pending[0].Platform)
	}
	if pending[0].Content != "hello from dingtalk" {
		t.Errorf("content = %q, want hello from dingtalk", pending[0].Content)
	}
}

func TestDingTalkHandlerTokenVerification(t *testing.T) {
	q := &commandQueue{}
	oldQueue := queue
	queue = q
	defer func() { queue = oldQueue }()

	handler := makeDingTalkHandler("secret123")

	req := httptest.NewRequest(http.MethodPost, "/im/dingtalk", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/im/dingtalk?token=secret123", strings.NewReader(`{"msgtype":"text","text":{"content":"hi"}}`))
	w = httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", w.Code)
	}
}

func TestExtractDingTalkContent(t *testing.T) {
	cb := &dingTalkCallback{MsgType: "text"}
	cb.Text.Content = "  hello  "
	if got := extractDingTalkContent(cb); got != "hello" {
		t.Errorf("text content = %q, want hello", got)
	}

	cb2 := &dingTalkCallback{MsgType: "markdown"}
	cb2.Markdown.Text = "## title"
	if got := extractDingTalkContent(cb2); got != "## title" {
		t.Errorf("markdown content = %q, want ## title", got)
	}

	cb3 := &dingTalkCallback{MsgType: "rich_text"}
	if got := extractDingTalkContent(cb3); got != "" {
		t.Errorf("unknown msgtype should return empty, got %q", got)
	}
}

// --- DingTalk signature tests ---

func TestDingTalkSign(t *testing.T) {
	// Test that sign produces a non-empty base64 string
	sign := dingTalkSign(1609459200000, "testsecret")
	if sign == "" {
		t.Error("sign should not be empty")
	}
	// Same inputs should produce same output
	sign2 := dingTalkSign(1609459200000, "testsecret")
	if sign != sign2 {
		t.Errorf("sign should be deterministic: %q != %q", sign, sign2)
	}
	// Different timestamp should produce different sign
	sign3 := dingTalkSign(1609459200001, "testsecret")
	if sign == sign3 {
		t.Error("different timestamp should produce different sign")
	}
}

// --- bot server tests ---

func TestStartStopBot(t *testing.T) {
	// Reset global state
	botMu.Lock()
	if runningBot != nil {
		runningBot.server.Close()
		runningBot = nil
	}
	botMu.Unlock()

	// Start bot
	result, err := runStartBot(0, []string{"wecom", "feishu", "dingtalk"}, "")
	if err != nil {
		t.Fatalf("start bot: %v", err)
	}
	s, ok := result.(string)
	if !ok || !strings.Contains(s, "started") {
		t.Errorf("unexpected start result: %v", result)
	}

	// Wait for server to be ready
	botMu.Lock()
	port := runningBot.port
	botMu.Unlock()

	// Test health endpoint
	time.Sleep(100 * time.Millisecond)
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/im/health", port))
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want 200", resp.StatusCode)
	}

	// Stop bot
	result, err = runStopBot()
	if err != nil {
		t.Fatalf("stop bot: %v", err)
	}
	s, ok = result.(string)
	if !ok || !strings.Contains(s, "stopped") {
		t.Errorf("unexpected stop result: %v", result)
	}
}

func TestStartBotAlreadyRunning(t *testing.T) {
	// Reset global state
	botMu.Lock()
	if runningBot != nil {
		runningBot.server.Close()
		runningBot = nil
	}
	botMu.Unlock()

	// Start first bot
	_, err := runStartBot(0, []string{"wecom"}, "")
	if err != nil {
		t.Fatalf("start first bot: %v", err)
	}

	// Try to start second bot
	_, err = runStartBot(0, []string{"feishu"}, "")
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}

	// Clean up
	runStopBot()
}

func TestStopBotNotRunning(t *testing.T) {
	botMu.Lock()
	if runningBot != nil {
		runningBot.server.Close()
		runningBot = nil
	}
	botMu.Unlock()

	result, err := runStopBot()
	if err != nil {
		t.Errorf("stop non-running bot should not error: %v", err)
	}
	s, ok := result.(string)
	if !ok || !strings.Contains(s, "not running") {
		t.Errorf("unexpected result: %v", result)
	}
}

// --- MCP protocol end-to-end test ---

func TestMCPProtocolEndToEnd(t *testing.T) {
	// Reset global state
	botMu.Lock()
	if runningBot != nil {
		runningBot.server.Close()
		runningBot = nil
	}
	botMu.Unlock()

	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	send := func(method string, params any, id int) map[string]any {
		t.Helper()
		req := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  method,
		}
		if params != nil {
			req["params"] = params
		}
		b, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		b = append(b, '\n')
		if _, err := wIn.Write(b); err != nil {
			t.Fatal(err)
		}

		rOut.SetReadDeadline(time.Now().Add(5 * time.Second))
		defer rOut.SetReadDeadline(time.Time{})
		br := bufio.NewReader(rOut)
		line, err := br.ReadBytes('\n')
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		var resp map[string]any
		if err := json.Unmarshal(line, &resp); err != nil {
			t.Fatalf("unmarshal response %q: %v", string(line), err)
		}
		return resp
	}

	// 1. initialize
	resp := send("initialize", map[string]any{}, 1)
	if resp["error"] != nil {
		t.Fatalf("initialize error: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "reasonix-plugin-im" {
		t.Errorf("serverInfo.name = %v", info["name"])
	}

	// 2. tools/list
	resp = send("tools/list", nil, 2)
	if resp["error"] != nil {
		t.Fatalf("tools/list error: %v", resp["error"])
	}
	toolList := resp["result"].(map[string]any)["tools"].([]any)
	if len(toolList) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(toolList))
	}
	names := map[string]bool{}
	for _, tt := range toolList {
		names[tt.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"start_bot", "stop_bot", "send_message", "list_pending_commands", "mark_command_done"} {
		if !names[want] {
			t.Errorf("tool %q missing from tools/list", want)
		}
	}

	// 3. tools/call list_pending_commands (no pending)
	resp = send("tools/call", map[string]any{
		"name":      "list_pending_commands",
		"arguments": map[string]any{"limit": 10},
	}, 3)
	if resp["error"] != nil {
		t.Fatalf("list_pending_commands error: %v", resp["error"])
	}
	result = resp["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "no pending") {
		t.Errorf("expected 'no pending' message, got: %s", text)
	}

	// 4. tools/call unknown tool → error
	resp = send("tools/call", map[string]any{
		"name": "bogus",
	}, 4)
	if resp["error"] == nil {
		t.Error("expected error for unknown tool")
	}

	// Clean up
	wIn.Close()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit after stdin closed")
	}
}

// --- send_message dispatch test ---

func TestRunSendMessageUnsupportedPlatform(t *testing.T) {
	_, err := runSendMessage("slack", "https://example.com", "hello", "text")
	if err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Errorf("expected unsupported platform error, got: %v", err)
	}
}

// --- arg helper tests ---

func TestArgStringSliceDefault(t *testing.T) {
	args := map[string]any{
		"platforms": []any{"wecom", "feishu"},
	}
	got := argStringSliceDefault(args, "platforms", []string{"dingtalk"})
	if len(got) != 2 || got[0] != "wecom" || got[1] != "feishu" {
		t.Errorf("got %v, want [wecom feishu]", got)
	}

	// Missing key → default
	got = argStringSliceDefault(args, "missing", []string{"default"})
	if len(got) != 1 || got[0] != "default" {
		t.Errorf("missing key should return default, got %v", got)
	}

	// Empty array → default
	args2 := map[string]any{
		"platforms": []any{},
	}
	got = argStringSliceDefault(args2, "platforms", []string{"default"})
	if len(got) != 1 || got[0] != "default" {
		t.Errorf("empty array should return default, got %v", got)
	}
}

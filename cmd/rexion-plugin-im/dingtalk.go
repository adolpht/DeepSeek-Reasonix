package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// --- DingTalk (钉钉) ---

// dingTalkCallback represents the callback payload from DingTalk robot.
// Reference: https://open.dingtalk.com/document/robots/receive-message
type dingTalkCallback struct {
	MsgType    string `json:"msgtype"`
	Text       struct {
		Content string `json:"content"`
	} `json:"text"`
	Markdown struct {
		Text  string `json:"text"`
		Title string `json:"title"`
	} `json:"markdown"`
	MsgID      string `json:"msgId"`
	CreateTime int64  `json:"createAt"`
	ConversationID string `json:"conversationId"`
	SenderStaffID  string `json:"senderStaffId"`
	SenderNick     string `json:"senderNick"`
}

// dingTalkSendReq is the request body for sending a DingTalk webhook message.
type dingTalkSendReq struct {
	MsgType  string `json:"msgtype"`
	Text     *struct {
		Content string `json:"content"`
	} `json:"text,omitempty"`
	Markdown *struct {
		Text  string `json:"text"`
		Title string `json:"title"`
	} `json:"markdown,omitempty"`
	At *struct {
		AtUserIDs []string `json:"atUserIds,omitempty"`
		IsAtAll   bool     `json:"isAtAll,omitempty"`
	} `json:"at,omitempty"`
}

// makeDingTalkHandler creates an HTTP handler for DingTalk callback.
func makeDingTalkHandler(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Token verification
		if token != "" {
			qToken := r.URL.Query().Get("token")
			if qToken != token {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var cb dingTalkCallback
		if err := json.Unmarshal(body, &cb); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		content := extractDingTalkContent(&cb)
		if content == "" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `{"status":"ok"}`)
			return
		}

		// Build the webhook URL from environment key for response
		webhookURL := ""
		if key := envString("IM_DINGTALK_KEY", ""); key != "" {
			webhookURL = fmt.Sprintf("https://oapi.dingtalk.com/robot/send?access_token=%s", key)
		}

		queue.add("dingtalk", content, webhookURL, map[string]string{
			"msg_type":        cb.MsgType,
			"msg_id":          cb.MsgID,
			"sender_staff_id": cb.SenderStaffID,
			"sender_nick":     cb.SenderNick,
		})

		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	}
}

// extractDingTalkContent extracts the text content from a DingTalk callback.
func extractDingTalkContent(cb *dingTalkCallback) string {
	switch cb.MsgType {
	case "text":
		return strings.TrimSpace(cb.Text.Content)
	case "markdown":
		return strings.TrimSpace(cb.Markdown.Text)
	default:
		return ""
	}
}

// sendDingTalkMessage sends a message via a DingTalk group-robot webhook.
// Appends HMAC-SHA256 signature when IM_DINGTALK_SECRET is configured.
// Returns a structured SendResult so the caller can classify failures.
func sendDingTalkMessage(webhookURL, content, msgType string) SendResult {
	if webhookURL == "" {
		return newSendFailure(fmt.Errorf("empty webhook URL for DingTalk group robot"), "auth_failed")
	}
	// Append signature if secret is configured
	secret := envString("IM_DINGTALK_SECRET", "")
	if secret != "" {
		timestamp := time.Now().UnixMilli()
		sign := dingTalkSign(timestamp, secret)
		sep := "?"
		if strings.Contains(webhookURL, "?") {
			sep = "&"
		}
		webhookURL = fmt.Sprintf("%s%stimestamp=%d&sign=%s", webhookURL, sep, timestamp, sign)
	}

	req := dingTalkSendReq{MsgType: msgType}
	switch msgType {
	case "markdown":
		req.Markdown = &struct {
			Text  string `json:"text"`
			Title string `json:"title"`
		}{Text: content, Title: "Result"}
	default:
		req.Text = &struct {
			Content string `json:"content"`
		}{Content: content}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return wrapSendErr("marshal dingtalk request", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return wrapSendErr("send dingtalk message", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		kind := classifyHTTPStatus(resp.StatusCode)
		return newSendFailure(fmt.Errorf("dingtalk returned status %d: %s", resp.StatusCode, string(respBody)), kind)
	}

	// Check DingTalk-specific error in response body
	var respObj struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respBody, &respObj); err == nil && respObj.ErrCode != 0 {
		kind := classifyDingTalkErrorCode(respObj.ErrCode)
		return newSendFailure(fmt.Errorf("dingtalk error %d: %s", respObj.ErrCode, respObj.ErrMsg), kind)
	}

	return newSendSuccess("")
}

// classifyHTTPStatus maps an HTTP status code to an ErrorKind.
func classifyHTTPStatus(status int) string {
	switch {
	case status == 401 || status == 403:
		return "auth_failed"
	case status == 404:
		return "dead_target"
	case status == 429:
		return "rate_limited"
	case status >= 500:
		return "timeout"
	default:
		return "unknown"
	}
}

// dingTalkSign generates the HMAC-SHA256 signature for DingTalk webhook.
func dingTalkSign(timestamp int64, secret string) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// sendDingTalkSessionMessage replies via a SessionWebhook URL from Stream mode.
// Unlike the group robot webhook, SessionWebhook is already authenticated and
// must NOT be signed with HMAC (signing would corrupt the URL — this was the
// root cause of RC1.1 in the original send_message tool path).
// Returns a structured SendResult so the caller can classify failures.
func sendDingTalkSessionMessage(sessionWebhook, content, msgType string) SendResult {
	if sessionWebhook == "" {
		return newSendFailure(fmt.Errorf("empty SessionWebhook URL"), "expired_webhook")
	}
	req := dingTalkSendReq{MsgType: msgType}
	switch msgType {
	case "markdown":
		req.Markdown = &struct {
			Text  string `json:"text"`
			Title string `json:"title"`
		}{Text: content, Title: "Result"}
	default:
		req.Text = &struct {
			Content string `json:"content"`
		}{Content: content}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return wrapSendErr("marshal dingtalk session request", err)
	}

	resp, err := http.Post(sessionWebhook, "application/json", bytes.NewReader(body))
	if err != nil {
		kind := classifySendError(err)
		// Network errors on a SessionWebhook usually mean it expired.
		if kind == "unknown" {
			kind = "expired_webhook"
		}
		return newSendFailure(fmt.Errorf("send dingtalk session message: %w", err), kind)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		kind := classifyHTTPStatus(resp.StatusCode)
		return newSendFailure(fmt.Errorf("dingtalk session returned status %d: %s", resp.StatusCode, string(respBody)), kind)
	}

	var respObj struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respBody, &respObj); err == nil && respObj.ErrCode != 0 {
		kind := classifyDingTalkErrorCode(respObj.ErrCode)
		// SessionWebhook-specific: errcode 310000 / 400001 typically mean expired
		if kind == "dead_target" {
			kind = "expired_webhook"
		}
		return newSendFailure(fmt.Errorf("dingtalk session error %d: %s", respObj.ErrCode, respObj.ErrMsg), kind)
	}
	return newSendSuccess("")
}

// isDingTalkGroupRobotWebhook returns true when url looks like a group-robot
// webhook (contains access_token=), which must be signed. SessionWebhook URLs
// use /robot/sendBySession and must NOT be signed.
func isDingTalkGroupRobotWebhook(url string) bool {
	return strings.Contains(url, "access_token=") ||
		strings.Contains(url, "/robot/send?")
}

// sendDingTalkReply is the unified DingTalk send entry point. It routes to
// the group-robot path (signs when IM_DINGTALK_SECRET is set) or the
// SessionWebhook path (never signs) based on isSessionWebhook.
//
// When isSessionWebhook is false but the URL does not match the group-robot
// pattern, we conservatively treat it as a SessionWebhook (no signing) to
// avoid corrupting an already-authenticated URL.
func sendDingTalkReply(webhookURL, content, msgType string, isSessionWebhook bool) SendResult {
	if isSessionWebhook {
		return sendDingTalkSessionMessage(webhookURL, content, msgType)
	}
	if !isDingTalkGroupRobotWebhook(webhookURL) {
		// URL doesn't look like a group-robot webhook; treat as session webhook.
		return sendDingTalkSessionMessage(webhookURL, content, msgType)
	}
	return sendDingTalkMessage(webhookURL, content, msgType)
}

// runSendMessage dispatches a message to the specified IM platform.
// For DingTalk, it auto-detects whether webhookURL is a group-robot webhook
// (signs when IM_DINGTALK_SECRET is set) or a SessionWebhook (never signs),
// fixing the RC1.1 bug where Stream-mode replies were corrupted by signing.
func runSendMessage(platform, webhookURL, content, msgType string) (any, error) {
	switch platform {
	case "wecom":
		if err := sendWeComMessage(webhookURL, content, msgType); err != nil {
			return nil, err
		}
		return fmt.Sprintf("message sent to WeCom via %s", webhookURL), nil
	case "feishu":
		if err := sendFeishuMessage(webhookURL, content, msgType); err != nil {
			return nil, err
		}
		return fmt.Sprintf("message sent to Feishu via %s", webhookURL), nil
	case "dingtalk":
		// Auto-detect URL type: group-robot webhook gets signed, SessionWebhook does not.
		isSession := !isDingTalkGroupRobotWebhook(webhookURL)
		res := sendDingTalkReply(webhookURL, content, msgType, isSession)
		if !res.Success {
			return nil, fmt.Errorf("%s", res.Error)
		}
		mode := "group-robot"
		if isSession {
			mode = "session-webhook"
		}
		return fmt.Sprintf("message sent to DingTalk via %s (%s)", webhookURL, mode), nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s (supported: wecom, feishu, dingtalk)", platform)
	}
}

// runReplyMessage replies to a pending command by its ID, automatically
// resolving the correct webhook URL and reply mode (stream vs webhook).
// This is the recommended way for the agent to push results back — it only
// needs the command_id, avoiding the fragile manual webhook_url passing
// that caused RC1.2.
func runReplyMessage(commandID, result, msgType string) (any, error) {
	cmd := queue.get(commandID)
	if cmd == nil {
		return nil, fmt.Errorf("command %q not found", commandID)
	}
	if cmd.Done {
		return nil, fmt.Errorf("command %q already done", commandID)
	}
	res := pushResult(cmd, result, msgType)
	if !res.Success {
		return nil, fmt.Errorf("reply failed (%s): %s", res.ErrorKind, res.Error)
	}
	return fmt.Sprintf("reply sent to %s for command %s", cmd.Platform, commandID), nil
}

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

// sendDingTalkMessage sends a message via DingTalk webhook.
// Supports HMAC-SHA256 signature verification if IM_DINGTALK_SECRET is set.
func sendDingTalkMessage(webhookURL, content, msgType string) error {
	// Append signature if secret is configured
	secret := envString("IM_DINGTALK_SECRET", "")
	if secret != "" {
		timestamp := time.Now().UnixMilli()
		sign := dingTalkSign(timestamp, secret)
		sep := "&"
		if strings.Contains(webhookURL, "?") {
			sep = "&"
		} else {
			sep = "?"
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
		return fmt.Errorf("marshal dingtalk request: %w", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send dingtalk message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dingtalk returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Check DingTalk-specific error in response body
	var respObj struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respBody, &respObj); err == nil && respObj.ErrCode != 0 {
		return fmt.Errorf("dingtalk error %d: %s", respObj.ErrCode, respObj.ErrMsg)
	}

	return nil
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
// must NOT be signed with HMAC (signing would corrupt the URL).
func sendDingTalkSessionMessage(sessionWebhook, content, msgType string) error {
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
		return fmt.Errorf("marshal dingtalk session request: %w", err)
	}

	resp, err := http.Post(sessionWebhook, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send dingtalk session message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dingtalk session returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var respObj struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respBody, &respObj); err == nil && respObj.ErrCode != 0 {
		return fmt.Errorf("dingtalk session error %d: %s", respObj.ErrCode, respObj.ErrMsg)
	}
	return nil
}

// runSendMessage dispatches a message to the specified IM platform.
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
		if err := sendDingTalkMessage(webhookURL, content, msgType); err != nil {
			return nil, err
		}
		return fmt.Sprintf("message sent to DingTalk via %s", webhookURL), nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s (supported: wecom, feishu, dingtalk)", platform)
	}
}

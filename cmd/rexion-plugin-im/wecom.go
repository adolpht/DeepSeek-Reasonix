package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// --- WeCom (企业微信) ---

// weComCallback represents the callback payload from WeCom webhook.
// Reference: https://developer.work.weixin.qq.com/document/path/91770
type weComCallback struct {
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

// weComSendReq is the request body for sending a WeCom webhook message.
type weComSendReq struct {
	MsgType  string `json:"msgtype"`
	Text     *struct {
		Content string `json:"content"`
	} `json:"text,omitempty"`
	Markdown *struct {
		Content string `json:"content"`
	} `json:"markdown,omitempty"`
}

// makeWeComHandler creates an HTTP handler for WeCom callback.
func makeWeComHandler(token string) http.HandlerFunc {
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

		var cb weComCallback
		if err := json.Unmarshal(body, &cb); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		content := extractWeComContent(&cb)
		if content == "" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `{"status":"ok"}`)
			return
		}

		// Build the webhook URL from environment key for response
		webhookURL := ""
		if key := envString("IM_WECOM_KEY", ""); key != "" {
			webhookURL = fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s", key)
		}

		enqueueParsedCommand("wecom", content, webhookURL, map[string]string{
			"msg_type": cb.MsgType,
		})

		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	}
}

// extractWeComContent extracts the text content from a WeCom callback.
func extractWeComContent(cb *weComCallback) string {
	switch cb.MsgType {
	case "text":
		return strings.TrimSpace(cb.Text.Content)
	case "markdown":
		return strings.TrimSpace(cb.Markdown.Content)
	default:
		return ""
	}
}

// sendWeComMessage sends a message via WeCom webhook.
func sendWeComMessage(webhookURL, content, msgType string) error {
	req := weComSendReq{MsgType: msgType}
	switch msgType {
	case "markdown":
		req.Markdown = &struct {
			Content string `json:"content"`
		}{Content: content}
	default:
		req.Text = &struct {
			Content string `json:"content"`
		}{Content: content}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal wecom request: %w", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send wecom message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("wecom returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

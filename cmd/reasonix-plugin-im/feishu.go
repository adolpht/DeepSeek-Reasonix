package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// --- Feishu (飞书) ---

// feishuCallback represents the callback payload from Feishu bot.
// Reference: https://open.feishu.cn/document/ukTMukTMukTM/uYjNwUjL2YDM14iN2ATN
type feishuCallback struct {
	Schema string `json:"schema"`
	Header struct {
		EventID    string `json:"event_id"`
		EventType  string `json:"event_type"`
		CreateTime string `json:"create_time"`
		Token      string `json:"token"`
	} `json:"header"`
	Event struct {
		Sender struct {
			SenderID struct {
				OpenID string `json:"open_id"`
			} `json:"sender_id"`
		} `json:"sender"`
		Message struct {
			MessageID string `json:"message_id"`
			Content   string `json:"content"`
			MsgType   string `json:"msg_type"`
		} `json:"message"`
	} `json:"event"`
	Challenge string `json:"challenge,omitempty"`
}

// feishuSendReq is the request body for sending a Feishu webhook message.
type feishuSendReq struct {
	MsgType string `json:"msg_type"`
	Content string `json:"content"`
}

// makeFeishuHandler creates an HTTP handler for Feishu callback.
func makeFeishuHandler(token string) http.HandlerFunc {
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

		var cb feishuCallback
		if err := json.Unmarshal(body, &cb); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		// Handle URL verification challenge
		if cb.Challenge != "" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"challenge":"%s"}`, cb.Challenge)
			return
		}

		content := extractFeishuContent(&cb)
		if content == "" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `{"status":"ok"}`)
			return
		}

		// Build the webhook URL from environment key for response
		webhookURL := ""
		if key := envString("IM_FEISHU_KEY", ""); key != "" {
			webhookURL = fmt.Sprintf("https://open.feishu.cn/open-apis/bot/v2/hook/%s", key)
		}

		queue.add("feishu", content, webhookURL, map[string]string{
			"msg_type":   cb.Event.Message.MsgType,
			"message_id": cb.Event.Message.MessageID,
			"sender_id":  cb.Event.Sender.SenderID.OpenID,
		})

		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	}
}

// extractFeishuContent extracts the text content from a Feishu callback.
func extractFeishuContent(cb *feishuCallback) string {
	msgContent := cb.Event.Message.Content
	if msgContent == "" {
		return ""
	}

	// Feishu message content is a JSON-encoded string
	var contentObj struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(msgContent), &contentObj); err != nil {
		// If not JSON, treat as plain text
		return strings.TrimSpace(msgContent)
	}
	return strings.TrimSpace(contentObj.Text)
}

// sendFeishuMessage sends a message via Feishu webhook.
func sendFeishuMessage(webhookURL, content, msgType string) error {
	// Feishu expects content as a JSON string inside the content field
	contentJSON, err := json.Marshal(map[string]string{"text": content})
	if err != nil {
		return fmt.Errorf("marshal feishu content: %w", err)
	}

	if msgType == "markdown" {
		contentJSON, err = json.Marshal(map[string]string{"text": content})
		if err != nil {
			return fmt.Errorf("marshal feishu markdown content: %w", err)
		}
	}

	req := feishuSendReq{
		MsgType: msgType,
		Content: string(contentJSON),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal feishu request: %w", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send feishu message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("feishu returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

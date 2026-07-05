package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// --- Feishu long-connection mode (长连接,无需公网IP) ---

var (
	feishuStreamMu      sync.Mutex
	runningFeishuStream *feishuStreamState
)

type feishuStreamState struct {
	wsClient  *larkws.Client
	apiClient *lark.Client
	appID     string
	appSecret string
	cancel    context.CancelFunc
}

// runStartFeishuStream starts the Feishu WebSocket long-connection client.
// No public IP needed — the SDK opens a WSS connection TO Feishu's gateway.
//
// The Feishu SDK's Start(ctx) is blocking: it connects, spawns internal
// goroutines (pingLoop, receiveMessageLoop), then blocks forever on
// select{}. The SDK handles reconnection internally (autoReconnect defaults
// to true), so no external watchdog is needed. Start only returns on fatal
// errors (e.g. auth failure), at which point retrying would fail anyway.
func runStartFeishuStream(appID, appSecret string) (any, error) {
	if appID == "" || appSecret == "" {
		return nil, fmt.Errorf("app_id and app_secret are required (set IM_FEISHU_APP_ID / IM_FEISHU_APP_SECRET)")
	}

	feishuStreamMu.Lock()
	defer feishuStreamMu.Unlock()

	if runningFeishuStream != nil {
		return nil, fmt.Errorf("Feishu stream already running (app_id=%s)", runningFeishuStream.appID)
	}

	// REST API client for sending reply messages
	apiClient := lark.NewClient(appID, appSecret)

	// Event dispatcher — empty verification/encrypt keys (长连接模式无需验签)
	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			handleFeishuStreamMessage(event)
			return nil
		})

	wsClient := larkws.NewClient(appID, appSecret,
		larkws.WithEventHandler(eventHandler),
	)

	ctx, cancel := context.WithCancel(context.Background())

	// Start is blocking — run it in a goroutine. The SDK's internal
	// autoReconnect handles connection drops transparently.
	go func() {
		if err := wsClient.Start(ctx); err != nil {
			log.Printf("Feishu stream client exited with error: %v", err)
		}
	}()

	runningFeishuStream = &feishuStreamState{
		wsClient:  wsClient,
		apiClient: apiClient,
		appID:     appID,
		appSecret: appSecret,
		cancel:    cancel,
	}

	log.Printf("Feishu stream started (app_id=%s), no public IP required", appID)
	return fmt.Sprintf("Feishu stream started (app_id=%s), receiving messages via WebSocket — no public IP needed", appID), nil
}

// runStopFeishuStream stops the Feishu WebSocket client.
// The SDK's Close() sets autoReconnect=false and disconnects, preventing
// internal reconnection.
func runStopFeishuStream() (any, error) {
	feishuStreamMu.Lock()
	defer feishuStreamMu.Unlock()

	if runningFeishuStream == nil {
		return "Feishu stream is not running", nil
	}

	runningFeishuStream.cancel()
	// wsClient.Close() sets autoReconnect=false then disconnects — safe to
	// call from outside the Start goroutine.
	runningFeishuStream.wsClient.Close()
	appID := runningFeishuStream.appID
	runningFeishuStream = nil
	log.Printf("Feishu stream stopped (app_id=%s)", appID)
	return fmt.Sprintf("Feishu stream stopped (app_id=%s)", appID), nil
}

// handleFeishuStreamMessage enqueues an incoming Feishu message from long-connection mode.
// Stores the message_id in Extra for later REST API reply.
func handleFeishuStreamMessage(event *larkim.P2MessageReceiveV1) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return
	}
	msg := event.Event.Message

	// EventMessage fields are *string — dereference safely
	msgType := derefStr(msg.MessageType)
	messageID := derefStr(msg.MessageId)
	chatID := derefStr(msg.ChatId)
	chatType := derefStr(msg.ChatType)
	contentRaw := derefStr(msg.Content)

	if msgType != "text" {
		log.Printf("Feishu stream: skipping non-text message (type=%s, id=%s)", msgType, messageID)
		return
	}

	// Content is JSON like {"text":"hello"}
	content := extractFeishuTextContent(contentRaw)
	if content == "" {
		return
	}

	senderID := ""
	if event.Event.Sender != nil && event.Event.Sender.SenderId != nil {
		senderID = derefStr(event.Event.Sender.SenderId.OpenId)
	}

	queue.add("feishu", content, "", map[string]string{
		"reply_mode": "stream",
		"message_id": messageID,
		"chat_id":    chatID,
		"chat_type":  chatType,
		"sender_id":  senderID,
	})
	log.Printf("Feishu stream: queued msg %q from chat=%s (sender=%s)", content, chatID, senderID)
}

// derefStr safely dereferences a *string, returning "" for nil.
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// extractFeishuTextContent parses the JSON content field {"text":"..."}.
func extractFeishuTextContent(raw string) string {
	if raw == "" {
		return ""
	}
	var obj struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		// Fallback: try as plain string
		return strings.TrimSpace(raw)
	}
	return strings.TrimSpace(obj.Text)
}

// replyFeishuStream sends a reply to a Feishu message via REST API.
// Uses the message_id stored when the command was enqueued.
func replyFeishuStream(messageID, text string) error {
	feishuStreamMu.Lock()
	apiClient := runningFeishuStream.apiClient
	feishuStreamMu.Unlock()

	if apiClient == nil {
		return fmt.Errorf("Feishu stream not running, cannot reply")
	}

	content, _ := json.Marshal(map[string]string{"text": text})
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("text").
			Content(string(content)).
			Build()).
		Build()

	resp, err := apiClient.Im.Message.Reply(context.Background(), req)
	if err != nil {
		return fmt.Errorf("Feishu reply API: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("Feishu reply failed: code=%d, msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

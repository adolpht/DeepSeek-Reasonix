package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/payload"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/utils"
)

// --- DingTalk Stream mode (长连接,无需公网IP) ---

// dingTalkStream holds the running DingTalk Stream client.
var (
	dingStreamMu      sync.Mutex
	runningDingStream *dingStreamState
)

type dingStreamState struct {
	cli       *client.StreamClient
	clientID  string
	clientSec string
	cancel    context.CancelFunc
}

// runStartDingTalkStream starts the DingTalk Stream client.
// No public IP needed — the SDK opens a WebSocket TO DingTalk's gateway.
//
// The DingTalk SDK's Start() is non-blocking: it establishes the WebSocket
// connection, spawns an internal processLoop goroutine, and returns nil
// immediately. The SDK handles reconnection internally when
// WithAutoReconnect(true) is set, so no external watchdog is needed.
func runStartDingTalkStream(clientID, clientSecret string) (any, error) {
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required (set IM_DINGTALK_APP_KEY / IM_DINGTALK_APP_SECRET)")
	}

	dingStreamMu.Lock()
	defer dingStreamMu.Unlock()

	if runningDingStream != nil {
		return nil, fmt.Errorf("DingTalk stream already running (client_id=%s)", runningDingStream.clientID)
	}

	ctx, cancel := context.WithCancel(context.Background())

	botHandler := chatbot.NewDefaultChatBotFrameHandler(func(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
		handleDingTalkStreamMessage(data)
		return []byte{}, nil
	})

	cli := client.NewStreamClient(
		client.WithAppCredential(client.NewAppCredentialConfig(clientID, clientSecret)),
		client.WithAutoReconnect(true),
		client.WithSubscription(
			utils.SubscriptionTypeKCallback,
			payload.BotMessageCallbackTopic,
			botHandler.OnEventReceived,
		),
	)

	// Start is synchronous for connection establishment: it returns nil after
	// the WebSocket is open and processLoop is spawned. An error means the
	// connection could not be established at all.
	if err := cli.Start(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("start DingTalk stream: %w", err)
	}

	runningDingStream = &dingStreamState{
		cli:       cli,
		clientID:  clientID,
		clientSec: clientSecret,
		cancel:    cancel,
	}

	log.Printf("DingTalk stream started (client_id=%s), no public IP required", clientID)
	return fmt.Sprintf("DingTalk stream started (client_id=%s), receiving messages via WebSocket — no public IP needed", clientID), nil
}

// runStopDingTalkStream stops the DingTalk Stream client.
// AutoReconnect is disabled before Close() to prevent the SDK from
// immediately re-establishing the connection.
func runStopDingTalkStream() (any, error) {
	dingStreamMu.Lock()
	defer dingStreamMu.Unlock()

	if runningDingStream == nil {
		return "DingTalk stream is not running", nil
	}

	// Disable auto-reconnect before closing so the SDK's internal processLoop
	// doesn't trigger a reconnect after we close the connection.
	runningDingStream.cli.AutoReconnect = false
	runningDingStream.cancel()
	runningDingStream.cli.Close()
	clientID := runningDingStream.clientID
	runningDingStream = nil
	log.Printf("DingTalk stream stopped (client_id=%s)", clientID)
	return fmt.Sprintf("DingTalk stream stopped (client_id=%s)", clientID), nil
}

// handleDingTalkStreamMessage enqueues an incoming DingTalk message from Stream mode.
// Stores the SessionWebhook URL (valid ~2h) as WebhookURL for later reply,
// and refreshes the chat_id -> URL cache so that long-running tasks whose
// original SessionWebhook expired can still reply via a fresher URL from any
// later message in the same chat (B2 fix for RC2.4).
func handleDingTalkStreamMessage(data *chatbot.BotCallbackDataModel) {
	if data == nil {
		return
	}
	content := data.Text.Content
	if content == "" {
		log.Printf("DingTalk stream: empty content, msg_id=%s, msgtype=%s", data.MsgId, data.Msgtype)
		return
	}

	// Refresh the SessionWebhook cache for this chat so later retries can
	// pick up a fresher URL even if the original command's URL expires.
	dingWebhookCache.Refresh(data.ConversationId, data.SessionWebhook)

	enqueueParsedCommand("dingtalk", content, data.SessionWebhook, map[string]string{
		"reply_mode":        "stream",
		"msg_id":            data.MsgId,
		"sender_staff_id":   data.SenderStaffId,
		"sender_nick":       data.SenderNick,
		"conversation_id":   data.ConversationId,
		"conversation_type": data.ConversationType,
	})
	log.Printf("DingTalk stream: queued msg %q from %s (sender=%s)", content, data.ConversationId, data.SenderNick)
}

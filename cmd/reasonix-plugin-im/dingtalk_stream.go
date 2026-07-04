package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/utils"
)

// --- DingTalk Stream mode (长连接,无需公网IP) ---

// dingTalkStream holds the running DingTalk Stream client.
var (
	dingStreamMu    sync.Mutex
	runningDingStream *dingStreamState
)

type dingStreamState struct {
	cli       *client.StreamClient
	clientID  string
	cancel    context.CancelFunc
}

// runStartDingTalkStream starts the DingTalk Stream client.
// No public IP needed — the SDK opens a WebSocket TO DingTalk's gateway.
func runStartDingTalkStream(clientID, clientSecret string) (any, error) {
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("client_id and client_secret are required (set IM_DINGTALK_APP_KEY / IM_DINGTALK_APP_SECRET)")
	}

	dingStreamMu.Lock()
	defer dingStreamMu.Unlock()

	if runningDingStream != nil {
		return nil, fmt.Errorf("DingTalk stream already running (client_id=%s)", runningDingStream.clientID)
	}

	botHandler := chatbot.NewDefaultChatBotFrameHandler(func(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
		handleDingTalkStreamMessage(data)
		return []byte{}, nil
	})
	cli := client.NewStreamClient(
		client.WithAppCredential(client.NewAppCredentialConfig(clientID, clientSecret)),
		client.WithAutoReconnect(true),
		client.WithSubscription(
			utils.SubscriptionTypeKCallback,
			"/v1.0/im/bot/messages/get",
			botHandler.OnEventReceived,
		),
	)

	ctx, cancel := context.WithCancel(context.Background())
	// Start is blocking — run in goroutine
	startErr := make(chan error, 1)
	go func() {
		err := cli.Start(ctx)
		if err != nil {
			startErr <- err
		}
	}()

	// Brief wait for immediate startup errors
	select {
	case err := <-startErr:
		cancel()
		return nil, fmt.Errorf("start DingTalk stream: %w", err)
	default:
	}

	runningDingStream = &dingStreamState{
		cli:      cli,
		clientID: clientID,
		cancel:   cancel,
	}
	log.Printf("DingTalk stream started (client_id=%s), no public IP required", clientID)
	return fmt.Sprintf("DingTalk stream started (client_id=%s), receiving messages via WebSocket — no public IP needed", clientID), nil
}

// runStopDingTalkStream stops the DingTalk Stream client.
func runStopDingTalkStream() (any, error) {
	dingStreamMu.Lock()
	defer dingStreamMu.Unlock()

	if runningDingStream == nil {
		return "DingTalk stream is not running", nil
	}

	runningDingStream.cancel()
	runningDingStream.cli.Close()
	clientID := runningDingStream.clientID
	runningDingStream = nil
	log.Printf("DingTalk stream stopped (client_id=%s)", clientID)
	return fmt.Sprintf("DingTalk stream stopped (client_id=%s)", clientID), nil
}

// handleDingTalkStreamMessage enqueues an incoming DingTalk message from Stream mode.
// Stores the SessionWebhook URL (valid ~2h) as WebhookURL for later reply.
func handleDingTalkStreamMessage(data *chatbot.BotCallbackDataModel) {
	if data == nil {
		return
	}
	content := data.Text.Content
	if content == "" {
		log.Printf("DingTalk stream: empty content, msg_id=%s, msgtype=%s", data.MsgId, data.Msgtype)
		return
	}

	queue.add("dingtalk", content, data.SessionWebhook, map[string]string{
		"reply_mode":       "stream",
		"msg_id":           data.MsgId,
		"sender_staff_id":  data.SenderStaffId,
		"sender_nick":      data.SenderNick,
		"conversation_id":  data.ConversationId,
		"conversation_type": data.ConversationType,
	})
	log.Printf("DingTalk stream: queued msg %q from %s (sender=%s)", content, data.ConversationId, data.SenderNick)
}

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
	"sync"
	"time"
)

// --- command queue ---

// pendingCommand represents a remote command received from an IM platform.
type pendingCommand struct {
	ID        string            `json:"id"`
	Platform  string            `json:"platform"`
	Content   string            `json:"content"`
	WebhookURL string           `json:"webhook_url,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
	ReceivedAt time.Time        `json:"received_at"`
	Done      bool              `json:"done"`
}

// commandQueue is an in-memory queue of pending commands, protected by a mutex.
type commandQueue struct {
	mu       sync.Mutex
	commands []*pendingCommand
	nextID   int
}

var queue = &commandQueue{}

func (q *commandQueue) add(platform, content, webhookURL string, extra map[string]string) *pendingCommand {
	q.mu.Lock()
	defer q.mu.Unlock()
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
	log.Printf("queued command %s from %s: %q", id, platform, content)
	return cmd
}

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

func (q *commandQueue) markDone(id string) *pendingCommand {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, cmd := range q.commands {
		if cmd.ID == id && !cmd.Done {
			cmd.Done = true
			return cmd
		}
	}
	return nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// --- bot server ---

// botServer holds the state of the running HTTP bot server.
type botServer struct {
	server   *http.Server
	port     int
	token    string
	platforms []string
}

var (
	botMu     sync.Mutex
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

	// Listen on the specified port
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("listen on port %d: %w", port, err)
	}

	runningBot = &botServer{
		server:    srv,
		port:      port,
		token:     token,
		platforms: platforms,
	}

	// Start serving in a goroutine
	go func() {
		log.Printf("bot server listening on :%d, platforms: %v", port, platforms)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("bot server error: %v", err)
		}
		botMu.Lock()
		runningBot = nil
		botMu.Unlock()
	}()

	return fmt.Sprintf("IM bot started on port %d, platforms: %v", port, platforms), nil
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
func runMarkCommandDone(commandID, result string) (any, error) {
	cmd := queue.markDone(commandID)
	if cmd == nil {
		return nil, fmt.Errorf("command %q not found or already done", commandID)
	}

	// Push result back to the originating platform
	if cmd.WebhookURL != "" {
		if err := pushResult(cmd.Platform, cmd.WebhookURL, result); err != nil {
			return nil, fmt.Errorf("command marked done but failed to push result: %w", err)
		}
		return fmt.Sprintf("command %s marked done, result pushed to %s", commandID, cmd.Platform), nil
	}

	return fmt.Sprintf("command %s marked done (no webhook URL to push result)", commandID), nil
}

// pushResult sends the execution result back to the originating IM platform.
func pushResult(platform, webhookURL, result string) error {
	switch platform {
	case "wecom":
		return sendWeComMessage(webhookURL, result, "text")
	case "feishu":
		return sendFeishuMessage(webhookURL, result, "text")
	case "dingtalk":
		return sendDingTalkMessage(webhookURL, result, "text")
	default:
		return fmt.Errorf("unsupported platform: %s", platform)
	}
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

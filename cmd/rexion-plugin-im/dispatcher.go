package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// confirmTimeout is how long the dispatcher waits for a user to confirm a
// sensitive command before auto-cancelling it.
const confirmTimeout = 5 * time.Minute

// CommandDispatcher orchestrates the routing of IM commands to the agent
// based on their WorkMode. It handles the confirmation flow for sensitive
// commands: when ConfirmRequired is set, the dispatcher sends a confirmation
// request back to the IM platform and holds the command until the user
// replies 确认/取消 (or the 5-minute timeout expires).
type CommandDispatcher struct {
	confirm *ConfirmDispatcher
}

// confirmEntry tracks a command awaiting user confirmation.
type confirmEntry struct {
	CommandID string
	Platform  string
	SessionKey string
	CreatedAt time.Time
}

// ConfirmDispatcher tracks pending confirmation requests and their timeouts.
// It complements sessionStore: the sessionStore links a command to its
// processing state, while ConfirmDispatcher links a session key to the
// command currently awaiting confirmation in that session, so an incoming
// "确认"/"取消" reply can be matched without requiring the command ID.
type ConfirmDispatcher struct {
	mu      sync.Mutex
	pending map[string]*confirmEntry // commandID -> entry
	// bySession maps a session key to the command currently awaiting
	// confirmation in that session.
	bySession map[string]string
}

var (
	confirmDispatcher = &ConfirmDispatcher{
		pending:   make(map[string]*confirmEntry),
		bySession: make(map[string]string),
	}
	commandDispatcher = &CommandDispatcher{confirm: confirmDispatcher}
)

// Dispatch is called after a command is enqueued. It applies the execution
// strategy based on WorkMode and handles the confirmation flow for sensitive
// commands.
//
// ModeAsk: the command is flagged for direct reply — the agent should answer
// without performing write operations.
// ModePlan: the command is made available for the agent, which should plan
// before executing (default).
// ModeCraft: the command is made available for immediate execution.
//
// When ConfirmRequired is true, a confirmation request is pushed to the IM
// platform and the command is held: queue.list() skips held commands so the
// agent won't poll it until the user confirms. On confirmation the hold is
// released; on cancellation or timeout it is marked done with a "cancelled"
// result.
func (d *CommandDispatcher) Dispatch(cmd *pendingCommand) error {
	if cmd == nil {
		return fmt.Errorf("nil command")
	}

	// Lazily sweep expired confirmations on every dispatch.
	d.confirm.sweepTimeouts()

	switch cmd.Mode {
	case string(ModeAsk):
		log.Printf("dispatch: command %s mode=ask (direct reply, no writes)", cmd.ID)
	case string(ModePlan):
		log.Printf("dispatch: command %s mode=plan (plan then execute)", cmd.ID)
	case string(ModeCraft):
		log.Printf("dispatch: command %s mode=craft (direct execute)", cmd.ID)
	default:
		log.Printf("dispatch: command %s mode=%s", cmd.ID, cmd.Mode)
	}

	if !cmd.ConfirmRequired {
		return nil
	}

	// Sensitive command — send confirmation request and hold.
	confirmPrompt := fmt.Sprintf("⚠️ 检测到敏感操作，请回复「确认」执行或「取消」放弃（5 分钟内未回复将自动取消）。\n任务: %s", cmd.Content)
	if res := pushResult(cmd, confirmPrompt, "text"); !res.Success {
		log.Printf("dispatch: failed to send confirmation prompt for %s: %s", cmd.ID, res.Error)
		return fmt.Errorf("send confirmation prompt: %s", res.Error)
	}

	sessionKey := commandSessionKey(cmd)
	d.confirm.register(cmd.ID, sessionKey, cmd.Platform)
	log.Printf("dispatch: command %s awaiting confirmation (session=%s)", cmd.ID, sessionKey)
	return nil
}

// Resolve handles a user's confirmation reply. confirmed=true releases the
// command for agent processing; confirmed=false cancels it. Returns the
// resolved command ID and whether a pending confirmation was found.
func (d *CommandDispatcher) Resolve(sessionKey string, confirmed bool) (string, bool) {
	return d.confirm.resolve(sessionKey, confirmed)
}

// ResolveByID resolves a pending confirmation by command ID (used by the
// confirm_im_command tool when the agent has a command_id rather than a
// session key).
func (d *CommandDispatcher) ResolveByID(commandID string, confirmed bool) (string, bool) {
	return d.confirm.resolveByID(commandID, confirmed)
}

// commandSessionKey derives the session key for a pending command from its
// Extra field (conversation_id / sender_id).
func commandSessionKey(cmd *pendingCommand) string {
	if cmd == nil {
		return ""
	}
	conv, sender := extraConvSender(cmd.Extra)
	return sessionModeKey(cmd.Platform, conv, sender)
}

// register records a command as awaiting confirmation.
func (c *ConfirmDispatcher) register(commandID, sessionKey, platform string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending[commandID] = &confirmEntry{
		CommandID:  commandID,
		Platform:   platform,
		SessionKey: sessionKey,
		CreatedAt:  time.Now(),
	}
	if sessionKey != "" {
		c.bySession[sessionKey] = commandID
	}
}

// IsHeld returns true when a command is currently awaiting confirmation and
// should be skipped by queue.list() (the agent must not poll it yet).
func (c *ConfirmDispatcher) IsHeld(commandID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.pending[commandID]
	return ok
}

// resolve matches a session's reply to a pending confirmation and either
// releases or cancels the command.
func (c *ConfirmDispatcher) resolve(sessionKey string, confirmed bool) (string, bool) {
	c.mu.Lock()
	commandID, ok := c.bySession[sessionKey]
	if !ok {
		c.mu.Unlock()
		return "", false
	}
	entry, exists := c.pending[commandID]
	if !exists {
		delete(c.bySession, sessionKey)
		c.mu.Unlock()
		return "", false
	}
	delete(c.pending, commandID)
	delete(c.bySession, sessionKey)
	c.mu.Unlock()

	if !confirmed {
		queue.markDone(commandID, "用户取消执行（敏感操作未确认）")
		sessionStore.markSessionFailed(commandID, "用户取消执行（敏感操作未确认）")
		log.Printf("confirm: command %s cancelled by user", commandID)
		return commandID, true
	}
	// Confirmed — the command is still in the queue (we never removed it).
	// Clear the confirm flag and release the hold so queue.list() returns it.
	if cmd := queue.get(commandID); cmd != nil {
		cmd.ConfirmRequired = false
	}
	log.Printf("confirm: command %s confirmed, released for execution", commandID)
	_ = entry
	return commandID, true
}

// resolveByID resolves a pending confirmation by command ID instead of
// session key (used by the confirm_im_command tool).
func (c *ConfirmDispatcher) resolveByID(commandID string, confirmed bool) (string, bool) {
	c.mu.Lock()
	entry, ok := c.pending[commandID]
	if !ok {
		c.mu.Unlock()
		return "", false
	}
	delete(c.pending, commandID)
	if entry.SessionKey != "" {
		delete(c.bySession, entry.SessionKey)
	}
	c.mu.Unlock()

	if !confirmed {
		queue.markDone(commandID, "用户取消执行（敏感操作未确认）")
		sessionStore.markSessionFailed(commandID, "用户取消执行（敏感操作未确认）")
		log.Printf("confirm: command %s cancelled by user (by ID)", commandID)
		return commandID, true
	}
	if cmd := queue.get(commandID); cmd != nil {
		cmd.ConfirmRequired = false
	}
	log.Printf("confirm: command %s confirmed, released for execution (by ID)", commandID)
	return commandID, true
}

// sweepTimeouts cancels any confirmation that has exceeded the timeout.
// Called lazily from register/resolve/Dispatch so no separate goroutine is
// needed.
func (c *ConfirmDispatcher) sweepTimeouts() {
	c.mu.Lock()
	cutoff := time.Now().Add(-confirmTimeout)
	var toCancel []string
	for id, entry := range c.pending {
		if entry.CreatedAt.Before(cutoff) {
			toCancel = append(toCancel, id)
		}
	}
	c.mu.Unlock()
	for _, id := range toCancel {
		c.cancelTimeout(id)
	}
}

// cancelTimeout marks a timed-out confirmation as cancelled.
func (c *ConfirmDispatcher) cancelTimeout(commandID string) {
	c.mu.Lock()
	entry, ok := c.pending[commandID]
	if !ok {
		c.mu.Unlock()
		return
	}
	delete(c.pending, commandID)
	if entry.SessionKey != "" {
		delete(c.bySession, entry.SessionKey)
	}
	c.mu.Unlock()
	queue.markDone(commandID, "确认超时自动取消（5 分钟未响应）")
	sessionStore.markSessionFailed(commandID, "确认超时自动取消（5 分钟未响应）")
	log.Printf("confirm: command %s auto-cancelled (timeout)", commandID)
}

// IsConfirmationReply checks whether text is a user's confirmation or
// cancellation reply. Returns (isReply, confirmed).
//   - "确认", "确定", "yes", "ok" → (true, true)
//   - "取消", "放弃", "no", "cancel" → (true, false)
//
// Matching is case-insensitive and tolerates trailing punctuation (。!！).
func IsConfirmationReply(text string) (bool, bool) {
	t := strings.TrimSpace(strings.ToLower(text))
	t = strings.TrimRight(t, "。.!！?？")
	switch t {
	case "确认", "确定", "yes", "ok", "y":
		return true, true
	case "取消", "放弃", "no", "n", "cancel":
		return true, false
	}
	return false, false
}

// enqueueParsedCommand handles the full flow of parsing an IM message,
// resolving session mode, checking for confirmation replies, enqueuing, and
// dispatching. Returns the enqueued command, or nil if the message was
// consumed as a confirmation reply or was empty.
//
// This is the shared entry point used by all platform handlers (webhook and
// stream) so that command parsing / mode resolution / confirmation handling
// stay consistent across platforms.
func enqueueParsedCommand(platform, content, webhookURL string, extra map[string]string) *pendingCommand {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	conv, sender := extraConvSender(extra)
	sessionKey := sessionModeKey(platform, conv, sender)

	// Lazily sweep expired confirmations.
	confirmDispatcher.sweepTimeouts()

	// Check for confirmation reply — if it resolves a pending confirmation,
	// the message is consumed (not enqueued as a new command).
	if isReply, confirmed := IsConfirmationReply(content); isReply {
		if _, ok := commandDispatcher.Resolve(sessionKey, confirmed); ok {
			return nil
		}
		// No pending confirmation — fall through to enqueue as a normal command.
	}

	// Parse and apply session-level mode persistence.
	pc := ParseCommand(content)
	pc.ResolveMode(sessionKey)

	cmd := queue.add(platform, content, webhookURL, extra, string(pc.Mode), pc.ConfirmRequired)
	if cmd != nil {
		if err := commandDispatcher.Dispatch(cmd); err != nil {
			log.Printf("enqueueParsedCommand: dispatch error for %s: %v", cmd.ID, err)
		}
	}
	return cmd
}

// runConfirmIMCommand confirms or cancels a pending sensitive command by its
// command ID. When confirmed, the command is released for agent processing;
// when cancelled, it is marked done with a "cancelled" result.
func runConfirmIMCommand(commandID string, confirmed bool) (any, error) {
	if commandID == "" {
		return nil, fmt.Errorf("command_id is required")
	}
	// Lazily sweep expired confirmations before resolving.
	confirmDispatcher.sweepTimeouts()
	resolvedID, ok := commandDispatcher.ResolveByID(commandID, confirmed)
	if !ok {
		return nil, fmt.Errorf("command %q is not awaiting confirmation (it may have been confirmed, cancelled, timed out, or never required confirmation)", commandID)
	}
	action := "confirmed"
	if !confirmed {
		action = "cancelled"
	}
	return map[string]any{
		"command_id": resolvedID,
		"action":     action,
	}, nil
}

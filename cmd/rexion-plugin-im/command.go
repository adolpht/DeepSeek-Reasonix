package main

import (
	"fmt"
	"strings"
	"sync"
)

// WorkMode determines how an IM command should be executed by the agent.
type WorkMode string

const (
	// ModeAsk: question-only mode — answer without performing write operations.
	ModeAsk WorkMode = "ask"
	// ModePlan: plan-then-execute mode (default). The agent outlines a plan
	// first, then executes it.
	ModePlan WorkMode = "plan"
	// ModeCraft: direct execution mode — skip planning and execute immediately.
	ModeCraft WorkMode = "craft"
)

// ParsedCommand holds the result of parsing a raw IM message into a
// structured command with execution mode and confirmation requirements.
type ParsedCommand struct {
	Mode            WorkMode `json:"mode"`
	Task            string   `json:"task"`
	ConfirmRequired bool     `json:"confirm_required"`
	RawContent      string   `json:"raw_content"`
}

// sensitiveKeywords lists operation keywords that require secondary
// confirmation before execution. Matching is case-insensitive and
// substring-based so "删除文件" / "git push" / "deploy prod" all trigger.
var sensitiveKeywords = []string{
	"删除", "移除", "清空", "卸载",
	"推送", "push",
	"部署", "发布", "deploy",
	"drop", "truncate", "rm -rf",
	"重启", "关机", "shutdown", "reboot",
	"覆盖", "overwrite",
	"强制", "force",
}

// IsSensitiveCommand returns true when task text contains a sensitive
// operation keyword that should require secondary confirmation.
func IsSensitiveCommand(task string) bool {
	if task == "" {
		return false
	}
	lower := strings.ToLower(task)
	for _, kw := range sensitiveKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// ParseCommand parses a raw IM message into a ParsedCommand.
//
// Recognized prefixes:
//   - /ask <task>   → ModeAsk
//   - /plan <task>  → ModePlan
//   - /craft <task> → ModeCraft
//   - /status       → ModeAsk (status query, no write)
//   - /cancel       → ModeAsk (cancellation request)
//
// When no prefix is recognized, the whole content is treated as the task
// and ModePlan is used (the default). Sensitive operation keywords in the
// task set ConfirmRequired=true regardless of mode.
func ParseCommand(content string) ParsedCommand {
	raw := content
	trimmed := strings.TrimSpace(content)
	pc := ParsedCommand{
		RawContent: raw,
		Task:       trimmed,
		Mode:       ModePlan,
	}

	if trimmed == "" {
		return pc
	}

	// /status and /cancel are exact-match control commands (no write ops).
	switch trimmed {
	case "/status":
		pc.Mode = ModeAsk
		pc.Task = "status"
		return pc
	case "/cancel":
		pc.Mode = ModeAsk
		pc.Task = "cancel"
		return pc
	}

	// Prefix-based mode selection.
	switch {
	case strings.HasPrefix(trimmed, "/ask "):
		pc.Mode = ModeAsk
		pc.Task = strings.TrimSpace(strings.TrimPrefix(trimmed, "/ask "))
	case strings.HasPrefix(trimmed, "/plan "):
		pc.Mode = ModePlan
		pc.Task = strings.TrimSpace(strings.TrimPrefix(trimmed, "/plan "))
	case strings.HasPrefix(trimmed, "/craft "):
		pc.Mode = ModeCraft
		pc.Task = strings.TrimSpace(strings.TrimPrefix(trimmed, "/craft "))
	default:
		// No prefix — keep default ModePlan, task is the whole content.
	}

	if pc.Task == "" {
		pc.Task = trimmed
	}

	if IsSensitiveCommand(pc.Task) {
		pc.ConfirmRequired = true
	}

	return pc
}

// --- session-level work mode state ---

// workModeStore remembers the last WorkMode used per IM session, so a user
// who issued "/craft ..." stays in craft mode for subsequent prefix-less
// messages until they switch with another /ask, /plan, or /craft command.
type workModeStore struct {
	mu    sync.Mutex
	modes map[string]WorkMode
}

var workModes = &workModeStore{modes: make(map[string]WorkMode)}

// sessionModeKey returns the storage key for a session's work mode. It
// prefers conversation_id, then sender_id, then platform as a fallback.
func sessionModeKey(platform, conversationID, senderID string) string {
	switch {
	case conversationID != "":
		return platform + ":conv:" + conversationID
	case senderID != "":
		return platform + ":user:" + senderID
	default:
		return platform + ":default"
	}
}

// extraConvSender extracts the conversation ID and sender ID from a
// pending command's Extra map, used for session-key derivation.
func extraConvSender(extra map[string]string) (conversationID, senderID string) {
	if extra == nil {
		return
	}
	conversationID = extra["conversation_id"]
	if conversationID == "" {
		conversationID = extra["chat_id"]
	}
	senderID = extra["sender_staff_id"]
	if senderID == "" {
		senderID = extra["sender_id"]
	}
	return
}

// get returns the stored WorkMode for the session, or ModePlan (default).
func (s *workModeStore) get(key string) WorkMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.modes[key]; ok {
		return m
	}
	return ModePlan
}

// set stores the WorkMode for a session.
func (s *workModeStore) set(key string, mode WorkMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modes[key] = mode
}

// ResolveMode applies session-level mode persistence to a parsed command.
// When the user issues an explicit mode prefix (/ask, /plan, /craft), the
// session mode is updated. When the message has no prefix, the previously
// stored mode is used (defaulting to ModePlan).
func (pc *ParsedCommand) ResolveMode(sessionKey string) {
	trimmed := strings.TrimSpace(pc.RawContent)
	explicit := strings.HasPrefix(trimmed, "/ask ") ||
		strings.HasPrefix(trimmed, "/plan ") ||
		strings.HasPrefix(trimmed, "/craft ") ||
		trimmed == "/status" || trimmed == "/cancel"

	if explicit {
		workModes.set(sessionKey, pc.Mode)
		return
	}
	// No explicit prefix — inherit the session's last mode.
	if m := workModes.get(sessionKey); m != "" {
		pc.Mode = m
	}
}

// runSetWorkMode sets the work mode for an IM session. The session is
// identified by platform + conversation_id (preferred) or sender_id.
func runSetWorkMode(platform, conversationID, senderID, mode string) (any, error) {
	if platform == "" {
		return nil, fmt.Errorf("platform is required")
	}
	var m WorkMode
	switch WorkMode(mode) {
	case ModeAsk, ModePlan, ModeCraft:
		m = WorkMode(mode)
	default:
		return nil, fmt.Errorf("invalid mode %q (valid: ask, plan, craft)", mode)
	}
	key := sessionModeKey(platform, conversationID, senderID)
	workModes.set(key, m)
	return map[string]any{
		"platform":        platform,
		"conversation_id": conversationID,
		"sender_id":       senderID,
		"mode":            string(m),
	}, nil
}

// runGetWorkMode returns the current work mode for an IM session.
func runGetWorkMode(platform, conversationID, senderID string) (any, error) {
	if platform == "" {
		return nil, fmt.Errorf("platform is required")
	}
	key := sessionModeKey(platform, conversationID, senderID)
	m := workModes.get(key)
	return map[string]any{
		"platform":        platform,
		"conversation_id": conversationID,
		"sender_id":       senderID,
		"mode":            string(m),
	}, nil
}

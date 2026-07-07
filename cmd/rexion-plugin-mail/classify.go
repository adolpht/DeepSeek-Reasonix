package main

import (
	"strings"
)

// category constants for email classification.
const (
	CategoryUrgent     = "urgent"
	CategoryAction     = "action"
	CategoryNewsletter = "newsletter"
	CategoryPersonal   = "personal"
	CategoryOther      = "other"
)

// classifiedMail is an email with its assigned category.
type classifiedMail struct {
	From     string `json:"from"`
	Subject  string `json:"subject"`
	Date     string `json:"date"`
	Category string `json:"category"`
}

// classifyEmails classifies a slice of mailMessage using rule-based heuristics.
func classifyEmails(msgs []mailMessage) []classifiedMail {
	results := make([]classifiedMail, 0, len(msgs))
	for _, m := range msgs {
		cat := classifySingle(m)
		results = append(results, classifiedMail{
			From:     m.From,
			Subject:  m.Subject,
			Date:     m.Date,
			Category: cat,
		})
	}
	return results
}

// classifySingle classifies a single email message based on heuristic rules.
//
// Priority order (first match wins):
//  1. urgent  — subject or snippet contains urgency keywords
//  2. action  — subject or snippet contains action-requesting keywords
//  3. newsletter — sender address contains automated-sender patterns
//  4. personal — sender is a personal contact (no automated patterns)
//  5. other   — default fallback
func classifySingle(m mailMessage) string {
	text := strings.ToLower(m.Subject + " " + m.Snippet + " " + m.From)

	if isUrgent(text) {
		return CategoryUrgent
	}
	if isAction(text) {
		return CategoryAction
	}
	if isNewsletter(m.From) {
		return CategoryNewsletter
	}
	if isPersonal(m.From) {
		return CategoryPersonal
	}
	return CategoryOther
}

// isUrgent checks for urgency-indicating keywords.
func isUrgent(text string) bool {
	urgentKeywords := []string{
		"urgent", "紧急", "asap", "critical", "重要",
		"immediately", "立即", "紧急处理", "emergency",
		"high priority", "time-sensitive", "限时",
		"deadline", "截止", "过期", "overdue",
	}
	lower := strings.ToLower(text)
	for _, kw := range urgentKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isAction checks for action-requesting keywords.
func isAction(text string) bool {
	actionKeywords := []string{
		"please", "请", "需要", "need to", "must", "必须",
		"required", "action required", "需要您", "请确认",
		"请回复", "请审批", "approval needed", "review needed",
		"sign", "签署", "complete", "完成", "submit",
		"提交", "confirm", "确认", "respond", "回复",
		"remind", "提醒", "todo", "待办",
	}
	lower := strings.ToLower(text)
	for _, kw := range actionKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// isNewsletter checks if the sender address suggests an automated/bulk sender.
func isNewsletter(from string) bool {
	newsletterPatterns := []string{
		"noreply", "no-reply", "newsletter", "digest",
		"notification", "notify", "mailer-daemon", "mailer",
		"postmaster", "automated", "auto-reply", "autorespond",
		"donotreply", "do-not-reply", "blast", "campaign",
		"marketing", "updates@", "news@", "announce",
		"mailing", "listserv", "listserver", "bounce",
	}
	lower := strings.ToLower(from)
	for _, pat := range newsletterPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

// isPersonal checks if the sender looks like a real person (not automated).
// A sender is considered personal if it doesn't match newsletter patterns
// and contains a typical personal email structure.
func isPersonal(from string) bool {
	// If it's already identified as a newsletter, it's not personal.
	if isNewsletter(from) {
		return false
	}

	// A personal email typically has a real name or a simple email address
	// without automated-sender keywords. Check for common personal patterns.
	lower := strings.ToLower(from)

	// If the from contains a real name (e.g. "John Doe <john@gmail.com>"),
	// it's likely personal.
	if containsPersonalName(from) {
		return true
	}

	// Simple heuristic: if the local part of the email looks like a name
	// (contains dots like "first.last" or is a simple word without
	// automated patterns), consider it personal.
	atIdx := strings.Index(lower, "@")
	if atIdx > 0 {
		localPart := lower[:atIdx]
		// Names with dots (first.last pattern) are typically personal.
		if strings.Contains(localPart, ".") && !strings.Contains(localPart, "no") {
			return true
		}
		// Simple single-word local parts that aren't too short are likely personal.
		if len(localPart) >= 3 && !strings.Contains(localPart, "-") {
			return true
		}
	}

	return false
}

// containsPersonalName checks if the From field contains a display name
// (text before the <email> part).
func containsPersonalName(from string) bool {
	ltIdx := strings.Index(from, "<")
	if ltIdx > 0 {
		name := strings.TrimSpace(from[:ltIdx])
		// A real name typically has at least 2 characters and isn't just quotes.
		name = strings.Trim(name, `"'`)
		if len(name) >= 2 {
			return true
		}
	}
	return false
}

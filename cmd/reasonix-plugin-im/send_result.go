package main

import (
	"fmt"
	"strings"
)

// SendResult is the structured outcome of an outbound IM message delivery.
// ErrorKind drives the retry / dead-target policy in pushResult and the
// callback retry queue (see callback_retry.go).
//
// ErrorKind values:
//   - ""                 : success, no error
//   - "expired_webhook"  : SessionWebhook expired; refresh from cache and retry
//   - "dead_target"       : bot kicked / chat deleted — do not retry
//   - "auth_failed"       : 401/403, bad token — do not retry, alert operator
//   - "rate_limited"      : 429 — retry with backoff
//   - "timeout"           : network timeout — retry with backoff
//   - "unknown"           : catch-all
type SendResult struct {
	Success   bool
	MessageID string
	Error     string
	ErrorKind string
}

func newSendSuccess(messageID string) SendResult {
	return SendResult{Success: true, MessageID: messageID}
}

func newSendFailure(err error, kind string) SendResult {
	if err == nil {
		return SendResult{Success: false, Error: "unknown error", ErrorKind: kind}
	}
	return SendResult{Success: false, Error: err.Error(), ErrorKind: kind}
}

// isRetryable returns true for transient errors that warrant a backoff retry.
func (r SendResult) isRetryable() bool {
	switch r.ErrorKind {
	case "timeout", "rate_limited", "unknown":
		return true
	case "expired_webhook":
		return true // retry after refreshing the webhook URL from cache
	default:
		return false
	}
}

// isDead returns true for permanent errors that should mark the target dead.
func (r SendResult) isDead() bool {
	return r.ErrorKind == "dead_target"
}

// classifySendError inspects an error / status code / response body and
// returns the most specific ErrorKind. Used by pushResult to decide the
// retry strategy without each platform send function needing to know about
// the classification scheme.
func classifySendError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "expired"), strings.Contains(msg, "session webhook"),
		strings.Contains(msg, "sessionwebhook"):
		return "expired_webhook"
	case strings.Contains(msg, "not found") || strings.Contains(msg, "kicked"),
		strings.Contains(msg, "not in group"), strings.Contains(msg, "chat not exist"):
		return "dead_target"
	case strings.Contains(msg, "401") || strings.Contains(msg, "403"),
		strings.Contains(msg, "unauthorized"), strings.Contains(msg, "forbidden"),
		strings.Contains(msg, "invalid token"), strings.Contains(msg, "auth"):
		return "auth_failed"
	case strings.Contains(msg, "429") || strings.Contains(msg, "rate limit"):
		return "rate_limited"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out"):
		return "timeout"
	default:
		return "unknown"
	}
}

// classifyDingTalkErrorCode maps DingTalk errcode integers to ErrorKind.
// Reference: https://open.dingtalk.com/document/orgapp-server/custom-robots
func classifyDingTalkErrorCode(errcode int) string {
	switch errcode {
	case 0:
		return ""
	case 310000, 400001, 400002: // chat not exist / not in group / bot kicked
		return "dead_target"
	case 300001, 400013, 88: // token / sign errors
		return "auth_failed"
	case 130101, 4000101: // frequency limit
		return "rate_limited"
	default:
		return "unknown"
	}
}

// wrapSendErr is a small helper to format a SendResult from a plain error
// when no platform-specific errcode is available.
func wrapSendErr(prefix string, err error) SendResult {
	return newSendFailure(fmt.Errorf("%s: %w", prefix, err), classifySendError(err))
}

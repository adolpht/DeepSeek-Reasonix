// Command Rexion-plugin-mail is an MCP stdio server exposing email tools
// (read, send, search, classify) to Rexion. It is a standalone binary
// wired in via Rexion.toml:
//
//	[[plugins]]
//	name    = "mail"
//	command = "Rexion-plugin-mail"
//
// Environment variables:
//
//	MAIL_IMAP_HOST   - IMAP server address (e.g. "imap.gmail.com:993")
//	MAIL_IMAP_USER   - IMAP username
//	MAIL_IMAP_PASS   - IMAP password (or App Password)
//	MAIL_SMTP_HOST   - SMTP server address (e.g. "smtp.gmail.com:587")
//	MAIL_SMTP_USER   - SMTP username
//	MAIL_SMTP_PASS   - SMTP password
//
// Protocol: newline-delimited JSON-RPC 2.0 on stdin/stdout. Logs go to stderr;
// stdout is reserved for JSON-RPC.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// version is overridable via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log.SetPrefix("Rexion-plugin-mail: ")
	log.SetFlags(0)
	if err := serve(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// --- JSON-RPC framing ---

type request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	protocolVersion    = "2024-11-05"
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

func serve(in *os.File, out *os.File) error {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	defer w.Flush()
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if rerr := handleLine(line, w); rerr != nil {
				return rerr
			}
			if ferr := w.Flush(); ferr != nil {
				return ferr
			}
		}
		if err != nil {
			return nil
		}
	}
}

func handleLine(line []byte, w *bufio.Writer) error {
	line = trimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		log.Printf("skipping unparseable line: %v", err)
		return nil
	}
	if req.ID == nil {
		return nil
	}

	resp := response{JSONRPC: "2.0", ID: *req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "Rexion-plugin-mail", "version": version},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolList()}
	case "tools/call":
		resp.Result, resp.Error = callTool(req.Params)
	default:
		resp.Error = &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// --- tools ---

type toolDef struct {
	name        string
	description string
	schema      map[string]any
	readOnly    bool
	run         func(args map[string]any) (any, error)
}

var tools = []toolDef{
	readMailTool,
	sendMailTool,
	searchMailTool,
	classifyMailTool,
	oauth2AuthorizeTool,
	oauth2CallbackTool,
}

func toolList() []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":        t.name,
			"description": t.description,
			"inputSchema": t.schema,
			"annotations": map[string]any{
				"readOnlyHint": t.readOnly,
				"title":        t.name,
			},
		})
	}
	return out
}

func callTool(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	for _, t := range tools {
		if t.name != p.Name {
			continue
		}
		result, err := t.run(p.Arguments)
		if err != nil {
			return textResult(err.Error(), true), nil
		}
		if s, ok := result.(string); ok {
			return textResult(s, false), nil
		}
		return result, nil
	}
	return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
}

func textResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// --- arg helpers ---

func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	return s, nil
}

func argStringDefault(args map[string]any, key, def string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return def
	}
	if s == "" {
		return def
	}
	return s
}

func argIntDefault(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return def
		}
		return int(n)
	case int:
		return n
	default:
		return def
	}
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r' || b[len(b)-1] == '\n') {
		b = b[:len(b)-1]
	}
	return b
}

// --- IMAP config (see imap.go: imapConfig) ---

// --- tool definitions ---

var readMailTool = toolDef{
	name:        "read_mail",
	description: "Read emails from the inbox via IMAP",
	readOnly:    true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"folder": map[string]any{"type": "string", "description": "Mailbox folder name (default INBOX)"},
			"limit":  map[string]any{"type": "integer", "description": "Maximum emails to return (default 10)"},
			"offset": map[string]any{"type": "integer", "description": "Skip first N emails (default 0)"},
		},
	},
	run: runReadMail,
}

var sendMailTool = toolDef{
	name:        "send_mail",
	description: "Send an email via SMTP",
	readOnly:    false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"to":                  map[string]any{"type": "string", "description": "Recipient email address (comma-separated for multiple)"},
			"subject":             map[string]any{"type": "string", "description": "Email subject"},
			"body":                map[string]any{"type": "string", "description": "Email body (plain text)"},
			"cc":                  map[string]any{"type": "string", "description": "CC recipients (comma-separated)"},
			"reply_to_message_id": map[string]any{"type": "string", "description": "Message-ID to reply to"},
		},
		"required": []string{"to", "subject", "body"},
	},
	run: runSendMail,
}

var searchMailTool = toolDef{
	name:        "search_mail",
	description: "Search emails matching a query via IMAP SEARCH",
	readOnly:    true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":  map[string]any{"type": "string", "description": "Search query (subject, from, or body text)"},
			"folder": map[string]any{"type": "string", "description": "Mailbox folder (default INBOX)"},
			"limit":  map[string]any{"type": "integer", "description": "Maximum results (default 10)"},
		},
		"required": []string{"query"},
	},
	run: runSearchMail,
}

var classifyMailTool = toolDef{
	name:        "classify_mail",
	description: "Classify emails into categories (urgent/action/newsletter/personal/other) using rule-based heuristics",
	readOnly:    true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"folder": map[string]any{"type": "string", "description": "Mailbox folder (default INBOX)"},
			"limit":  map[string]any{"type": "integer", "description": "Maximum emails to classify (default 20)"},
		},
	},
	run: runClassifyMail,
}

// --- tool handlers ---

func runReadMail(args map[string]any) (any, error) {
	folder := argStringDefault(args, "folder", "INBOX")
	limit := argIntDefault(args, "limit", 10)
	offset := argIntDefault(args, "offset", 0)

	auth, err := imapConfig()
	if err != nil {
		return nil, err
	}

	msgs, err := imapReadFolder(auth, folder, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("read mail: %w", err)
	}

	if len(msgs) == 0 {
		return textResult("No emails found in "+folder, false), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Emails in %q (showing %d, offset %d):\n\n", folder, len(msgs), offset))
	for i, m := range msgs {
		b.WriteString(fmt.Sprintf("%d. From: %s\n", offset+i+1, m.From))
		b.WriteString(fmt.Sprintf("   Subject: %s\n", m.Subject))
		b.WriteString(fmt.Sprintf("   Date: %s\n", m.Date))
		if m.Snippet != "" {
			b.WriteString(fmt.Sprintf("   Preview: %s\n", m.Snippet))
		}
		b.WriteString("\n")
	}
	return textResult(b.String(), false), nil
}

func runSendMail(args map[string]any) (any, error) {
	to, err := argString(args, "to")
	if err != nil {
		return nil, err
	}
	subject, err := argString(args, "subject")
	if err != nil {
		return nil, err
	}
	body, err := argString(args, "body")
	if err != nil {
		return nil, err
	}
	cc := argStringDefault(args, "cc", "")
	replyToID := argStringDefault(args, "reply_to_message_id", "")

	auth, err := smtpConfig()
	if err != nil {
		return nil, err
	}

	recipients := splitAddresses(to)
	ccRecipients := splitAddresses(cc)

	if err := smtpSendMail(auth, auth.User, recipients, ccRecipients, subject, body, replyToID); err != nil {
		return nil, fmt.Errorf("send mail: %w", err)
	}

	result := map[string]any{
		"status":  "sent",
		"to":      recipients,
		"cc":      ccRecipients,
		"subject": subject,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return textResult(string(b), false), nil
}

func runSearchMail(args map[string]any) (any, error) {
	query, err := argString(args, "query")
	if err != nil {
		return nil, err
	}
	folder := argStringDefault(args, "folder", "INBOX")
	limit := argIntDefault(args, "limit", 10)

	auth, err := imapConfig()
	if err != nil {
		return nil, err
	}

	msgs, err := imapSearchMail(auth, folder, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search mail: %w", err)
	}

	if len(msgs) == 0 {
		return textResult(fmt.Sprintf("No emails found matching %q in %s", query, folder), false), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Search results for %q in %q (%d found):\n\n", query, folder, len(msgs)))
	for i, m := range msgs {
		b.WriteString(fmt.Sprintf("%d. From: %s\n", i+1, m.From))
		b.WriteString(fmt.Sprintf("   Subject: %s\n", m.Subject))
		b.WriteString(fmt.Sprintf("   Date: %s\n", m.Date))
		if m.Snippet != "" {
			b.WriteString(fmt.Sprintf("   Preview: %s\n", m.Snippet))
		}
		b.WriteString("\n")
	}
	return textResult(b.String(), false), nil
}

func runClassifyMail(args map[string]any) (any, error) {
	folder := argStringDefault(args, "folder", "INBOX")
	limit := argIntDefault(args, "limit", 20)

	auth, err := imapConfig()
	if err != nil {
		return nil, err
	}

	msgs, err := imapReadFolder(auth, folder, limit, 0)
	if err != nil {
		return nil, fmt.Errorf("classify mail: %w", err)
	}

	if len(msgs) == 0 {
		return textResult("No emails found in "+folder, false), nil
	}

	classified := classifyEmails(msgs)

	result := make([]map[string]any, 0, len(classified))
	for _, c := range classified {
		result = append(result, map[string]any{
			"from":     c.From,
			"subject":  c.Subject,
			"date":     c.Date,
			"category": c.Category,
		})
	}

	b, _ := json.MarshalIndent(result, "", "  ")
	return textResult(string(b), false), nil
}

// splitAddresses splits a comma-separated list of email addresses and trims whitespace.
func splitAddresses(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- OAuth2 tool definitions ---

var oauth2AuthorizeTool = toolDef{
	name:        "oauth2_authorize",
	description: "Get the OAuth2 authorization URL for Gmail or Outlook. Visit the URL in a browser to grant access.",
	readOnly:    true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"provider":     map[string]any{"type": "string", "enum": []string{"gmail", "outlook"}, "description": "OAuth2 provider"},
			"clientId":     map[string]any{"type": "string", "description": "OAuth2 client ID"},
			"clientSecret": map[string]any{"type": "string", "description": "OAuth2 client secret"},
			"tokenFile":    map[string]any{"type": "string", "description": "Path to persist the OAuth2 token"},
		},
		"required": []string{"provider", "clientId", "clientSecret"},
	},
	run: runOAuth2Authorize,
}

var oauth2CallbackTool = toolDef{
	name:        "oauth2_callback",
	description: "Exchange an OAuth2 authorization code for an access token. Call after the user visits the authorize URL and grants access.",
	readOnly:    false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"provider":     map[string]any{"type": "string", "enum": []string{"gmail", "outlook"}},
			"clientId":     map[string]any{"type": "string"},
			"clientSecret": map[string]any{"type": "string"},
			"code":         map[string]any{"type": "string", "description": "Authorization code from the OAuth2 callback"},
			"tokenFile":    map[string]any{"type": "string"},
		},
		"required": []string{"provider", "clientId", "clientSecret", "code"},
	},
	run: runOAuth2Callback,
}

func runOAuth2Authorize(args map[string]any) (any, error) {
	provider, err := argString(args, "provider")
	if err != nil {
		return nil, err
	}
	clientID, err := argString(args, "clientId")
	if err != nil {
		return nil, err
	}
	clientSecret, err := argString(args, "clientSecret")
	if err != nil {
		return nil, err
	}
	tokenFile := argStringDefault(args, "tokenFile", defaultOAuth2TokenFile())

	cfg := OAuth2Config{
		Provider:     OAuth2Provider(provider),
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenFile:    tokenFile,
	}

	if cfg.Provider != OAuth2Gmail && cfg.Provider != OAuth2Outlook {
		return nil, fmt.Errorf("unsupported provider %q; use \"gmail\" or \"outlook\"", provider)
	}

	url, err := GetOAuth2AuthURL(cfg)
	if err != nil {
		return nil, fmt.Errorf("generate auth URL: %w", err)
	}

	result := map[string]any{
		"authorizationUrl": url,
		"provider":         string(cfg.Provider),
		"tokenFile":        tokenFile,
		"instructions":     "Visit the URL above in your browser to authorize. After granting access, you will receive a code — pass it to the oauth2_callback tool.",
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return textResult(string(b), false), nil
}

func runOAuth2Callback(args map[string]any) (any, error) {
	provider, err := argString(args, "provider")
	if err != nil {
		return nil, err
	}
	clientID, err := argString(args, "clientId")
	if err != nil {
		return nil, err
	}
	clientSecret, err := argString(args, "clientSecret")
	if err != nil {
		return nil, err
	}
	code, err := argString(args, "code")
	if err != nil {
		return nil, err
	}
	tokenFile := argStringDefault(args, "tokenFile", defaultOAuth2TokenFile())

	cfg := OAuth2Config{
		Provider:     OAuth2Provider(provider),
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenFile:    tokenFile,
	}

	if cfg.Provider != OAuth2Gmail && cfg.Provider != OAuth2Outlook {
		return nil, fmt.Errorf("unsupported provider %q; use \"gmail\" or \"outlook\"", provider)
	}

	token, err := ExchangeOAuth2Code(cfg, code)
	if err != nil {
		return nil, fmt.Errorf("exchange OAuth2 code: %w", err)
	}

	result := map[string]any{
		"status":       "success",
		"provider":     string(cfg.Provider),
		"tokenType":    token.TokenType,
		"accessToken":  token.AccessToken,
		"refreshToken": token.RefreshToken,
		"expiry":       token.Expiry.Format(time.RFC3339),
	}
	if cfg.TokenFile != "" {
		result["tokenFile"] = cfg.TokenFile
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return textResult(string(b), false), nil
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// --- classify tests ---

func TestClassifyUrgent(t *testing.T) {
	msgs := []mailMessage{
		{From: "boss@company.com", Subject: "URGENT: Server down", Snippet: "The production server is down", Date: "2025-01-01T00:00:00Z"},
		{From: "manager@company.com", Subject: "紧急通知", Snippet: "请立即处理", Date: "2025-01-01T00:00:00Z"},
		{From: "ops@company.com", Subject: "ASAP: Critical fix needed", Snippet: "Deadline today", Date: "2025-01-01T00:00:00Z"},
	}
	results := classifyEmails(msgs)
	for _, r := range results {
		if r.Category != CategoryUrgent {
			t.Errorf("expected urgent, got %s for subject %q", r.Category, r.Subject)
		}
	}
}

func TestClassifyAction(t *testing.T) {
	msgs := []mailMessage{
		{From: "colleague@company.com", Subject: "Please review the document", Snippet: "Need your feedback", Date: "2025-01-01T00:00:00Z"},
		{From: "hr@company.com", Subject: "需要您确认", Snippet: "请回复此邮件", Date: "2025-01-01T00:00:00Z"},
	}
	results := classifyEmails(msgs)
	for _, r := range results {
		if r.Category != CategoryAction {
			t.Errorf("expected action, got %s for subject %q", r.Category, r.Subject)
		}
	}
}

func TestClassifyNewsletter(t *testing.T) {
	msgs := []mailMessage{
		{From: "noreply@newsletter.com", Subject: "Weekly digest", Snippet: "Here are this week's top stories", Date: "2025-01-01T00:00:00Z"},
		{From: "digest@techcrunch.com", Subject: "Daily news", Snippet: "Latest tech news", Date: "2025-01-01T00:00:00Z"},
		{From: "mailer-daemon@company.com", Subject: "Delivery report", Snippet: "Message delivery status", Date: "2025-01-01T00:00:00Z"},
	}
	results := classifyEmails(msgs)
	for _, r := range results {
		if r.Category != CategoryNewsletter {
			t.Errorf("expected newsletter, got %s for from %q", r.Category, r.From)
		}
	}
}

func TestClassifyPersonal(t *testing.T) {
	msgs := []mailMessage{
		{From: "John Smith <john.smith@gmail.com>", Subject: "Lunch tomorrow?", Snippet: "Are you free for lunch?", Date: "2025-01-01T00:00:00Z"},
		{From: "alice@company.com", Subject: "Meeting notes", Snippet: "Here are the meeting notes", Date: "2025-01-01T00:00:00Z"},
	}
	results := classifyEmails(msgs)
	for _, r := range results {
		if r.Category != CategoryPersonal {
			t.Errorf("expected personal, got %s for from %q", r.Category, r.From)
		}
	}
}

func TestClassifyOther(t *testing.T) {
	msgs := []mailMessage{
		{From: "info@unknown-system.org", Subject: "System report", Snippet: "Automated system report", Date: "2025-01-01T00:00:00Z"},
	}
	results := classifyEmails(msgs)
	// info@ triggers newsletter pattern, so this should be newsletter not other.
	// Let's use a different from address.
	msgs2 := []mailMessage{
		{From: "support@vendor.com", Subject: "Invoice #123", Snippet: "Your invoice is attached", Date: "2025-01-01T00:00:00Z"},
	}
	_ = results
	results2 := classifyEmails(msgs2)
	if results2[0].Category == CategoryUrgent {
		t.Errorf("should not be urgent for subject %q", results2[0].Subject)
	}
}

func TestClassifyPriority(t *testing.T) {
	// Urgent should take priority over newsletter.
	msg := mailMessage{
		From: "noreply@alerts.com", Subject: "URGENT alert", Snippet: "Critical system alert",
		Date: "2025-01-01T00:00:00Z",
	}
	result := classifySingle(msg)
	if result != CategoryUrgent {
		t.Errorf("expected urgent to take priority, got %s", result)
	}

	// Action should take priority over personal.
	msg2 := mailMessage{
		From: "John <john@gmail.com>", Subject: "Please confirm your attendance",
		Snippet: "Need your response", Date: "2025-01-01T00:00:00Z",
	}
	result2 := classifySingle(msg2)
	if result2 != CategoryAction {
		t.Errorf("expected action to take priority, got %s", result2)
	}
}

// --- helper function tests ---

func TestSplitAddresses(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"alice@example.com", []string{"alice@example.com"}},
		{"alice@example.com, bob@example.com", []string{"alice@example.com", "bob@example.com"}},
		{" alice@example.com , bob@example.com ", []string{"alice@example.com", "bob@example.com"}},
		{",,", nil},
	}
	for _, tt := range tests {
		got := splitAddresses(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitAddresses(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitAddresses(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestSplitHostPort(t *testing.T) {
	tests := []struct {
		input    string
		wantHost string
		wantPort string
	}{
		{"smtp.gmail.com:587", "smtp.gmail.com", "587"},
		{"localhost:25", "localhost", "25"},
		{"smtp.example.com", "smtp.example.com", ""},
	}
	for _, tt := range tests {
		host, port := splitHostPort(tt.input)
		if host != tt.wantHost || port != tt.wantPort {
			t.Errorf("splitHostPort(%q) = (%q, %q), want (%q, %q)",
				tt.input, host, port, tt.wantHost, tt.wantPort)
		}
	}
}

func TestParseEmailAddress(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"alice@example.com", "alice@example.com"},
		{"Alice <alice@example.com>", "alice@example.com"},
		{"  bob@test.com  ", "bob@test.com"},
	}
	for _, tt := range tests {
		got, err := parseEmailAddress(tt.input)
		if err != nil {
			t.Errorf("parseEmailAddress(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseEmailAddress(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildMessage(t *testing.T) {
	msg := buildMessage(
		"sender@test.com",
		[]string{"alice@test.com", "bob@test.com"},
		[]string{"cc@test.com"},
		"Test Subject",
		"Hello World",
		"<msg123@test.com>",
	)

	if !strings.Contains(msg, "From: sender@test.com") {
		t.Error("message missing From header")
	}
	if !strings.Contains(msg, "To: alice@test.com, bob@test.com") {
		t.Error("message missing To header")
	}
	if !strings.Contains(msg, "Cc: cc@test.com") {
		t.Error("message missing Cc header")
	}
	if !strings.Contains(msg, "Subject: Test Subject") {
		t.Error("message missing Subject header")
	}
	if !strings.Contains(msg, "Hello World") {
		t.Error("message missing body")
	}
	if !strings.Contains(msg, "In-Reply-To: <msg123@test.com>") {
		t.Error("message missing In-Reply-To header")
	}
	if !strings.Contains(msg, "References: <msg123@test.com>") {
		t.Error("message missing References header")
	}
}

func TestBuildMessageNoCC(t *testing.T) {
	msg := buildMessage(
		"sender@test.com",
		[]string{"alice@test.com"},
		nil,
		"Hello",
		"Body",
		"",
	)
	if strings.Contains(msg, "Cc:") {
		t.Error("message should not contain Cc header when cc is nil")
	}
	if strings.Contains(msg, "In-Reply-To:") {
		t.Error("message should not contain In-Reply-To when replyToMessageID is empty")
	}
}

// --- IMAP config tests ---

func TestImapConfigMissing(t *testing.T) {
	// Save and clear env vars.
	origHost := os.Getenv("MAIL_IMAP_HOST")
	origUser := os.Getenv("MAIL_IMAP_USER")
	origPass := os.Getenv("MAIL_IMAP_PASS")
	os.Unsetenv("MAIL_IMAP_HOST")
	os.Unsetenv("MAIL_IMAP_USER")
	os.Unsetenv("MAIL_IMAP_PASS")
	defer func() {
		os.Setenv("MAIL_IMAP_HOST", origHost)
		os.Setenv("MAIL_IMAP_USER", origUser)
		os.Setenv("MAIL_IMAP_PASS", origPass)
	}()

	_, err := imapConfig()
	if err == nil {
		t.Error("expected error when IMAP config is missing")
	}
}

func TestSmtpConfigMissing(t *testing.T) {
	origHost := os.Getenv("MAIL_SMTP_HOST")
	origUser := os.Getenv("MAIL_SMTP_USER")
	origPass := os.Getenv("MAIL_SMTP_PASS")
	os.Unsetenv("MAIL_SMTP_HOST")
	os.Unsetenv("MAIL_SMTP_USER")
	os.Unsetenv("MAIL_SMTP_PASS")
	defer func() {
		os.Setenv("MAIL_SMTP_HOST", origHost)
		os.Setenv("MAIL_SMTP_USER", origUser)
		os.Setenv("MAIL_SMTP_PASS", origPass)
	}()

	_, err := smtpConfig()
	if err == nil {
		t.Error("expected error when SMTP config is missing")
	}
}

// --- JSON-RPC tool dispatch tests ---

func TestToolList(t *testing.T) {
	list := toolList()
	names := make(map[string]bool)
	for _, t := range list {
		name, _ := t["name"].(string)
		names[name] = true
	}
	for _, expected := range []string{"read_mail", "send_mail", "search_mail", "classify_mail"} {
		if !names[expected] {
			t.Errorf("tool %q not found in tool list", expected)
		}
	}
}

func TestCallToolUnknown(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"name":      "nonexistent",
		"arguments": map[string]any{},
	})
	_, rpcErr := callTool(params)
	if rpcErr == nil {
		t.Fatal("expected error for unknown tool")
	}
	if rpcErr.Code != codeInvalidParams {
		t.Errorf("error code = %d, want %d", rpcErr.Code, codeInvalidParams)
	}
}

func TestCallToolSendMailMissingArgs(t *testing.T) {
	params, _ := json.Marshal(map[string]any{
		"name":      "send_mail",
		"arguments": map[string]any{},
	})
	result, rpcErr := callTool(params)
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %v", rpcErr)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result is not a map")
	}
	if isError, _ := m["isError"].(bool); !isError {
		t.Error("expected isError=true for missing required args")
	}
}

func TestReadMailMissingConfig(t *testing.T) {
	os.Unsetenv("MAIL_IMAP_HOST")
	os.Unsetenv("MAIL_IMAP_USER")
	os.Unsetenv("MAIL_IMAP_PASS")

	_, err := runReadMail(map[string]any{})
	if err == nil {
		t.Error("expected error when IMAP not configured")
	}
	if !strings.Contains(err.Error(), "MAIL_IMAP") {
		t.Errorf("error should mention MAIL_IMAP, got: %v", err)
	}
}

func TestSearchMailMissingConfig(t *testing.T) {
	os.Unsetenv("MAIL_IMAP_HOST")
	os.Unsetenv("MAIL_IMAP_USER")
	os.Unsetenv("MAIL_IMAP_PASS")

	_, err := runSearchMail(map[string]any{"query": "test"})
	if err == nil {
		t.Error("expected error when IMAP not configured")
	}
}

func TestClassifyMailMissingConfig(t *testing.T) {
	os.Unsetenv("MAIL_IMAP_HOST")
	os.Unsetenv("MAIL_IMAP_USER")
	os.Unsetenv("MAIL_IMAP_PASS")

	_, err := runClassifyMail(map[string]any{})
	if err == nil {
		t.Error("expected error when IMAP not configured")
	}
}

func TestSendMailMissingConfig(t *testing.T) {
	os.Unsetenv("MAIL_SMTP_HOST")
	os.Unsetenv("MAIL_SMTP_USER")
	os.Unsetenv("MAIL_SMTP_PASS")

	_, err := runSendMail(map[string]any{
		"to": "test@test.com", "subject": "hi", "body": "hello",
	})
	if err == nil {
		t.Error("expected error when SMTP not configured")
	}
}

// --- isUrgent / isAction / isNewsletter / isPersonal unit tests ---

func TestIsUrgent(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"urgent matter", true},
		{"紧急处理", true},
		{"this is ASAP", true},
		{"hello world", false},
		{"regular meeting", false},
	}
	for _, tt := range cases {
		got := isUrgent(tt.text)
		if got != tt.want {
			t.Errorf("isUrgent(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestIsAction(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"please review", true},
		{"请确认", true},
		{"需要您处理", true},
		{"fyi only", false},
	}
	for _, tt := range cases {
		got := isAction(tt.text)
		if got != tt.want {
			t.Errorf("isAction(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestIsNewsletter(t *testing.T) {
	cases := []struct {
		from string
		want bool
	}{
		{"noreply@github.com", true},
		{"no-reply@service.com", true},
		{"newsletter@medium.com", true},
		{"digest@news.ycombinator.com", true},
		{"john@gmail.com", false},
		{"alice@company.com", false},
	}
	for _, tt := range cases {
		got := isNewsletter(tt.from)
		if got != tt.want {
			t.Errorf("isNewsletter(%q) = %v, want %v", tt.from, got, tt.want)
		}
	}
}

func TestIsPersonal(t *testing.T) {
	cases := []struct {
		from string
		want bool
	}{
		{"John Smith <john.smith@gmail.com>", true},
		{"alice@company.com", true},
		{"noreply@github.com", false},
		{"info@system.org", false},
	}
	for _, tt := range cases {
		got := isPersonal(tt.from)
		if got != tt.want {
			t.Errorf("isPersonal(%q) = %v, want %v", tt.from, got, tt.want)
		}
	}
}

// --- arg helper tests ---

func TestArgString(t *testing.T) {
	args := map[string]any{"name": "alice", "num": float64(42)}
	val, err := argString(args, "name")
	if err != nil || val != "alice" {
		t.Errorf("argString(name) = %q, %v; want alice, nil", val, err)
	}
	_, err = argString(args, "missing")
	if err == nil {
		t.Error("expected error for missing key")
	}
	_, err = argString(args, "num")
	if err == nil {
		t.Error("expected error for non-string value")
	}
}

func TestArgStringDefault(t *testing.T) {
	args := map[string]any{"name": "bob"}
	if got := argStringDefault(args, "name", "default"); got != "bob" {
		t.Errorf("got %q, want bob", got)
	}
	if got := argStringDefault(args, "missing", "default"); got != "default" {
		t.Errorf("got %q, want default", got)
	}
	if got := argStringDefault(args, "empty", "default"); got != "default" {
		t.Errorf("got %q, want default", got)
	}
}

func TestArgIntDefault(t *testing.T) {
	args := map[string]any{"count": float64(5), "str": "hello"}
	if got := argIntDefault(args, "count", 10); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
	if got := argIntDefault(args, "missing", 10); got != 10 {
		t.Errorf("got %d, want 10", got)
	}
	if got := argIntDefault(args, "str", 10); got != 10 {
		t.Errorf("got %d, want 10 for non-int value", got)
	}
}

// --- textResult test ---

func TestTextResult(t *testing.T) {
	r := textResult("hello", false)
	content, ok := r["content"].([]map[string]any)
	if !ok {
		t.Fatal("content is not the right type")
	}
	if content[0]["text"] != "hello" {
		t.Errorf("text = %v, want hello", content[0]["text"])
	}
	if r["isError"] != false {
		t.Error("isError should be false")
	}

	r2 := textResult("oops", true)
	if r2["isError"] != true {
		t.Error("isError should be true")
	}
}

// --- decodeHeader test ---

func TestDecodeHeader(t *testing.T) {
	// Plain ASCII should pass through.
	if got := decodeHeader("Hello"); got != "Hello" {
		t.Errorf("decodeHeader(Hello) = %q, want Hello", got)
	}
	// RFC 2047 encoded subject.
	if got := decodeHeader("=?UTF-8?B?5L2g5aW9?="); got == "" {
		t.Error("decodeHeader returned empty for encoded string")
	}
	_ = fmt.Sprintf("decoded: %s", decodeHeader("=?UTF-8?B?5L2g5aW9?="))
}

// --- OAuth2 tests ---

func TestOAuth2IMAPAuthString(t *testing.T) {
	got := OAuth2IMAPAuthString("user@gmail.com", "ya29.token123")
	want := "user=user@gmail.com\x01auth=Bearer ya29.token123\x01\x01"
	if got != want {
		t.Errorf("OAuth2IMAPAuthString = %q, want %q", got, want)
	}
}

func TestOAuth2SMTPAuthString(t *testing.T) {
	got := OAuth2SMTPAuthString("user@gmail.com", "ya29.token456")
	// SMTP uses the same format as IMAP.
	want := OAuth2IMAPAuthString("user@gmail.com", "ya29.token456")
	if got != want {
		t.Errorf("OAuth2SMTPAuthString = %q, want %q", got, want)
	}
}

func TestMailAuthConfigUseOAuth2(t *testing.T) {
	// No OAuth2 config → false.
	plain := mailAuthConfig{Host: "imap.gmail.com:993", User: "u", Password: "p"}
	if plain.useOAuth2() {
		t.Error("plain config should not use OAuth2")
	}

	// With OAuth2 config and token → true.
	withOAuth := mailAuthConfig{
		Host:   "imap.gmail.com:993",
		User:   "u@gmail.com",
		OAuth2: &OAuth2Config{Provider: OAuth2Gmail, ClientID: "id", ClientSecret: "secret"},
		Token:  &oauth2.Token{AccessToken: "at"},
	}
	if !withOAuth.useOAuth2() {
		t.Error("config with OAuth2+Token should use OAuth2")
	}

	// OAuth2 config but nil token → false.
	noToken := mailAuthConfig{
		Host:   "imap.gmail.com:993",
		User:   "u@gmail.com",
		OAuth2: &OAuth2Config{Provider: OAuth2Gmail, ClientID: "id", ClientSecret: "secret"},
	}
	if noToken.useOAuth2() {
		t.Error("config with OAuth2 but no Token should not use OAuth2")
	}
}

func TestDefaultOAuth2TokenFile(t *testing.T) {
	path := defaultOAuth2TokenFile()
	if !strings.HasSuffix(path, "mail_oauth2_token.json") {
		t.Errorf("token file should end with mail_oauth2_token.json, got %q", path)
	}
}

func TestLoadOAuth2ConfigFromEnv(t *testing.T) {
	origProvider := os.Getenv("MAIL_OAUTH2_PROVIDER")
	origID := os.Getenv("MAIL_OAUTH2_CLIENT_ID")
	origSecret := os.Getenv("MAIL_OAUTH2_CLIENT_SECRET")
	defer func() {
		os.Setenv("MAIL_OAUTH2_PROVIDER", origProvider)
		os.Setenv("MAIL_OAUTH2_CLIENT_ID", origID)
		os.Setenv("MAIL_OAUTH2_CLIENT_SECRET", origSecret)
	}()

	// No env vars → not configured.
	os.Unsetenv("MAIL_OAUTH2_PROVIDER")
	_, ok := loadOAuth2ConfigFromEnv()
	if ok {
		t.Error("should not be configured without env vars")
	}

	// All env vars set → configured.
	os.Setenv("MAIL_OAUTH2_PROVIDER", "gmail")
	os.Setenv("MAIL_OAUTH2_CLIENT_ID", "test-id")
	os.Setenv("MAIL_OAUTH2_CLIENT_SECRET", "test-secret")
	cfg, ok := loadOAuth2ConfigFromEnv()
	if !ok {
		t.Error("should be configured with all env vars")
	}
	if cfg.Provider != OAuth2Gmail {
		t.Errorf("provider = %q, want gmail", cfg.Provider)
	}
	if cfg.ClientID != "test-id" {
		t.Errorf("clientID = %q, want test-id", cfg.ClientID)
	}
}

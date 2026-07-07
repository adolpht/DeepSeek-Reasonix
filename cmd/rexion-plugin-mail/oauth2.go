package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// OAuth2Provider identifies which OAuth2 provider to use.
type OAuth2Provider string

const (
	OAuth2Gmail   OAuth2Provider = "gmail"
	OAuth2Outlook OAuth2Provider = "outlook"
)

// OAuth2Config holds the OAuth2 configuration for a mail provider.
type OAuth2Config struct {
	Provider     OAuth2Provider `json:"provider"`
	ClientID     string         `json:"clientId"`
	ClientSecret string         `json:"clientSecret"`
	TokenFile    string         `json:"tokenFile,omitempty"` // path to persist token
}

// Gmail OAuth2 scopes
var gmailScopes = []string{
	"https://mail.google.com/", // full Gmail access (IMAP + SMTP)
}

// Outlook OAuth2 scopes
var outlookScopes = []string{
	"https://outlook.office.com/IMAP.AccessAsUser.All",
	"https://outlook.office.com/SMTP.Send",
}

var (
	oauth2Mu    sync.Mutex
	oauth2Token *oauth2.Token
	oauth2Cfg   *oauth2.Config
)

// These function variables are indirection points used by the IMAP/SMTP
// auth paths so tests can override them without making real network calls.
var (
	getOAuth2AccessTokenFn = GetOAuth2AccessToken
	loadOAuth2TokenFn      = LoadOAuth2Token
)

// mailAuthConfig holds the resolved connection + authentication settings for
// either IMAP or SMTP. When OAuth2 fields are populated, XOAUTH2 is used;
// otherwise the PLAIN password path is used.
type mailAuthConfig struct {
	Host     string
	User     string
	Password string        // used when authenticating via PLAIN
	OAuth2   *OAuth2Config // non-nil when authenticating via XOAUTH2
	Token    *oauth2.Token // non-nil when authenticating via XOAUTH2
}

// useOAuth2 reports whether this connection should authenticate via XOAUTH2.
func (a mailAuthConfig) useOAuth2() bool {
	return a.OAuth2 != nil && a.Token != nil
}

// defaultOAuth2TokenFile returns the standard token persistence path:
// ~/.rexion/mail_oauth2_token.json
func defaultOAuth2TokenFile() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".rexion", "mail_oauth2_token.json")
	}
	return filepath.Join(home, ".rexion", "mail_oauth2_token.json")
}

// loadOAuth2ConfigFromEnv reads the OAuth2 provider configuration from
// environment variables. The bool result reports whether OAuth2 is configured.
func loadOAuth2ConfigFromEnv() (OAuth2Config, bool) {
	provider := os.Getenv("MAIL_OAUTH2_PROVIDER")
	if provider == "" {
		return OAuth2Config{}, false
	}
	clientID := os.Getenv("MAIL_OAUTH2_CLIENT_ID")
	clientSecret := os.Getenv("MAIL_OAUTH2_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		return OAuth2Config{}, false
	}
	tokenFile := os.Getenv("MAIL_OAUTH2_TOKEN_FILE")
	if tokenFile == "" {
		tokenFile = defaultOAuth2TokenFile()
	}
	return OAuth2Config{
		Provider:     OAuth2Provider(provider),
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenFile:    tokenFile,
	}, true
}

// persistOAuth2Token writes the token to the given file, creating parent
// directories as needed. Errors are intentionally non-fatal: token refresh
// is best-effort and a failed write should not abort an in-flight mail op.
func persistOAuth2Token(tokenFile string, token *oauth2.Token) {
	if tokenFile == "" {
		return
	}
	if dir := filepath.Dir(tokenFile); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o700)
	}
	if data, err := json.Marshal(token); err == nil {
		_ = os.WriteFile(tokenFile, data, 0o600)
	}
}

// oauth2Endpoint returns the OAuth2 endpoint for the given provider.
func oauth2Endpoint(provider OAuth2Provider) oauth2.Endpoint {
	switch provider {
	case OAuth2Gmail:
		return google.Endpoint
	case OAuth2Outlook:
		return oauth2.Endpoint{
			AuthURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		}
	default:
		return oauth2.Endpoint{}
	}
}

// setupOAuth2Config creates an oauth2.Config from the OAuth2Config.
func setupOAuth2Config(cfg OAuth2Config) *oauth2.Config {
	var scopes []string
	switch cfg.Provider {
	case OAuth2Gmail:
		scopes = gmailScopes
	case OAuth2Outlook:
		scopes = outlookScopes
	}
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Scopes:       scopes,
		Endpoint:     oauth2Endpoint(cfg.Provider),
	}
}

// GetOAuth2AuthURL returns the URL the user must visit to authorize the app.
// This is used by the `oauth2_authorize` MCP tool.
func GetOAuth2AuthURL(cfg OAuth2Config) (string, error) {
	config := setupOAuth2Config(cfg)
	// Use a random state for CSRF protection
	state := fmt.Sprintf("Rexion-%d", time.Now().UnixNano())
	return config.AuthCodeURL(state, oauth2.AccessTypeOffline), nil
}

// ExchangeOAuth2Code exchanges the authorization code for an access token.
// This is used by the `oauth2_callback` MCP tool.
func ExchangeOAuth2Code(cfg OAuth2Config, code string) (*oauth2.Token, error) {
	config := setupOAuth2Config(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("oauth2 exchange: %w", err)
	}
	// Persist token
	persistOAuth2Token(cfg.TokenFile, token)
	oauth2Mu.Lock()
	oauth2Token = token
	oauth2Cfg = config
	oauth2Mu.Unlock()
	return token, nil
}

// LoadOAuth2Token loads a previously saved token from file.
func LoadOAuth2Token(tokenFile string) (*oauth2.Token, error) {
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}
	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	return &token, nil
}

// OAuth2TokenSource returns an oauth2.TokenSource that automatically refreshes.
func OAuth2TokenSource(cfg OAuth2Config, token *oauth2.Token) oauth2.TokenSource {
	config := setupOAuth2Config(cfg)
	return config.TokenSource(context.Background(), token)
}

// OAuth2IMAPAuthString generates the OAuth2 SASL auth string for IMAP.
// Format: user=<user>\x01auth=Bearer <token>\x01\x01
func OAuth2IMAPAuthString(user, accessToken string) string {
	return fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", user, accessToken)
}

// OAuth2SMTPAuthString generates the OAuth2 SASL auth string for SMTP.
// Same format as IMAP.
func OAuth2SMTPAuthString(user, accessToken string) string {
	return OAuth2IMAPAuthString(user, accessToken)
}

// GetOAuth2AccessToken returns a valid access token, refreshing if needed.
func GetOAuth2AccessToken(cfg OAuth2Config, token *oauth2.Token) (string, error) {
	src := OAuth2TokenSource(cfg, token)
	t, err := src.Token()
	if err != nil {
		return "", fmt.Errorf("refresh token: %w", err)
	}
	// Persist refreshed token
	persistOAuth2Token(cfg.TokenFile, t)
	return t.AccessToken, nil
}

// oauth2HTTPClient returns an HTTP client with OAuth2 authentication.
// Useful for Outlook REST API calls.
func oauth2HTTPClient(cfg OAuth2Config, token *oauth2.Token) *http.Client {
	src := OAuth2TokenSource(cfg, token)
	return oauth2.NewClient(context.Background(), src)
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	state := fmt.Sprintf("reasonix-%d", time.Now().UnixNano())
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
	if cfg.TokenFile != "" {
		if data, err := json.Marshal(token); err == nil {
			os.WriteFile(cfg.TokenFile, data, 0600)
		}
	}
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
	if cfg.TokenFile != "" {
		if data, err := json.Marshal(t); err == nil {
			os.WriteFile(cfg.TokenFile, data, 0600)
		}
	}
	return t.AccessToken, nil
}

// oauth2HTTPClient returns an HTTP client with OAuth2 authentication.
// Useful for Outlook REST API calls.
func oauth2HTTPClient(cfg OAuth2Config, token *oauth2.Token) *http.Client {
	src := OAuth2TokenSource(cfg, token)
	return oauth2.NewClient(context.Background(), src)
}

package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// smtpConfig reads SMTP connection settings from environment variables and
// resolves the authentication method. OAuth2 is preferred when configured;
// otherwise it falls back to PLAIN password auth. Returns a clear error if
// neither is available.
func smtpConfig() (mailAuthConfig, error) {
	host := os.Getenv("MAIL_SMTP_HOST")
	user := os.Getenv("MAIL_SMTP_USER")
	if host == "" || user == "" {
		return mailAuthConfig{}, fmt.Errorf(
			"mail SMTP not configured. Set environment variables: MAIL_SMTP_HOST, MAIL_SMTP_USER (plus MAIL_SMTP_PASS or OAuth2 env vars)",
		)
	}

	if cfg, ok := loadOAuth2ConfigFromEnv(); ok {
		if cfg.Provider != OAuth2Gmail && cfg.Provider != OAuth2Outlook {
			return mailAuthConfig{}, fmt.Errorf("unsupported OAuth2 provider %q; use \"gmail\" or \"outlook\"", cfg.Provider)
		}
		tok, err := loadOAuth2TokenFn(cfg.TokenFile)
		if err != nil {
			return mailAuthConfig{}, fmt.Errorf(
				"OAuth2 configured but no token found at %q: %w. Please run oauth2_authorize/oauth2_callback first",
				cfg.TokenFile, err,
			)
		}
		return mailAuthConfig{Host: host, User: user, OAuth2: &cfg, Token: tok}, nil
	}

	pass := os.Getenv("MAIL_SMTP_PASS")
	if pass == "" {
		return mailAuthConfig{}, fmt.Errorf(
			"请先配置 OAuth2（调用 oauth2_authorize）或设置 MAIL_SMTP_PASS 环境变量",
		)
	}
	return mailAuthConfig{Host: host, User: user, Password: pass}, nil
}

// xoauth2SMTPAuth implements net/smtp.Auth for the XOAUTH2 mechanism.
type xoauth2SMTPAuth struct {
	user  string
	token string
}

func (a *xoauth2SMTPAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "XOAUTH2", []byte(OAuth2SMTPAuthString(a.user, a.token)), nil
}

func (a *xoauth2SMTPAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		return nil, fmt.Errorf("unexpected server challenge during XOAUTH2")
	}
	return nil, nil
}

// smtpDial connects to the SMTP server, sends EHLO, and upgrades to STARTTLS
// when the server advertises it.
func smtpDial(fullHost, hostPart string) (*smtp.Client, error) {
	c, err := smtp.Dial(fullHost)
	if err != nil {
		return nil, fmt.Errorf("connect to SMTP server %s: %w", fullHost, err)
	}
	if err := c.Hello("localhost"); err != nil {
		c.Close()
		return nil, fmt.Errorf("EHLO: %w", err)
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{
			ServerName:         hostPart,
			InsecureSkipVerify: true,
		}); err != nil {
			c.Close()
			return nil, fmt.Errorf("STARTTLS: %w", err)
		}
	}
	return c, nil
}

// smtpDeliver sends the message envelope and body over an authenticated client.
func smtpDeliver(c *smtp.Client, from string, to, cc []string, subject, body, replyToMessageID string) error {
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	allRecipients := append([]string{}, to...)
	allRecipients = append(allRecipients, cc...)
	for _, rcpt := range allRecipients {
		addr, err := parseEmailAddress(rcpt)
		if err != nil {
			return fmt.Errorf("invalid recipient %q: %w", rcpt, err)
		}
		if err := c.Rcpt(addr); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", addr, err)
		}
	}
	msg := buildMessage(from, to, cc, subject, body, replyToMessageID)
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := wc.Write([]byte(msg)); err != nil {
		wc.Close()
		return fmt.Errorf("write message body: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}
	return nil
}

// smtpSendMail sends a plain-text email via SMTP. When the auth config carries
// an OAuth2 token, XOAUTH2 is used; otherwise PLAIN auth. On OAuth2 auth
// failure the token is force-refreshed and the send is retried once on a fresh
// connection.
func smtpSendMail(auth mailAuthConfig, from string, to, cc []string, subject, body, replyToMessageID string) error {
	hostPart, port := splitHostPort(auth.Host)
	if port == "" {
		port = "587"
	}
	fullHost := hostPart + ":" + port
	log.Printf("connecting to SMTP server %s", fullHost)

	if auth.useOAuth2() {
		return smtpSendOAuth2(auth, hostPart, fullHost, from, to, cc, subject, body, replyToMessageID)
	}
	return smtpSendPlain(auth, hostPart, fullHost, from, to, cc, subject, body, replyToMessageID)
}

// smtpSendPlain authenticates with a PLAIN password and sends the message.
func smtpSendPlain(auth mailAuthConfig, hostPart, fullHost, from string, to, cc []string, subject, body, replyToMessageID string) error {
	c, err := smtpDial(fullHost, hostPart)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Auth(smtp.PlainAuth("", auth.User, auth.Password, hostPart)); err != nil {
		return fmt.Errorf("SMTP auth: %w", err)
	}
	if err := smtpDeliver(c, from, to, cc, subject, body, replyToMessageID); err != nil {
		return err
	}
	if err := c.Quit(); err != nil {
		log.Printf("SMTP QUIT warning: %v", err)
	}
	log.Printf("email sent to %s (subject: %s)", strings.Join(to, ", "), subject)
	return nil
}

// smtpSendOAuth2 authenticates with XOAUTH2 and sends the message. On auth
// failure it force-refreshes the token and retries once on a new connection.
func smtpSendOAuth2(auth mailAuthConfig, hostPart, fullHost, from string, to, cc []string, subject, body, replyToMessageID string) error {
	accessToken, err := getOAuth2AccessTokenFn(*auth.OAuth2, auth.Token)
	if err != nil {
		return fmt.Errorf("oauth2 get access token: %w", err)
	}

	c, err := smtpDial(fullHost, hostPart)
	if err != nil {
		return err
	}
	if err := c.Auth(&xoauth2SMTPAuth{user: auth.User, token: accessToken}); err == nil {
		return smtpFinish(c, from, to, cc, subject, body, replyToMessageID)
	}

	// Auth failed — force a token refresh and retry on a fresh connection.
	log.Printf("SMTP XOAUTH2 auth failed: %v; refreshing token and retrying", err)
	c.Close()
	expired := *auth.Token
	expired.Expiry = time.Now().Add(-time.Hour)
	refreshed, rerr := getOAuth2AccessTokenFn(*auth.OAuth2, &expired)
	if rerr != nil {
		return fmt.Errorf("oauth2 refresh token: %w", rerr)
	}
	c2, err := smtpDial(fullHost, hostPart)
	if err != nil {
		return err
	}
	if err := c2.Auth(&xoauth2SMTPAuth{user: auth.User, token: refreshed}); err != nil {
		c2.Close()
		return fmt.Errorf("SMTP XOAUTH2 auth: %w", err)
	}
	return smtpFinish(c2, from, to, cc, subject, body, replyToMessageID)
}

// smtpFinish delivers the message, quits, and logs the result.
func smtpFinish(c *smtp.Client, from string, to, cc []string, subject, body, replyToMessageID string) error {
	defer c.Close()
	if err := smtpDeliver(c, from, to, cc, subject, body, replyToMessageID); err != nil {
		return err
	}
	if err := c.Quit(); err != nil {
		log.Printf("SMTP QUIT warning: %v", err)
	}
	log.Printf("email sent to %s (subject: %s)", strings.Join(to, ", "), subject)
	return nil
}

// buildMessage constructs a MIME message as a string.
func buildMessage(from string, to, cc []string, subject, body, replyToMessageID string) string {
	var b strings.Builder

	// Headers.
	b.WriteString(fmt.Sprintf("From: %s\r\n", from))
	b.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	if len(cc) > 0 {
		b.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(cc, ", ")))
	}
	b.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	b.WriteString(fmt.Sprintf("Date: %s\r\n", mailDateFormat()))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	if replyToMessageID != "" {
		b.WriteString(fmt.Sprintf("In-Reply-To: %s\r\n", replyToMessageID))
		b.WriteString(fmt.Sprintf("References: %s\r\n", replyToMessageID))
	}
	b.WriteString("\r\n")
	b.WriteString(body)
	b.WriteString("\r\n")

	return b.String()
}

// mailDateFormat returns the current time formatted as RFC 2822.
func mailDateFormat() string {
	return time.Now().Format("Mon, 02 Jan 2006 15:04:05 -0700")
}

// splitHostPort splits a host:port string. If no port is present, returns
// the host and an empty string.
func splitHostPort(addr string) (host, port string) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, ""
	}
	return addr[:i], addr[i+1:]
}

// parseEmailAddress extracts the bare email address from a string that might
// include a display name (e.g. "John <john@example.com>" → "john@example.com").
func parseEmailAddress(s string) (string, error) {
	s = strings.TrimSpace(s)
	addr, err := mail.ParseAddress(s)
	if err != nil {
		return "", err
	}
	return addr.Address, nil
}

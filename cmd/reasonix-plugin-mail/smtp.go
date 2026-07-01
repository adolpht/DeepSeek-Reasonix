package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
)

// smtpConfig reads SMTP connection settings from environment variables.
func smtpConfig() (host, user, pass string, err error) {
	host = os.Getenv("MAIL_SMTP_HOST")
	user = os.Getenv("MAIL_SMTP_USER")
	pass = os.Getenv("MAIL_SMTP_PASS")
	if host == "" || user == "" || pass == "" {
		return "", "", "", fmt.Errorf(
			"mail SMTP not configured. Set environment variables: MAIL_SMTP_HOST, MAIL_SMTP_USER, MAIL_SMTP_PASS",
		)
	}
	return host, user, pass, nil
}

// smtpSendMail sends a plain-text email via SMTP with TLS (STARTTLS).
func smtpSendMail(host, user, pass, from string, to, cc []string, subject, body, replyToMessageID string) error {
	// Parse host and port.
	hostPart, port := splitHostPort(host)
	if port == "" {
		port = "587"
	}
	fullHost := hostPart + ":" + port

	log.Printf("connecting to SMTP server %s", fullHost)

	// Connect to the SMTP server.
	c, err := smtp.Dial(fullHost)
	if err != nil {
		return fmt.Errorf("connect to SMTP server %s: %w", fullHost, err)
	}
	defer c.Close()

	// Send EHLO.
	if err := c.Hello("localhost"); err != nil {
		return fmt.Errorf("EHLO: %w", err)
	}

	// Upgrade to TLS via STARTTLS.
	tlsConfig := &tls.Config{
		ServerName:         hostPart,
		InsecureSkipVerify: true,
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}

	// Authenticate.
	auth := smtp.PlainAuth("", user, pass, hostPart)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("SMTP auth: %w", err)
	}

	// Set the sender.
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}

	// Add recipients.
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

	// Build the message.
	msg := buildMessage(from, to, cc, subject, body, replyToMessageID)

	// Send the data.
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
	return mail.Now().Format("Mon, 02 Jan 2006 15:04:05 -0700")
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

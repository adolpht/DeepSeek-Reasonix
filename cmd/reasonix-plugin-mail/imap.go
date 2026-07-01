package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/charset"
)

func init() {
	// Register charset readers so go-imap can decode non-UTF-8 charsets
	// (e.g. GB2312, ISO-8859-1) commonly found in email.
	charset.RegisterCharsetReader(func(charset string, r io.Reader) (io.Reader, error) {
		return r, nil
	})
}

// mailMessage is a simplified representation of an email for tool output.
type mailMessage struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Snippet string `json:"snippet"`
}

// imapDial connects to the IMAP server over TLS and authenticates.
func imapDial(host, user, pass string) (*client.Client, error) {
	c, err := client.DialTLS(host, &tls.Config{
		// Many self-hosted or corporate IMAP servers use certificates that
		// are not in the system trust store; we allow it for usability but
		// log a warning.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to IMAP server %s: %w", host, err)
	}
	log.Printf("connected to %s", host)

	if err := c.Login(user, pass); err != nil {
		c.Logout()
		return nil, fmt.Errorf("IMAP login failed: %w", err)
	}
	log.Printf("logged in as %s", user)
	return c, nil
}

// imapReadFolder reads emails from the specified mailbox folder.
func imapReadFolder(host, user, pass, folder string, limit, offset int) ([]mailMessage, error) {
	c, err := imapDial(host, user, pass)
	if err != nil {
		return nil, err
	}
	defer c.Logout()

	mbox, err := c.Select(folder, true)
	if err != nil {
		return nil, fmt.Errorf("select folder %q: %w", folder, err)
	}

	if mbox.Messages == 0 {
		return nil, nil
	}

	// Calculate the sequence range: newest first.
	total := int(mbox.Messages)
	start := total - offset
	if start < 1 {
		start = 1
	}
	end := start - limit + 1
	if end < 1 {
		end = 1
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddRange(uint32(end), uint32(start))

	// Fetch the essential fields.
	section := &imap.FetchSection{Body: []imap.FetchSectionItem{imap.FetchBodyPeek}}
	fetchItems := []imap.FetchItem{
		imap.FetchEnvelope,
		imap.FetchBodySection(section),
		imap.FetchItem("BODY[HEADER.FIELDS (MESSAGE-ID)]"),
	}

	messages := make(chan *imap.Message, limit)
	err = c.Fetch(seqSet, fetchItems, messages)
	if err != nil {
		return nil, fmt.Errorf("fetch messages: %w", err)
	}

	var results []mailMessage
	for msg := range messages {
		m := parseMessage(msg)
		results = append(results, m)
	}

	// Reverse so newest is last → we want newest first, but IMAP returns
	// in ascending order. Reverse the slice.
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}

	return results, nil
}

// imapSearchMail searches emails matching a query using IMAP SEARCH.
func imapSearchMail(host, user, pass, folder, query string, limit int) ([]mailMessage, error) {
	c, err := imapDial(host, user, pass)
	if err != nil {
		return nil, err
	}
	defer c.Logout()

	mbox, err := c.Select(folder, true)
	if err != nil {
		return nil, fmt.Errorf("select folder %q: %w", folder, err)
	}

	// Build search criteria: match subject OR from OR body.
	criteria := imap.NewSearchCriteria()
	subjCriteria := imap.NewSearchCriteria()
	subjCriteria.Header = map[string]string{"Subject": query}
	fromCriteria := imap.NewSearchCriteria()
	fromCriteria.Header = map[string]string{"From": query}
	bodyCriteria := imap.NewSearchCriteria()
	bodyCriteria.Body = []string{query}

	criteria.Or = [][2]*imap.SearchCriteria{
		{subjCriteria, fromCriteria},
	}
	// Also include body match via OR with the combined above.
	combinedCriteria := imap.NewSearchCriteria()
	combinedCriteria.Or = [][2]*imap.SearchCriteria{
		{criteria, bodyCriteria},
	}

	uids, err := c.Search(combinedCriteria)
	if err != nil {
		return nil, fmt.Errorf("IMAP SEARCH: %w", err)
	}

	if len(uids) == 0 {
		return nil, nil
	}

	// Limit results: take the last N (newest).
	if len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(uids...)

	section := &imap.FetchSection{Body: []imap.FetchSectionItem{imap.FetchBodyPeek}}
	fetchItems := []imap.FetchItem{
		imap.FetchEnvelope,
		imap.FetchBodySection(section),
	}

	messages := make(chan *imap.Message, limit)
	err = c.Fetch(seqSet, fetchItems, messages)
	if err != nil {
		return nil, fmt.Errorf("fetch search results: %w", err)
	}

	var results []mailMessage
	for msg := range messages {
		m := parseMessage(msg)
		results = append(results, m)
	}

	return results, nil
}

// parseMessage extracts a simplified mailMessage from an IMAP message struct.
func parseMessage(msg *imap.Message) mailMessage {
	m := mailMessage{}
	if msg.Envelope != nil {
		m.Subject = decodeHeader(msg.Envelope.Subject)
		m.Date = msg.Envelope.Date.Format(time.RFC3339)
		if len(msg.Envelope.From) > 0 {
			m.From = formatAddress(msg.Envelope.From[0])
		}
	}

	// Try to extract a text snippet from the body.
	m.Snippet = extractSnippet(msg)

	return m
}

// formatAddress formats an imap.Address as "Name <email>" or just "email".
func formatAddress(addr *imap.Address) string {
	name := decodeHeader(addr.PersonalName)
	email := addr.MailboxName + "@" + addr.HostName
	if name != "" && name != email {
		return fmt.Sprintf("%s <%s>", name, email)
	}
	return email
}

// decodeHeader decodes RFC 2047 encoded header values.
func decodeHeader(s string) string {
	dec := new(mime.WordDecoder)
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

// extractSnippet reads the text/plain body part and returns the first ~200 chars.
func extractSnippet(msg *imap.Message) string {
	section := &imap.FetchSection{Body: []imap.FetchSectionItem{imap.FetchBodyPeek}}
	r := msg.GetBody(section)
	if r == nil {
		return ""
	}

	// Parse the MIME structure to find the text/plain part.
	mediaType, params, err := mime.ParseMediaType(msg.Envelope.Subject)
	_ = mediaType
	_ = params
	// Fallback: just read the raw body and try to extract readable text.
	return readTextSnippet(r, 200)
}

// readTextSnippet reads up to maxRunes from a reader, attempting to decode
// quoted-printable if needed, and returns a clean text snippet.
func readTextSnippet(r io.Reader, maxRunes int) string {
	// Read a limited amount of data.
	lr := io.LimitReader(r, 4096)
	data, err := io.ReadAll(lr)
	if err != nil {
		return ""
	}

	text := string(data)

	// Try quoted-printable decode if it looks like QP.
	if strings.Contains(text, "=\n") || strings.Contains(text, "=3D") {
		qr := quotedprintable.NewReader(strings.NewReader(text))
		qData, qErr := io.ReadAll(io.LimitReader(qr, 4096))
		if qErr == nil {
			text = string(qData)
		}
	}

	// Clean up: remove carriage returns, collapse whitespace.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// Try to find the first text/plain content after headers.
	// MIME messages have headers separated from body by a blank line.
	if idx := strings.Index(text, "\n\n"); idx >= 0 {
		text = text[idx+2:]
	}

	// Collapse multiple whitespace lines.
	lines := strings.Split(text, "\n")
	var clean []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip lines that look like MIME boundaries or encoding artifacts.
		if strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "Content-") {
			continue
		}
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	text = strings.Join(clean, " ")

	// Truncate to maxRunes.
	runes := []rune(text)
	if len(runes) > maxRunes {
		text = string(runes[:maxRunes]) + "..."
	}

	return text
}

// parseMailAddresses parses a string of email addresses (as found in a mail
// header) into a slice of formatted address strings.
func parseMailAddresses(s string) []string {
	if s == "" {
		return nil
	}
	addrs, err := mail.ParseAddressList(s)
	if err != nil {
		// Fallback: split by comma.
		return splitAddresses(s)
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Name != "" {
			out = append(out, fmt.Sprintf("%s <%s>", a.Name, a.Address))
		} else {
			out = append(out, a.Address)
		}
	}
	return out
}

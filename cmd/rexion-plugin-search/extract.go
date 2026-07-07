package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// webExtractTool fetches a web page and extracts its text content, optionally
// filtered by an extraction goal. No API key required — uses net/http directly.
var webExtractTool = toolDef{
	name: "web_extract",
	description: "Extract structured information from a web page. Fetches the page, parses HTML, " +
		"and returns the text content. Use extract_goal to focus on specific information (e.g. 'pricing', 'features').",
	readOnly: true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":          map[string]any{"type": "string", "description": "URL of the web page to extract from"},
			"extract_goal": map[string]any{"type": "string", "description": "What to extract from the page (e.g. 'pricing', 'features', 'contact info'). Guides extraction focus."},
		},
		"required": []string{"url"},
	},
	run: runWebExtract,
}

func runWebExtract(args map[string]any) (any, error) {
	rawURL, err := argString(args, "url")
	if err != nil {
		return nil, err
	}
	extractGoal := argStringDefault(args, "extract_goal", "")

	// Validate URL.
	parsedURL, err := parseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	// Fetch the page.
	body, contentType, err := fetchPage(parsedURL.String())
	if err != nil {
		return nil, fmt.Errorf("fetch page: %w", err)
	}

	// Extract text from HTML.
	text, title, err := extractTextFromHTML(body, contentType)
	if err != nil {
		return nil, fmt.Errorf("extract text: %w", err)
	}

	// If extract_goal is specified, try to find the most relevant section.
	if extractGoal != "" {
		text = focusExtraction(text, extractGoal)
	}

	// Build result.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Title: %s\n", title))
	b.WriteString(fmt.Sprintf("URL: %s\n", parsedURL.String()))
	if extractGoal != "" {
		b.WriteString(fmt.Sprintf("Extraction goal: %s\n", extractGoal))
	}
	b.WriteString("\n---\n\n")
	b.WriteString(text)

	return b.String(), nil
}

// parseURL validates and normalizes a URL string.
func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q (want http/https)", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("URL has no host")
	}
	return u, nil
}

// fetchPage downloads a web page with a reasonable timeout and User-Agent.
func fetchPage(urlStr string) (string, string, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "RexionSearchPlugin/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Limit response body to 2MB to avoid memory issues.
	limited := io.LimitReader(resp.Body, 2*1024*1024)
	bodyBytes, err := io.ReadAll(limited)
	if err != nil {
		return "", "", fmt.Errorf("read body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	return string(bodyBytes), contentType, nil
}

// extractTextFromHTML parses HTML and extracts visible text content.
func extractTextFromHTML(htmlStr, contentType string) (string, string, error) {
	// Skip non-HTML content types — return as-is for plain text.
	if !strings.Contains(contentType, "html") && !strings.Contains(contentType, "xhtml") {
		// Heuristic: if the body starts with <, treat as HTML anyway.
		trimmed := strings.TrimSpace(htmlStr)
		if len(trimmed) == 0 || trimmed[0] != '<' {
			return htmlStr, "", nil
		}
	}

	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return "", "", fmt.Errorf("parse HTML: %w", err)
	}

	var title string
	var b strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Skip script, style, nav, footer, header — not main content.
			switch n.Data {
			case "script", "style", "noscript", "nav", "footer", "header", "aside":
				return
			case "title":
				title = extractNodeText(n)
				return
			case "h1", "h2", "h3", "h4", "h5", "h6":
				b.WriteString("\n\n" + strings.Repeat("#", headingLevel(n.Data)) + " " + extractNodeText(n) + "\n")
				return
			case "p", "div", "section", "article", "li", "td", "th", "dd", "dt", "blockquote":
				text := extractNodeText(n)
				if strings.TrimSpace(text) != "" {
					b.WriteString(text + "\n")
				}
				return
			case "br":
				b.WriteString("\n")
				return
			case "tr":
				b.WriteString(extractNodeText(n) + "\n")
				return
			case "a":
				href := getAttr(n, "href")
				text := extractNodeText(n)
				if href != "" && text != "" {
					b.WriteString(fmt.Sprintf("[%s](%s)", text, href))
				} else if text != "" {
					b.WriteString(text)
				}
				return
			}
		}
		if n.Type == html.TextNode {
			text := n.Data
			if strings.TrimSpace(text) != "" {
				b.WriteString(text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	// Clean up the output: collapse multiple blank lines, trim whitespace.
	result := collapseBlankLines(b.String())
	return result, title, nil
}

// extractNodeText returns all text content within a node (recursively).
func extractNodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// getAttr returns the value of the named attribute, or empty string.
func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// headingLevel maps HTML heading tag names to a numeric level.
func headingLevel(tag string) int {
	switch tag {
	case "h1":
		return 1
	case "h2":
		return 2
	case "h3":
		return 3
	case "h4":
		return 4
	case "h5":
		return 5
	case "h6":
		return 6
	default:
		return 2
	}
}

// focusExtraction attempts to find the most relevant section of text based on
// the extraction goal. It splits the text into paragraphs and ranks them by
// keyword overlap with the goal.
func focusExtraction(text, goal string) string {
	goalLower := strings.ToLower(goal)
	goalWords := splitWords(goalLower)

	paragraphs := strings.Split(text, "\n\n")
	if len(paragraphs) <= 3 {
		return text // Too few paragraphs to filter meaningfully.
	}

	// Score each paragraph by keyword overlap with the goal.
	type scored struct {
		text  string
		score int
	}
	scoredParas := make([]scored, 0, len(paragraphs))
	for _, p := range paragraphs {
		pLower := strings.ToLower(p)
		pWords := splitWords(pLower)
		overlap := 0
		for _, gw := range goalWords {
			for _, pw := range pWords {
				if gw == pw || strings.Contains(pw, gw) || strings.Contains(gw, pw) {
					overlap++
					break
				}
			}
		}
		scoredParas = append(scoredParas, scored{text: p, score: overlap})
	}

	// Collect paragraphs with non-zero scores, plus their neighbors for context.
	relevant := make(map[int]bool)
	for i, sp := range scoredParas {
		if sp.score > 0 {
			relevant[i] = true
			if i > 0 {
				relevant[i-1] = true
			}
			if i < len(scoredParas)-1 {
				relevant[i+1] = true
			}
		}
	}

	if len(relevant) == 0 {
		// No relevant paragraphs found; return the first 5000 chars.
		if len(text) > 5000 {
			return text[:5000] + "\n\n... (truncated, no section matched the extraction goal)"
		}
		return text
	}

	var b strings.Builder
	for i, p := range paragraphs {
		if relevant[i] {
			b.WriteString(p + "\n\n")
		}
	}
	return b.String()
}

// splitWords splits a string into lowercase words for matching.
func splitWords(s string) []string {
	s = strings.ToLower(s)
	// Split on non-alphanumeric characters.
	var words []string
	var current strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= '\u4e00' && r <= '\u9fff' {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

// collapseBlankLines reduces runs of 3+ blank lines to 2.
func collapseBlankLines(s string) string {
	// Replace runs of 3+ newlines with 2 newlines.
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

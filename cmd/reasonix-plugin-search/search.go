package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// webSearchTool searches the web using a configured search API (SerpAPI or Bing).
// Requires SEARCH_API_KEY environment variable; SEARCH_API_PROVIDER selects the
// backend (default: serpapi).
var webSearchTool = toolDef{
	name: "web_search",
	description: "Search the web for information. Returns a list of results with title, snippet, and url. " +
		"Requires SEARCH_API_KEY environment variable; uses SEARCH_API_PROVIDER (serpapi or bing, default serpapi).",
	readOnly: true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":       map[string]any{"type": "string", "description": "Search query string"},
			"max_results": map[string]any{"type": "integer", "description": "Maximum number of results to return (default 5, max 20)"},
		},
		"required": []string{"query"},
	},
	run: runWebSearch,
}

// searchResult is one item in the search results.
type searchResult struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	URL     string `json:"url"`
}

func runWebSearch(args map[string]any) (any, error) {
	query, err := argString(args, "query")
	if err != nil {
		return nil, err
	}
	maxResults := argIntDefault(args, "max_results", 5)
	if maxResults < 1 {
		maxResults = 1
	}
	if maxResults > 20 {
		maxResults = 20
	}

	apiKey := os.Getenv("SEARCH_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("SEARCH_API_KEY environment variable is not set. " +
			"Please set SEARCH_API_KEY to your search API key. " +
			"Optionally set SEARCH_API_PROVIDER to 'serpapi' (default) or 'bing'. " +
			"Get a SerpAPI key at https://serpapi.com/manage-api-key")
	}

	provider := os.Getenv("SEARCH_API_PROVIDER")
	if provider == "" {
		provider = "serpapi"
	}

	var results []searchResult
	switch strings.ToLower(provider) {
	case "bing":
		results, err = searchBing(apiKey, query, maxResults)
	case "serpapi":
		results, err = searchSerpAPI(apiKey, query, maxResults)
	default:
		return nil, fmt.Errorf("unsupported SEARCH_API_PROVIDER %q (use 'serpapi' or 'bing')", provider)
	}
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Format as readable text.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Search results for %q (provider: %s, count: %d):\n\n", query, provider, len(results)))
	for i, r := range results {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
		b.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		b.WriteString(fmt.Sprintf("   %s\n\n", r.URL))
	}
	return b.String(), nil
}

// --- SerpAPI ---

func searchSerpAPI(apiKey, query string, maxResults int) ([]searchResult, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("api_key", apiKey)
	params.Set("engine", "google")
	params.Set("num", fmt.Sprintf("%d", maxResults))
	params.Set("hl", "en")

	reqURL := "https://serpapi.com/search?" + params.Encode()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("serpapi request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read serpapi response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("serpapi returned status %d: %s", resp.StatusCode, string(body))
	}

	var serpResp struct {
		OrganicResults []struct {
			Title   string `json:"title"`
			Snippet string `json:"snippet"`
			Link    string `json:"link"`
		} `json:"organic_results"`
		SearchMetadata struct {
			Status string `json:"status"`
		} `json:"search_metadata"`
	}
	if err := json.Unmarshal(body, &serpResp); err != nil {
		return nil, fmt.Errorf("parse serpapi response: %w", err)
	}

	results := make([]searchResult, 0, len(serpResp.OrganicResults))
	for _, r := range serpResp.OrganicResults {
		if len(results) >= maxResults {
			break
		}
		results = append(results, searchResult{
			Title:   r.Title,
			Snippet: r.Snippet,
			URL:     r.Link,
		})
	}
	return results, nil
}

// --- Bing Search API ---

func searchBing(apiKey, query string, maxResults int) ([]searchResult, error) {
	reqURL := fmt.Sprintf("https://api.bing.microsoft.com/v7.0/search?q=%s&count=%d",
		url.QueryEscape(query), maxResults)

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create bing request: %w", err)
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read bing response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bing returned status %d: %s", resp.StatusCode, string(body))
	}

	var bingResp struct {
		WebPages struct {
			Value []struct {
				Name        string `json:"name"`
				Snippet     string `json:"snippet"`
				URL         string `json:"url"`
			} `json:"value"`
		} `json:"webPages"`
	}
	if err := json.Unmarshal(body, &bingResp); err != nil {
		return nil, fmt.Errorf("parse bing response: %w", err)
	}

	results := make([]searchResult, 0, len(bingResp.WebPages.Value))
	for _, r := range bingResp.WebPages.Value {
		results = append(results, searchResult{
			Title:   r.Name,
			Snippet: r.Snippet,
			URL:     r.URL,
		})
	}
	return results, nil
}

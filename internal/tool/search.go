package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ToolSearch provides BM25-based search over the tool registry.
type ToolSearch struct {
	mu       sync.RWMutex
	registry *Registry
	index    *BM25Index
	docs     []toolDoc
	snapshot toolSnapshot // last-indexed registry state
}

type toolDoc struct {
	Name        string
	Description string
	Category    string // "builtin" | "mcp" | "skill"
	Server      string // MCP server name (empty for builtins)
}

type toolSnapshot struct {
	count int
	names string // sorted, joined for quick comparison
}

// ToolSearchTool implements the Tool interface for tool_search.
type ToolSearchTool struct {
	search *ToolSearch
}

// NewToolSearchTool creates a tool_search tool backed by the given registry.
func NewToolSearchTool(registry *Registry) *ToolSearchTool {
	return &ToolSearchTool{
		search: &ToolSearch{
			registry: registry,
		},
	}
}

func (t *ToolSearchTool) Name() string { return "tool_search" }

func (t *ToolSearchTool) Description() string {
	return "Search available tools by keyword or function description. Returns matching tools with relevance scores. Use this when you need to find the right tool for a task but aren't sure which one to use, especially when many MCP tools are available."
}

func (t *ToolSearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "query":{"type":"string","description":"Search keywords or functional description"},
  "category":{"type":"string","description":"Filter by category: builtin, mcp, or skill"},
  "limit":{"type":"integer","description":"Maximum results to return (default 10)","minimum":1,"maximum":50}
},
"required":["query"]
}`)
}

func (t *ToolSearchTool) ReadOnly() bool { return true }

func (t *ToolSearchTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query    string `json:"query"`
		Category string `json:"category"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if p.Limit <= 0 {
		p.Limit = 10
	}
	if p.Limit > 50 {
		p.Limit = 50
	}

	results, err := t.search.Search(p.Query, p.Category, p.Limit)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No matching tools found.", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d matching tools:\n", len(results))
	for i, r := range results {
		if r.Category == "mcp" && r.Server != "" {
			fmt.Fprintf(&b, "\n%d. %s (mcp: %s) [score: %.1f]\n", i+1, r.Name, r.Server, r.Score)
		} else {
			fmt.Fprintf(&b, "\n%d. %s (%s) [score: %.1f]\n", i+1, r.Name, r.Category, r.Score)
		}
		if r.Description != "" {
			// Truncate long descriptions for readability.
			desc := r.Description
			if len(desc) > 120 {
				desc = desc[:117] + "..."
			}
			fmt.Fprintf(&b, "   %s\n", desc)
		}
	}
	return b.String(), nil
}

// SearchResult is a single tool search result with metadata.
type SearchResult struct {
	Name        string
	Score       float64
	Description string
	Category    string
	Server      string
}

// Search performs a BM25 search over the tool registry.
func (ts *ToolSearch) Search(query, category string, limit int) ([]SearchResult, error) {
	ts.ensureIndex()

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	bm25Results := ts.index.Search(query, limit*2) // fetch extra for category filtering

	var results []SearchResult
	for _, r := range bm25Results {
		// Find the toolDoc by ID.
		var doc *toolDoc
		for i := range ts.docs {
			if ts.docs[i].Name == r.ID {
				doc = &ts.docs[i]
				break
			}
		}
		if doc == nil {
			continue
		}

		// Filter by category if specified.
		if category != "" && doc.Category != category {
			continue
		}

		results = append(results, SearchResult{
			Name:        doc.Name,
			Score:       r.Score,
			Description: doc.Description,
			Category:    doc.Category,
			Server:      doc.Server,
		})
		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

// ensureIndex lazily builds or rebuilds the BM25 index when the registry changes.
func (ts *ToolSearch) ensureIndex() {
	ts.mu.RLock()
	current := ts.registrySnapshot()
	if ts.index != nil && ts.snapshot == current {
		ts.mu.RUnlock()
		return
	}
	ts.mu.RUnlock()

	// Rebuild under write lock.
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Double-check after acquiring write lock.
	current = ts.registrySnapshot()
	if ts.index != nil && ts.snapshot == current {
		return
	}

	ts.rebuildIndexLocked()
}

// registrySnapshot captures the current registry state for change detection.
func (ts *ToolSearch) registrySnapshot() toolSnapshot {
	names := ts.registry.Names()
	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Strings(sorted)
	return toolSnapshot{
		count: len(names),
		names: strings.Join(sorted, ","),
	}
}

// rebuildIndexLocked rebuilds the BM25 index from the registry. Caller must hold ts.mu.
func (ts *ToolSearch) rebuildIndexLocked() {
	names := ts.registry.Names()
	docs := make([]BM25Doc, 0, len(names))
	ts.docs = make([]toolDoc, 0, len(names))

	for _, name := range names {
		t, ok := ts.registry.Get(name)
		if !ok {
			continue
		}

		category := "builtin"
		server := ""
		if serverName, _, isMCP := SplitMCPName(name); isMCP {
			category = "mcp"
			server = serverName
		}

		desc := t.Description()
		schemaText := extractSchemaParamNames(t.Schema())

		// Build weighted text: name tokens repeated 3x, description 1x,
		// schema param names 1.5x (rounded to 2), server name 0.5x (rounded to 1).
		var b strings.Builder

		// Tool name: weight 3.0 (repeat 3 times)
		for i := 0; i < 3; i++ {
			b.WriteString(name)
			b.WriteByte(' ')
		}

		// Description: weight 1.0
		b.WriteString(desc)
		b.WriteByte(' ')

		// Schema parameter names: weight 1.5 (repeat 2 times)
		for i := 0; i < 2; i++ {
			b.WriteString(schemaText)
			b.WriteByte(' ')
		}

		// MCP server name: weight 0.5 (repeat 1 time)
		if server != "" {
			b.WriteString(server)
			b.WriteByte(' ')
		}

		// Category label
		b.WriteString(category)

		docs = append(docs, BM25Doc{
			ID:   name,
			Text: b.String(),
		})
		ts.docs = append(ts.docs, toolDoc{
			Name:        name,
			Description: desc,
			Category:    category,
			Server:      server,
		})
	}

	ts.index = NewBM25Index(docs)
	ts.snapshot = ts.registrySnapshot()
}

// extractSchemaParamNames extracts parameter names from a JSON Schema object.
func extractSchemaParamNames(schema json.RawMessage) string {
	if len(schema) == 0 {
		return ""
	}
	var s struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return ""
	}
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, " ")
}

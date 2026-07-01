package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// --- Tool registration tests ---

func TestToolListRegistration(t *testing.T) {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.name)
	}
	expected := []string{"web_search", "web_extract", "compare_table"}
	for _, e := range expected {
		if !containsStr(names, e) {
			t.Errorf("missing tool %q in %v", e, names)
		}
	}
	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}
}

func TestToolListHasSchemas(t *testing.T) {
	list := toolList()
	if len(list) != 3 {
		t.Fatalf("expected 3 tool entries, got %d", len(list))
	}
	for _, entry := range list {
		name, _ := entry["name"].(string)
		if name == "" {
			t.Error("tool entry missing name")
		}
		if _, ok := entry["inputSchema"]; !ok {
			t.Errorf("tool %q missing inputSchema", name)
		}
		if _, ok := entry["description"]; !ok {
			t.Errorf("tool %q missing description", name)
		}
		if ann, ok := entry["annotations"].(map[string]any); ok {
			if _, ok := ann["readOnlyHint"]; !ok {
				t.Errorf("tool %q annotations missing readOnlyHint", name)
			}
		} else {
			t.Errorf("tool %q missing annotations", name)
		}
	}
}

// --- web_search tests ---

func TestRunWebSearchMissingQuery(t *testing.T) {
	_, err := runWebSearch(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing query")
	}
	if !strings.Contains(err.Error(), "query") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunWebSearchMissingAPIKey(t *testing.T) {
	t.Setenv("SEARCH_API_KEY", "")
	res, err := runWebSearch(map[string]any{
		"query": "test query",
	})
	if err == nil {
		s, ok := res.(string)
		if !ok || !strings.Contains(s, "SEARCH_API_KEY") {
			t.Error("expected error or message about missing SEARCH_API_KEY")
		}
	} else {
		if !strings.Contains(err.Error(), "SEARCH_API_KEY") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

func TestRunWebSearchUnsupportedProvider(t *testing.T) {
	t.Setenv("SEARCH_API_KEY", "test-key")
	t.Setenv("SEARCH_API_PROVIDER", "duckduckgo")
	_, err := runWebSearch(map[string]any{
		"query": "test query",
	})
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported SEARCH_API_PROVIDER") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunWebSearchMaxResultsClamping(t *testing.T) {
	t.Setenv("SEARCH_API_KEY", "")
	_, _ = runWebSearch(map[string]any{
		"query":       "test",
		"max_results": 0,
	})
	_, _ = runWebSearch(map[string]any{
		"query":       "test",
		"max_results": 100,
	})
}

// --- URL parsing tests ---

func TestParseURLValid(t *testing.T) {
	u, err := parseURL("https://example.com/path")
	if err != nil {
		t.Fatalf("parseURL failed: %v", err)
	}
	if u.Scheme != "https" {
		t.Errorf("scheme = %q, want 'https'", u.Scheme)
	}
	if u.Host != "example.com" {
		t.Errorf("host = %q, want 'example.com'", u.Host)
	}
}

func TestParseURLNoScheme(t *testing.T) {
	// "example.com/page" is parsed as scheme=example.com, opaque=//page by url.Parse,
	// so host is empty. This is expected — users should provide a full URL with scheme.
	_, err := parseURL("example.com/page")
	if err == nil {
		t.Fatal("expected error for URL without scheme (parsed as opaque)")
	}
}

func TestParseURLUnsupportedScheme(t *testing.T) {
	_, err := parseURL("ftp://example.com/file")
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
	if !strings.Contains(err.Error(), "unsupported scheme") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseURLNoHost(t *testing.T) {
	_, err := parseURL("https://")
	if err == nil {
		t.Fatal("expected error for URL with no host")
	}
	if !strings.Contains(err.Error(), "no host") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseURLInvalid(t *testing.T) {
	_, err := parseURL("://invalid")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

// --- web_extract tests ---

func TestRunWebExtractMissingURL(t *testing.T) {
	_, err := runWebExtract(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing url")
	}
	if !strings.Contains(err.Error(), "url") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunWebExtractInvalidURL(t *testing.T) {
	_, err := runWebExtract(map[string]any{
		"url": "ftp://bad.scheme.com",
	})
	if err == nil {
		t.Fatal("expected error for invalid URL scheme")
	}
	if !strings.Contains(err.Error(), "invalid URL") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- HTML extraction tests ---

func TestExtractTextFromHTML(t *testing.T) {
	htmlStr := `<html><head><title>Test Page</title></head><body><h1>Hello</h1><p>World</p></body></html>`
	text, title, err := extractTextFromHTML(htmlStr, "text/html")
	if err != nil {
		t.Fatalf("extractTextFromHTML failed: %v", err)
	}
	if title != "Test Page" {
		t.Errorf("title = %q, want 'Test Page'", title)
	}
	if !strings.Contains(text, "Hello") || !strings.Contains(text, "World") {
		t.Errorf("text missing expected content: %s", text)
	}
}

func TestExtractTextFromHTMLSkipsScript(t *testing.T) {
	htmlStr := `<html><body><p>Visible</p><script>var x = 1;</script></body></html>`
	text, _, err := extractTextFromHTML(htmlStr, "text/html")
	if err != nil {
		t.Fatalf("extractTextFromHTML failed: %v", err)
	}
	if strings.Contains(text, "var x") {
		t.Error("script content should be excluded")
	}
	if !strings.Contains(text, "Visible") {
		t.Error("visible content should be included")
	}
}

func TestExtractTextFromHTMLPlain(t *testing.T) {
	text, _, err := extractTextFromHTML("Just plain text", "text/plain")
	if err != nil {
		t.Fatalf("extractTextFromHTML failed: %v", err)
	}
	if text != "Just plain text" {
		t.Errorf("text = %q, want 'Just plain text'", text)
	}
}

func TestExtractTextFromHTMLEmpty(t *testing.T) {
	text, _, err := extractTextFromHTML("", "text/html")
	if err != nil {
		t.Fatalf("extractTextFromHTML failed: %v", err)
	}
	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
}

// --- focusExtraction tests ---

func TestFocusExtraction(t *testing.T) {
	text := "Introduction paragraph.\n\nPricing starts at $9.99 per month.\n\nConclusion paragraph."
	result := focusExtraction(text, "pricing")
	if !strings.Contains(result, "$9.99") {
		t.Errorf("focused extraction should contain pricing info, got: %s", result)
	}
}

func TestFocusExtractionShortText(t *testing.T) {
	text := "Only one paragraph."
	result := focusExtraction(text, "anything")
	if result != text {
		t.Errorf("short text should be returned as-is, got: %s", result)
	}
}

func TestFocusExtractionNoMatch(t *testing.T) {
	text := "Paragraph about cats.\n\nParagraph about dogs.\n\nParagraph about fish.\n\nParagraph about birds."
	result := focusExtraction(text, "quantum physics")
	if result == "" {
		t.Error("should return some text even with no match")
	}
}

// --- splitWords tests ---

func TestSplitWords(t *testing.T) {
	words := splitWords("Hello, World! 123")
	expected := []string{"hello", "world", "123"}
	if len(words) != len(expected) {
		t.Fatalf("expected %d words, got %d: %v", len(expected), len(words), words)
	}
	for i, w := range expected {
		if words[i] != w {
			t.Errorf("word[%d] = %q, want %q", i, words[i], w)
		}
	}
}

func TestSplitWordsChinese(t *testing.T) {
	words := splitWords("你好世界")
	if len(words) != 1 || words[0] != "你好世界" {
		t.Errorf("expected ['你好世界'], got %v", words)
	}
}

func TestSplitWordsEmpty(t *testing.T) {
	words := splitWords("")
	if len(words) != 0 {
		t.Errorf("expected empty, got %v", words)
	}
}

// --- collapseBlankLines tests ---

func TestCollapseBlankLines(t *testing.T) {
	input := "line1\n\n\n\n\nline2"
	want := "line1\n\nline2"
	got := collapseBlankLines(input)
	if got != want {
		t.Errorf("collapseBlankLines = %q, want %q", got, want)
	}
}

func TestCollapseBlankLinesNoCollapse(t *testing.T) {
	input := "line1\n\nline2"
	got := collapseBlankLines(input)
	if got != input {
		t.Errorf("collapseBlankLines = %q, want %q", got, input)
	}
}

// --- headingLevel tests ---

func TestHeadingLevel(t *testing.T) {
	cases := []struct {
		tag  string
		want int
	}{
		{"h1", 1},
		{"h2", 2},
		{"h3", 3},
		{"h4", 4},
		{"h5", 5},
		{"h6", 6},
		{"div", 2},
	}
	for _, tc := range cases {
		got := headingLevel(tc.tag)
		if got != tc.want {
			t.Errorf("headingLevel(%q) = %d, want %d", tc.tag, got, tc.want)
		}
	}
}

// --- compare_table tests ---

func TestRunCompareTableMissingDimensions(t *testing.T) {
	_, err := runCompareTable(map[string]any{
		"items": []any{map[string]any{"name": "A", "data": map[string]any{}}},
	})
	if err == nil {
		t.Fatal("expected error for missing dimensions")
	}
	if !strings.Contains(err.Error(), "dimensions") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCompareTableMissingItems(t *testing.T) {
	_, err := runCompareTable(map[string]any{
		"dimensions": []any{"Price"},
	})
	if err == nil {
		t.Fatal("expected error for missing items")
	}
	if !strings.Contains(err.Error(), "items") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCompareTableEmptyDimensions(t *testing.T) {
	_, err := runCompareTable(map[string]any{
		"dimensions": []any{},
		"items":      []any{map[string]any{"name": "A", "data": map[string]any{}}},
	})
	if err == nil {
		t.Fatal("expected error for empty dimensions")
	}
	if !strings.Contains(err.Error(), "non-empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCompareTableEmptyItems(t *testing.T) {
	_, err := runCompareTable(map[string]any{
		"dimensions": []any{"Price"},
		"items":      []any{},
	})
	if err == nil {
		t.Fatal("expected error for empty items")
	}
	if !strings.Contains(err.Error(), "non-empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCompareTableItemMissingName(t *testing.T) {
	_, err := runCompareTable(map[string]any{
		"dimensions": []any{"Price"},
		"items":      []any{map[string]any{"data": map[string]any{}}},
	})
	if err == nil {
		t.Fatal("expected error for item missing name")
	}
	if !strings.Contains(err.Error(), "missing 'name'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCompareTableItemMissingData(t *testing.T) {
	_, err := runCompareTable(map[string]any{
		"dimensions": []any{"Price"},
		"items":      []any{map[string]any{"name": "A"}},
	})
	if err == nil {
		t.Fatal("expected error for item missing data")
	}
	if !strings.Contains(err.Error(), "missing 'data'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCompareTableMarkdown(t *testing.T) {
	res, err := runCompareTable(map[string]any{
		"dimensions": []any{"Price", "Speed"},
		"items": []any{
			map[string]any{
				"name": "Product A",
				"data": map[string]any{"Price": "$10", "Speed": "Fast"},
			},
			map[string]any{
				"name": "Product B",
				"data": map[string]any{"Price": "$20", "Speed": "Slow"},
			},
		},
	})
	if err != nil {
		t.Fatalf("compare_table failed: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}
	if !strings.Contains(s, "|Item|") && !strings.Contains(s, "| Item |") {
		t.Errorf("missing header row in markdown table: %s", s)
	}
	if !strings.Contains(s, "|---|") && !strings.Contains(s, "| --- |") {
		t.Errorf("missing separator row in markdown table: %s", s)
	}
	if !strings.Contains(s, "Product A") || !strings.Contains(s, "Product B") {
		t.Errorf("missing item names in table: %s", s)
	}
	if !strings.Contains(s, "$10") || !strings.Contains(s, "$20") {
		t.Errorf("missing data values in table: %s", s)
	}
}

func TestRunCompareTableMissingDimensionValue(t *testing.T) {
	res, err := runCompareTable(map[string]any{
		"dimensions": []any{"Price", "Rating"},
		"items": []any{
			map[string]any{
				"name": "Product A",
				"data": map[string]any{"Price": "$10"},
			},
		},
	})
	if err != nil {
		t.Fatalf("compare_table failed: %v", err)
	}
	s, _ := res.(string)
	if !strings.Contains(s, "\u2014") {
		t.Errorf("expected em-dash for missing dimension value, got: %s", s)
	}
}

func TestRunCompareTableXLSXMissingPath(t *testing.T) {
	_, err := runCompareTable(map[string]any{
		"dimensions":    []any{"Price"},
		"items":         []any{map[string]any{"name": "A", "data": map[string]any{"Price": "$10"}}},
		"output_format": "xlsx",
	})
	if err == nil {
		t.Fatal("expected error for xlsx without path")
	}
	if !strings.Contains(err.Error(), "path") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCompareTableXLSXBasic(t *testing.T) {
	dir := t.TempDir()
	xlsxPath := dir + "\\compare.xlsx"
	res, err := runCompareTable(map[string]any{
		"dimensions":    []any{"Price"},
		"items":         []any{map[string]any{"name": "A", "data": map[string]any{"Price": "$10"}}},
		"output_format": "xlsx",
		"path":          xlsxPath,
	})
	if err != nil {
		t.Fatalf("compare_table xlsx failed: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}
	if !strings.Contains(s, "written") {
		t.Errorf("unexpected result: %s", s)
	}
	if _, err := os.Stat(xlsxPath); os.IsNotExist(err) {
		t.Errorf("xlsx file not created at %s", xlsxPath)
	}
}

// --- buildMarkdownTable tests ---

func TestBuildMarkdownTable(t *testing.T) {
	rows := [][]string{
		{"Item", "Price", "Speed"},
		{"A", "$10", "Fast"},
		{"B", "$20", "Slow"},
	}
	result := buildMarkdownTable(rows)
	if !strings.Contains(result, "|Item|") && !strings.Contains(result, "| Item |") {
		t.Errorf("missing header: %s", result)
	}
	if !strings.Contains(result, "|---|") && !strings.Contains(result, "| --- |") {
		t.Errorf("missing separator: %s", result)
	}
	if !strings.Contains(result, "| A |") {
		t.Errorf("missing data row: %s", result)
	}
}

func TestBuildMarkdownTableEmpty(t *testing.T) {
	result := buildMarkdownTable(nil)
	if result != "" {
		t.Errorf("expected empty string for nil rows, got %q", result)
	}
}

func TestBuildMarkdownTablePipeEscaping(t *testing.T) {
	rows := [][]string{
		{"Header"},
		{"value|with|pipes"},
	}
	result := buildMarkdownTable(rows)
	if !strings.Contains(result, `value\|with\|pipes`) {
		t.Errorf("pipes not escaped: %s", result)
	}
}

// --- toCellString tests ---

func TestToCellString(t *testing.T) {
	cases := []struct {
		input any
		want  string
	}{
		{nil, ""},
		{"hello", "hello"},
		{float64(42), "42"},
		{float64(3.14), "3.14"},
		{true, "true"},
		{false, "false"},
	}
	for _, tc := range cases {
		got := toCellString(tc.input)
		if got != tc.want {
			t.Errorf("toCellString(%v) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --- Arg helper tests ---

func TestArgString(t *testing.T) {
	val, err := argString(map[string]any{"key": "value"}, "key")
	if err != nil || val != "value" {
		t.Errorf("got %q, %v; want 'value', nil", val, err)
	}
}

func TestArgStringMissing(t *testing.T) {
	_, err := argString(map[string]any{}, "key")
	if err == nil {
		t.Error("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "missing required argument") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestArgStringWrongType(t *testing.T) {
	_, err := argString(map[string]any{"key": 42}, "key")
	if err == nil {
		t.Error("expected error for wrong type")
	}
	if !strings.Contains(err.Error(), "must be a string") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestArgStringDefault(t *testing.T) {
	val := argStringDefault(map[string]any{}, "key", "fallback")
	if val != "fallback" {
		t.Errorf("got %q, want 'fallback'", val)
	}
	val = argStringDefault(map[string]any{"key": "actual"}, "key", "fallback")
	if val != "actual" {
		t.Errorf("got %q, want 'actual'", val)
	}
}

func TestArgIntDefault(t *testing.T) {
	val := argIntDefault(map[string]any{}, "key", 99)
	if val != 99 {
		t.Errorf("got %d, want 99", val)
	}
	val = argIntDefault(map[string]any{"key": float64(7)}, "key", 99)
	if val != 7 {
		t.Errorf("got %d, want 7", val)
	}
}

func TestArgStringSlice(t *testing.T) {
	val, err := argStringSlice(map[string]any{"key": []any{"a", "b", "c"}}, "key")
	if err != nil {
		t.Fatalf("argStringSlice failed: %v", err)
	}
	if len(val) != 3 || val[0] != "a" || val[1] != "b" || val[2] != "c" {
		t.Errorf("got %v, want [a b c]", val)
	}
}

func TestArgStringSliceMissing(t *testing.T) {
	_, err := argStringSlice(map[string]any{}, "key")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestArgStringSliceWrongType(t *testing.T) {
	_, err := argStringSlice(map[string]any{"key": "not-array"}, "key")
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

// --- MCP protocol end-to-end tests ---

func TestMCPInitialize(t *testing.T) {
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "initialize", nil, 1)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize missing result: %v", resp)
	}
	if pv, _ := result["protocolVersion"].(string); pv != protocolVersion {
		t.Errorf("protocolVersion = %q, want %q", pv, protocolVersion)
	}
	serverInfo, _ := result["serverInfo"].(map[string]any)
	if name, _ := serverInfo["name"].(string); name != "reasonix-plugin-search" {
		t.Errorf("serverInfo.name = %q, want 'reasonix-plugin-search'", name)
	}

	wIn.Close()
	<-errCh
}

func TestMCPToolsList(t *testing.T) {
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "tools/list", nil, 1)
	toolsRaw, _ := resp["result"].(map[string]any)["tools"]
	toolsArr, ok := toolsRaw.([]any)
	if !ok || len(toolsArr) != 3 {
		t.Fatalf("tools/list expected 3 tools, got %v", toolsArr)
	}

	names := make([]string, 0, len(toolsArr))
	for _, tool := range toolsArr {
		if m, ok := tool.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	expected := []string{"web_search", "web_extract", "compare_table"}
	for _, e := range expected {
		if !containsStr(names, e) {
			t.Errorf("missing tool %q in tools/list response", e)
		}
	}

	wIn.Close()
	<-errCh
}

func TestMCPToolsCallUnknownTool(t *testing.T) {
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "tools/call", map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]any{},
	}, 1)

	rpcErr, _ := resp["error"].(map[string]any)
	if rpcErr == nil {
		t.Fatalf("expected error for unknown tool, got: %v", resp)
	}
	code, _ := rpcErr["code"].(float64)
	if int(code) != codeInvalidParams {
		t.Errorf("error code = %d, want %d", int(code), codeInvalidParams)
	}
	msg, _ := rpcErr["message"].(string)
	if !strings.Contains(msg, "unknown tool") {
		t.Errorf("error message = %q, want 'unknown tool'", msg)
	}

	wIn.Close()
	<-errCh
}

func TestMCPMethodNotFound(t *testing.T) {
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "nonexistent/method", nil, 1)

	rpcErr, _ := resp["error"].(map[string]any)
	if rpcErr == nil {
		t.Fatalf("expected error for unknown method, got: %v", resp)
	}
	code, _ := rpcErr["code"].(float64)
	if int(code) != codeMethodNotFound {
		t.Errorf("error code = %d, want %d", int(code), codeMethodNotFound)
	}
	msg, _ := rpcErr["message"].(string)
	if !strings.Contains(msg, "method not found") {
		t.Errorf("error message = %q, want 'method not found'", msg)
	}

	wIn.Close()
	<-errCh
}

func TestMCPToolsCallWebSearchMissingKey(t *testing.T) {
	t.Setenv("SEARCH_API_KEY", "")

	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "tools/call", map[string]any{
		"name":      "web_search",
		"arguments": map[string]any{"query": "test"},
	}, 1)

	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("tools/call web_search missing result: %v", resp)
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("expected isError=true for missing API key")
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("missing content in result: %v", result)
	}
	textEntry, _ := content[0].(map[string]any)
	text, _ := textEntry["text"].(string)
	if !strings.Contains(text, "SEARCH_API_KEY") {
		t.Errorf("error text should mention SEARCH_API_KEY, got: %s", text)
	}

	wIn.Close()
	<-errCh
}

func TestMCPToolsCallCompareTable(t *testing.T) {
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "tools/call", map[string]any{
		"name": "compare_table",
		"arguments": map[string]any{
			"dimensions": []any{"Price"},
			"items": []any{
				map[string]any{
					"name": "A",
					"data": map[string]any{"Price": "$10"},
				},
			},
		},
	}, 1)

	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("tools/call compare_table missing result: %v", resp)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("missing content in result: %v", result)
	}
	textEntry, _ := content[0].(map[string]any)
	text, _ := textEntry["text"].(string)
	if !strings.Contains(text, "|Item|") && !strings.Contains(text, "| Item |") {
		t.Errorf("unexpected markdown table result: %s", text)
	}

	wIn.Close()
	<-errCh
}

// --- JSON-RPC message parsing tests ---

func TestHandleLineEmpty(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	bw := bufio.NewWriter(w)
	if err := handleLine([]byte("   \n"), bw); err != nil {
		t.Errorf("handleLine on empty input returned error: %v", err)
	}
	if err := handleLine([]byte(""), bw); err != nil {
		t.Errorf("handleLine on empty byte slice returned error: %v", err)
	}
	w.Close()
	r.Close()
}

func TestHandleLineUnparseable(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	bw := bufio.NewWriter(w)
	if err := handleLine([]byte("not json\n"), bw); err != nil {
		t.Errorf("handleLine on unparseable input returned error: %v", err)
	}
	w.Close()
	r.Close()
}

func TestHandleLineNotification(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	bw := bufio.NewWriter(w)
	if err := handleLine([]byte(`{"jsonrpc":"2.0","method":"initialized"}`+"\n"), bw); err != nil {
		t.Errorf("handleLine on notification returned error: %v", err)
	}
	w.Close()
	r.Close()
}

func TestTextResult(t *testing.T) {
	tr := textResult("hello", false)
	// textResult stores content as []map[string]any, which is not []any at runtime.
	contentRaw, ok := tr["content"]
	if !ok {
		t.Fatal("textResult missing content key")
	}
	// Use JSON round-trip to normalize the type.
	b, err := json.Marshal(contentRaw)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	var content []map[string]any
	if err := json.Unmarshal(b, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if len(content) != 1 {
		t.Fatalf("expected 1 content entry, got %d", len(content))
	}
	text, _ := content[0]["text"].(string)
	if text != "hello" {
		t.Errorf("text = %q, want 'hello'", text)
	}
	isError, _ := tr["isError"].(bool)
	if isError {
		t.Error("isError should be false")
	}
}

func TestTextResultError(t *testing.T) {
	tr := textResult("something failed", true)
	isError, _ := tr["isError"].(bool)
	if !isError {
		t.Error("isError should be true")
	}
}

// --- trimSpace tests ---

func TestTrimSpace(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"  hello  ", "hello"},
		{"\n\tfoo\r\n", "foo"},
		{"no-space", "no-space"},
		{"  ", ""},
	}
	for _, tc := range cases {
		got := string(trimSpace([]byte(tc.input)))
		if got != tc.want {
			t.Errorf("trimSpace(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --- helpers ---

func containsStr(arr []string, s string) bool {
	for _, x := range arr {
		if x == s {
			return true
		}
	}
	return false
}

func sendMCP(t *testing.T, wIn, rOut *os.File, method string, params any, id int) map[string]any {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if _, err := wIn.Write(b); err != nil {
		t.Fatal(err)
	}

	rOut.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer rOut.SetReadDeadline(time.Time{})
	br := bufio.NewReader(rOut)
	line, err := br.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nraw: %s", err, line)
	}
	return resp
}

package main

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// writeFile is a small wrapper for tests that need to drop a file on disk.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// osPipe wraps os.Pipe so the main test file can stay platform-agnostic.
func osPipe() (*os.File, *os.File, error) { return os.Pipe() }

// sendMCP writes one JSON-RPC request line and reads one response line back
// within a generous timeout. Mirrors the sheet plugin's serve_test.go helper.
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

// jsonField walks dotted path keys into a map[string]any; returns nil if any
// step is missing or the wrong type.
func jsonField(m map[string]any, path string) any {
	cur := m
	for _, key := range splitDots(path) {
		v, ok := cur[key]
		if !ok {
			return nil
		}
		next, ok := v.(map[string]any)
		if !ok {
			return v
		}
		cur = next
	}
	return cur
}

func splitDots(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

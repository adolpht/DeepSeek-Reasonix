package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// TestMCPProtocolEndToEnd drives the real serve() loop over a pipe: send
// initialize → tools/list → tools/call(read_sheet) and verify each response.
// This is the closest unit-level proxy to "Rexion spawns the plugin and
// calls a tool", covering the full JSON-RPC framing + dispatch path.
func TestMCPProtocolEndToEnd(t *testing.T) {
	// Build a small xlsx to query.
	xlsxPath := filepath.Join(t.TempDir(), "data.xlsx")
	f := excelize.NewFile()
	_ = f.SetCellValue("Sheet1", "A1", "city")
	_ = f.SetCellValue("Sheet1", "B1", "pop")
	_ = f.SetCellValue("Sheet1", "A2", "北京")
	_ = f.SetCellValue("Sheet1", "B2", 2100)
	_ = f.SetCellValue("Sheet1", "A3", "上海")
	_ = f.SetCellValue("Sheet1", "B3", 2400)
	if err := f.SaveAs(xlsxPath); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// stdin pipe: we write requests; the server reads them.
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// stdout pipe: the server writes responses; we read them.
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	// Run serve() in a goroutine; it returns when stdin closes.
	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	// Helper to send a JSON-RPC request and read one response line back.
	send := func(method string, params any, id int) map[string]any {
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

		// Read one response line with a timeout so a hung server fails fast.
		rOut.SetReadDeadline(time.Now().Add(5 * time.Second))
		defer rOut.SetReadDeadline(time.Time{})
		br := bufio.NewReader(rOut)
		line, err := br.ReadBytes('\n')
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		var resp map[string]any
		if err := json.Unmarshal(line, &resp); err != nil {
			t.Fatalf("unmarshal response %q: %v", string(line), err)
		}
		return resp
	}

	// 1. initialize
	resp := send("initialize", map[string]any{}, 1)
	if resp["error"] != nil {
		t.Fatalf("initialize error: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "Rexion-plugin-sheet" {
		t.Errorf("serverInfo.name = %v", info["name"])
	}

	// 2. tools/list
	resp = send("tools/list", nil, 2)
	if resp["error"] != nil {
		t.Fatalf("tools/list error: %v", resp["error"])
	}
	tools := resp["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	// Verify tool names.
	names := map[string]bool{}
	for _, tt := range tools {
		names[tt.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"read_sheet", "write_sheet", "query_sheet", "chart_sheet"} {
		if !names[want] {
			t.Errorf("tool %q missing from tools/list", want)
		}
	}

	// 3. tools/call read_sheet
	resp = send("tools/call", map[string]any{
		"name": "read_sheet",
		"arguments": map[string]any{
			"path":   xlsxPath,
			"format": "markdown",
		},
	}, 3)
	if resp["error"] != nil {
		t.Fatalf("tools/call error: %v", resp["error"])
	}
	result = resp["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "北京") || !strings.Contains(text, "pop") {
		t.Errorf("read_sheet result missing data:\n%s", text)
	}

	// 4. tools/call on unknown tool → rpcError
	resp = send("tools/call", map[string]any{
		"name": "bogus",
	}, 4)
	if resp["error"] == nil {
		t.Errorf("expected error for unknown tool, got nil")
	}

	// Close stdin to end serve() and drain the goroutine.
	wIn.Close()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit after stdin closed")
	}
}

// ensure io.EOF is used (compile-time guard against unused import if the loop
// changes). Removed if not needed; kept minimal.
var _ = io.EOF

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- todo database tests ---

func TestAddTodo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REXION_HOME", dir)

	res, err := runAddTodo(map[string]any{
		"title":    "Test task",
		"priority": "high",
	})
	if err != nil {
		t.Fatalf("add_todo failed: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}
	if !strings.Contains(s, "Test task") || !strings.Contains(s, "high") {
		t.Errorf("unexpected result: %s", s)
	}
}

func TestAddTodoDefaultPriority(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REXION_HOME", dir)

	res, err := runAddTodo(map[string]any{
		"title": "Default priority task",
	})
	if err != nil {
		t.Fatalf("add_todo failed: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string, got %T", res)
	}
	if !strings.Contains(s, "medium") {
		t.Errorf("expected default priority 'medium', got: %s", s)
	}
}

func TestAddTodoInvalidPriority(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REXION_HOME", dir)

	_, err := runAddTodo(map[string]any{
		"title":    "Bad priority",
		"priority": "urgent",
	})
	if err == nil {
		t.Fatal("expected error for invalid priority")
	}
	if !strings.Contains(err.Error(), "invalid priority") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAddTodoMissingTitle(t *testing.T) {
	_, err := runAddTodo(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing title")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestListTodoEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REXION_HOME", dir)

	res, err := runListTodo(map[string]any{})
	if err != nil {
		t.Fatalf("list_todo failed: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string, got %T", res)
	}
	if !strings.Contains(s, "No todo items found") {
		t.Errorf("expected empty message, got: %s", s)
	}
}

func TestAddAndListTodo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REXION_HOME", dir)

	// Add a todo.
	_, err := runAddTodo(map[string]any{
		"title":       "Buy groceries",
		"description": "Milk, eggs, bread",
		"due_date":    "2026-07-05",
		"priority":    "high",
	})
	if err != nil {
		t.Fatalf("add_todo failed: %v", err)
	}

	// List todos.
	res, err := runListTodo(map[string]any{})
	if err != nil {
		t.Fatalf("list_todo failed: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string, got %T", res)
	}
	if !strings.Contains(s, "Buy groceries") {
		t.Errorf("expected 'Buy groceries' in result, got: %s", s)
	}
	if !strings.Contains(s, "high") {
		t.Errorf("expected priority 'high' in result, got: %s", s)
	}
}

func TestListTodoFilterByStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REXION_HOME", dir)

	// Add a todo.
	addRes, err := runAddTodo(map[string]any{
		"title": "Pending task",
	})
	if err != nil {
		t.Fatalf("add_todo failed: %v", err)
	}

	// Extract the ID from the add result.
	addStr, _ := addRes.(string)
	var todoID string
	if _, err := fmt.Sscanf(addStr, "Todo %q added (id: %s", new(string), &todoID); true {
		// Try a simpler extraction.
		if idx := strings.Index(addStr, "id: "); idx >= 0 {
			rest := addStr[idx+4:]
			if end := strings.Index(rest, ","); end >= 0 {
				todoID = rest[:end]
			} else if end := strings.Index(rest, ")"); end >= 0 {
				todoID = rest[:end]
			}
		}
	}

	// Filter by "done" status — should find nothing.
	res, err := runListTodo(map[string]any{"status": "done"})
	if err != nil {
		t.Fatalf("list_todo failed: %v", err)
	}
	s, _ := res.(string)
	if !strings.Contains(s, "No todo items found") {
		t.Errorf("expected no done items, got: %s", s)
	}

	// Filter by "pending" — should find the task.
	res, err = runListTodo(map[string]any{"status": "pending"})
	if err != nil {
		t.Fatalf("list_todo failed: %v", err)
	}
	s, _ = res.(string)
	if !strings.Contains(s, "Pending task") {
		t.Errorf("expected 'Pending task', got: %s", s)
	}

	// Use the extracted ID for update if available.
	if todoID != "" {
		// Mark as done.
		_, err = runUpdateTodo(map[string]any{
			"id":     todoID,
			"status": "done",
		})
		if err != nil {
			t.Fatalf("update_todo failed: %v", err)
		}

		// Now "done" filter should find it.
		res, err = runListTodo(map[string]any{"status": "done"})
		if err != nil {
			t.Fatalf("list_todo failed: %v", err)
		}
		s, _ = res.(string)
		if !strings.Contains(s, "Pending task") {
			t.Errorf("expected 'Pending task' in done items, got: %s", s)
		}
	}
}

func TestUpdateTodoNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REXION_HOME", dir)

	_, err := runUpdateTodo(map[string]any{
		"id":     "nonexistent_id",
		"status": "done",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent todo")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUpdateTodoInvalidStatus(t *testing.T) {
	_, err := runUpdateTodo(map[string]any{
		"id":     "some_id",
		"status": "in_progress",
	})
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUpdateTodoNoFieldsProvided(t *testing.T) {
	_, err := runUpdateTodo(map[string]any{
		"id": "some_id",
	})
	if err == nil {
		t.Fatal("expected error when no update fields provided")
	}
	if !strings.Contains(err.Error(), "at least one") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUpdateTodoTitle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REXION_HOME", dir)

	// Add a todo and extract its ID.
	addRes, err := runAddTodo(map[string]any{
		"title": "Original title",
	})
	if err != nil {
		t.Fatalf("add_todo failed: %v", err)
	}
	todoID := extractTodoID(addRes.(string))
	if todoID == "" {
		t.Fatal("failed to extract todo ID")
	}

	// Update the title.
	_, err = runUpdateTodo(map[string]any{
		"id":    todoID,
		"title": "Updated title",
	})
	if err != nil {
		t.Fatalf("update_todo failed: %v", err)
	}

	// Verify the update.
	res, err := runListTodo(map[string]any{})
	if err != nil {
		t.Fatalf("list_todo failed: %v", err)
	}
	s, _ := res.(string)
	if !strings.Contains(s, "Updated title") {
		t.Errorf("expected 'Updated title', got: %s", s)
	}
}

func TestAddTodoWithDueDate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REXION_HOME", dir)

	res, err := runAddTodo(map[string]any{
		"title":    "Deadline task",
		"due_date": "2026-07-15",
	})
	if err != nil {
		t.Fatalf("add_todo failed: %v", err)
	}
	s, _ := res.(string)
	if !strings.Contains(s, "Deadline task") {
		t.Errorf("unexpected result: %s", s)
	}
}

// --- calendar parser tests ---

func TestParseIcalBuddyOutput(t *testing.T) {
	input := `Team Standup
    at 9:00 AM - 9:30 AM
    location: Room 101

Sprint Review
    at 3:00 PM - 4:00 PM`

	events := parseIcalBuddyOutput(input)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Title != "Team Standup" {
		t.Errorf("first event title = %q, want 'Team Standup'", events[0].Title)
	}
	if events[0].Location != "Room 101" {
		t.Errorf("first event location = %q, want 'Room 101'", events[0].Location)
	}
	if events[1].Title != "Sprint Review" {
		t.Errorf("second event title = %q, want 'Sprint Review'", events[1].Title)
	}
}

func TestParseIcalBuddyOutputEmpty(t *testing.T) {
	events := parseIcalBuddyOutput("")
	if len(events) != 0 {
		t.Errorf("expected 0 events for empty input, got %d", len(events))
	}
}

func TestParseAppleScriptOutput(t *testing.T) {
	input := `Team Meeting|2026-07-01 10:00:00|2026-07-01 11:00:00|Room 5|Discuss project
Lunch|2026-07-01 12:00:00|2026-07-01 13:00:00||`

	events := parseAppleScriptOutput(input)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Title != "Team Meeting" {
		t.Errorf("first event title = %q", events[0].Title)
	}
	if events[0].Location != "Room 5" {
		t.Errorf("first event location = %q", events[0].Location)
	}
	if events[0].Description != "Discuss project" {
		t.Errorf("first event description = %q", events[0].Description)
	}
	if events[1].Title != "Lunch" {
		t.Errorf("second event title = %q", events[1].Title)
	}
}

func TestParseAppleScriptOutputEmpty(t *testing.T) {
	events := parseAppleScriptOutput("")
	if len(events) != 0 {
		t.Errorf("expected 0 events for empty input, got %d", len(events))
	}
}

// --- MCP protocol end-to-end test ---

func TestMCPProtocolEndToEnd(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REXION_HOME", dir)

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

	// initialize
	resp := sendMCP(t, wIn, rOut, "initialize", nil, 1)
	if got := jsonField(resp, "result"); got == nil {
		t.Fatalf("initialize missing result: %v", resp)
	}

	// tools/list
	resp = sendMCP(t, wIn, rOut, "tools/list", nil, 2)
	toolsRaw, _ := resp["result"].(map[string]any)["tools"]
	toolsArr, ok := toolsRaw.([]any)
	if !ok || len(toolsArr) != 5 {
		t.Fatalf("tools/list expected 5 tools, got %v", toolsArr)
	}

	// tools/call add_todo
	resp = sendMCP(t, wIn, rOut, "tools/call", map[string]any{
		"name":      "add_todo",
		"arguments": map[string]any{"title": "E2E test task", "priority": "low"},
	}, 3)
	if got := resp["result"]; got == nil {
		t.Fatalf("tools/call add_todo missing result: %v", resp)
	}

	// tools/call list_todo
	resp = sendMCP(t, wIn, rOut, "tools/call", map[string]any{
		"name":      "list_todo",
		"arguments": map[string]any{},
	}, 4)
	if got := resp["result"]; got == nil {
		t.Fatalf("tools/call list_todo missing result: %v", resp)
	}

	wIn.Close()
	<-errCh
}

// --- tool registration test ---

func TestToolList(t *testing.T) {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.name)
	}
	expected := []string{"read_event", "add_event", "list_todo", "add_todo", "update_todo"}
	for _, e := range expected {
		if !containsStr(names, e) {
			t.Errorf("missing tool %q in %v", e, names)
		}
	}
}

// --- helpers ---

func extractTodoID(s string) string {
	// Format: Todo "title" added (id: todo_xxx, priority: xxx, status: pending).
	idx := strings.Index(s, "id: ")
	if idx < 0 {
		return ""
	}
	rest := s[idx+4:]
	end := strings.Index(rest, ",")
	if end < 0 {
		end = strings.Index(rest, ")")
	}
	if end < 0 {
		return rest
	}
	return rest[:end]
}

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

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// DBPathForTest returns the database path for testing purposes.
func TestDBPathWithEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REXION_HOME", dir)
	got := dbPath()
	want := filepath.Join(dir, "data.db")
	if got != want {
		t.Errorf("dbPath() = %q, want %q", got, want)
	}
}

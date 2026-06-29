package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// TestParseCell verifies Excel cell-name parsing (e.g. "B3" → col=2, row=3).
func TestParseCell(t *testing.T) {
	cases := []struct {
		in        string
		col, row  int
		ok        bool
	}{
		{"A1", 1, 1, true},
		{"B3", 2, 3, true},
		{"Z10", 26, 10, true},
		{"AA1", 27, 1, true},
		{"AB12", 28, 12, true},
		{"", 0, 0, false},
		{"A", 0, 0, false},
		{"1A", 0, 0, false},
		{"A1B", 0, 0, false},
	}
	for _, c := range cases {
		col, row, ok := parseCell(c.in)
		if col != c.col || row != c.row || ok != c.ok {
			t.Errorf("parseCell(%q) = (%d,%d,%v), want (%d,%d,%v)",
				c.in, col, row, ok, c.col, c.row, c.ok)
		}
	}
}

// TestFilterByRange checks rectangular extraction from a row matrix.
func TestFilterByRange(t *testing.T) {
	rows := [][]string{
		{"a1", "b1", "c1"},
		{"a2", "b2", "c2"},
		{"a3", "b3", "c3"},
	}
	got := filterByRange(rows, "A1:C2")
	if len(got) != 2 || got[0][0] != "a1" || got[1][2] != "c2" {
		t.Errorf("filterByRange A1:C2 = %v", got)
	}
	got = filterByRange(rows, "B2:C3")
	if len(got) != 2 || got[0][0] != "b2" || got[1][1] != "c3" {
		t.Errorf("filterByRange B2:C3 = %v", got)
	}
}

// TestRowsToMarkdown checks Markdown table rendering and pipe escaping.
func TestRowsToMarkdown(t *testing.T) {
	rows := [][]string{
		{"name", "note"},
		{"a", "x|y"},
	}
	out := rowsToMarkdown(rows)
	want := "| name | note |\n|---|---|\n| a | x\\|y |\n"
	if out != want {
		t.Errorf("rowsToMarkdown = %q, want %q", out, want)
	}
}

// TestParseMarkdownTable checks Markdown table parsing (inverse of rendering).
func TestParseMarkdownTable(t *testing.T) {
	in := "| name | value |\n|---|---|\n| a | 1 |\n| b | 2 |\n"
	rows, err := parseMarkdownTable(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0][0] != "name" || rows[2][1] != "2" {
		t.Errorf("parseMarkdownTable = %v", rows)
	}
}

// TestToCellString verifies JSON-derived value → string conversion.
func TestToCellString(t *testing.T) {
	if got := toCellString(float64(42)); got != "42" {
		t.Errorf("toCellString(42) = %q", got)
	}
	if got := toCellString(float64(3.14)); got != "3.14" {
		t.Errorf("toCellString(3.14) = %q", got)
	}
	if got := toCellString("hi"); got != "hi" {
		t.Errorf("toCellString(hi) = %q", got)
	}
	if got := toCellString(nil); got != "" {
		t.Errorf("toCellString(nil) = %q", got)
	}
}

// --- integration: real xlsx round-trip via excelize ---

// newTestXLSX writes a small sheet to a temp file and returns its path.
func newTestXLSX(t *testing.T) string {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Sheet1"
	_ = f.SetCellValue(sheet, "A1", "region")
	_ = f.SetCellValue(sheet, "B1", "amount")
	_ = f.SetCellValue(sheet, "A2", "华东")
	_ = f.SetCellValue(sheet, "B2", 1000)
	_ = f.SetCellValue(sheet, "A3", "华北")
	_ = f.SetCellValue(sheet, "B3", 2500)
	_ = f.SetCellValue(sheet, "A4", "华东")
	_ = f.SetCellValue(sheet, "B4", 3000)

	dir := t.TempDir()
	path := filepath.Join(dir, "test.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("save xlsx: %v", err)
	}
	return path
}

// TestReadSheetXLSX verifies end-to-end read_sheet on a real xlsx file.
func TestReadSheetXLSX(t *testing.T) {
	path := newTestXLSX(t)
	got, err := runReadSheet(map[string]any{
		"path": path,
	})
	if err != nil {
		t.Fatalf("runReadSheet: %v", err)
	}
	out := got.(string)
	// header + 3 data rows, region column present
	if !contains(out, "region") || !contains(out, "华东") || !contains(out, "2500") {
		t.Errorf("read_sheet output missing expected data:\n%s", out)
	}
}

// TestQuerySheetGroupBy verifies query_sheet aggregation with GROUP BY.
func TestQuerySheetGroupBy(t *testing.T) {
	path := newTestXLSX(t)
	got, err := runQuerySheet(map[string]any{
		"path":  path,
		"select": []any{"region", "SUM(amount) as total"},
		"group_by": []any{"region"},
		"order_by": "total DESC",
	})
	if err != nil {
		t.Fatalf("runQuerySheet: %v", err)
	}
	out := got.(string)
	// Two groups: 华东(4000) and 华北(2500). DESC ⇒ 华东 first.
	if !contains(out, "华东") || !contains(out, "华北") {
		t.Errorf("query output missing groups:\n%s", out)
	}
	if !contains(out, "4000") || !contains(out, "2500") {
		t.Errorf("query output missing sums:\n%s", out)
	}
}

// TestQuerySheetWhere verifies query_sheet WHERE filtering with AND.
func TestQuerySheetWhere(t *testing.T) {
	path := newTestXLSX(t)
	got, err := runQuerySheet(map[string]any{
		"path":  path,
		"select": []any{"region", "amount"},
		"where": "region='华东' AND amount>2000",
	})
	if err != nil {
		t.Fatalf("runQuerySheet: %v", err)
	}
	out := got.(string)
	// Only one row matches: 华东 3000
	if !contains(out, "3000") || contains(out, "1000") {
		t.Errorf("where filter wrong:\n%s", out)
	}
}

// TestWriteSheetAppend verifies write_sheet append mode round-trips through read.
func TestWriteSheetAppend(t *testing.T) {
	path := newTestXLSX(t)
	// Append one row.
	_, err := runWriteSheet(map[string]any{
		"path":  path,
		"data":  []any{[]any{"华南", 9999}},
		"mode":  "append",
	})
	if err != nil {
		t.Fatalf("runWriteSheet append: %v", err)
	}
	// Read back: should have 4 data rows now.
	got, err := runReadSheet(map[string]any{"path": path})
	if err != nil {
		t.Fatalf("runReadSheet: %v", err)
	}
	out := got.(string)
	if !contains(out, "华南") || !contains(out, "9999") {
		t.Errorf("appended row missing:\n%s", out)
	}
}

// TestCSVRoundTrip verifies csv read/write end-to-end.
func TestCSVRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	// Write a CSV.
	_, err := runWriteSheet(map[string]any{
		"path": path,
		"data": []any{
			[]any{"name", "age"},
			[]any{"alice", 30},
			[]any{"bob", 25},
		},
	})
	if err != nil {
		t.Fatalf("write csv: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("csv not created: %v", err)
	}
	// Read it back.
	got, err := runReadSheet(map[string]any{"path": path, "format": "markdown"})
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	out := got.(string)
	if !contains(out, "alice") || !contains(out, "bob") {
		t.Errorf("csv round-trip missing data:\n%s", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

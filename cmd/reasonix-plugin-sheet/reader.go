package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// readSheetTool reads a region of an xlsx/csv file and returns rows as Markdown
// or JSON. Read-only: declared via readOnlyHint so the agent batches it in
// parallel and the permission layer auto-allows it.
//
// Large files are protected by max_rows (default 1000) to keep token cost
// bounded; callers wanting aggregation should use query_sheet instead.
var readSheetTool = toolDef{
	name: "read_sheet",
	description: "Read rows from an xlsx/csv file. Returns a Markdown table by default. " +
		"Use 'range' to bound the region and 'max_rows' to cap output (default 1000). " +
		"For large files prefer query_sheet which aggregates server-side.",
	readOnly: true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":       map[string]any{"type": "string", "description": "Absolute path to the xlsx or csv file"},
			"sheet":      map[string]any{"type": "string", "description": "Sheet name (xlsx only); default first sheet"},
			"range":      map[string]any{"type": "string", "description": "Excel range like 'A1:Z100'; empty = auto-detect used area"},
			"header_row": map[string]any{"type": "integer", "description": "1-based header row; 0 = no header (default 1)"},
			"max_rows":   map[string]any{"type": "integer", "description": "Row cap to bound tokens (default 1000)"},
			"format":     map[string]any{"type": "string", "enum": []string{"markdown", "json", "csv"}, "description": "Output format (default markdown)"},
		},
		"required": []string{"path"},
	},
	run: runReadSheet,
}

func runReadSheet(args map[string]any) (any, error) {
	path, err := argString(args, "path")
	if err != nil {
		return nil, err
	}
	sheet := argStringDefault(args, "sheet", "")
	rng := argStringDefault(args, "range", "")
	headerRow := argIntDefault(args, "header_row", 1)
	maxRows := argIntDefault(args, "max_rows", 1000)
	if maxRows <= 0 {
		maxRows = 1000
	}
	format := argStringDefault(args, "format", "markdown")

	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	rows, truncated, totalRows, err := readRows(path, sheet, rng, headerRow, maxRows)
	if err != nil {
		return nil, err
	}

	var out string
	switch format {
	case "json":
		out = rowsToJSON(rows)
	case "csv":
		out = rowsToCSV(rows)
	default:
		out = rowsToMarkdown(rows)
	}

	summary := fmt.Sprintf("(rows returned: %d, total in range: %d, truncated: %v)\n\n", len(rows), totalRows, truncated)
	return summary + out, nil
}

// readRows normalizes xlsx and csv into [][]string, applying range/max_rows.
func readRows(path, sheet, rng string, headerRow, maxRows int) (rows [][]string, truncated bool, total int, err error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".csv":
		return readCSV(path, maxRows)
	case ".xlsx", ".xlsm":
		return readXLSX(path, sheet, rng, headerRow, maxRows)
	default:
		return nil, false, 0, fmt.Errorf("unsupported file extension %q (want .xlsx/.xlsm/.csv)", ext)
	}
}

func readCSV(path string, maxRows int) ([][]string, bool, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, 0, fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.FieldsPerRecord = -1 // allow variable column counts

	var rows [][]string
	total := 0
	for {
		rec, err := r.Read()
		if err != nil {
			break // EOF
		}
		total++
		if len(rows) < maxRows {
			rows = append(rows, rec)
		}
	}
	return rows, total > maxRows, total, nil
}

func readXLSX(path, sheet, rng string, headerRow, maxRows int) ([][]string, bool, int, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, false, 0, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	if sheet == "" {
		sheet = f.GetSheetName(0)
	}
	idx, err := f.GetSheetIndex(sheet)
	if err != nil || idx < 0 {
		return nil, false, 0, fmt.Errorf("sheet %q not found: %v", sheet, err)
	}

	// GetRows returns only used cells; if a range is given, we filter further.
	// headerRow is applied to the raw rows below; range filtering respects the
	// absolute cell coordinates (so "A1:C10" works regardless of header_row).
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, false, 0, fmt.Errorf("read rows: %w", err)
	}
	allRows := rows
	if rng != "" {
		allRows = filterByRange(rows, rng)
	}

	// Apply header_row offset: rows before header_row are dropped from output
	// but counted in total.
	start := 0
	if headerRow > 0 {
		start = headerRow - 1
	}
	if start > len(allRows) {
		start = len(allRows)
	}
	allRows = allRows[start:]

	total := len(allRows)
	truncated := total > maxRows
	if total > maxRows {
		allRows = allRows[:maxRows]
	}
	return allRows, truncated, total, nil
}

// filterByRange extracts the sub-rectangle described by an Excel range like
// "A1:C10". It is intentionally simple: parse start/end cells via the column
// letter convention.
func filterByRange(rows [][]string, rng string) [][]string {
	rng = strings.TrimSpace(rng)
	if rng == "" {
		return rows
	}
	parts := strings.SplitN(rng, ":", 2)
	if len(parts) != 2 {
		return rows // malformed; fall back to all
	}
	startCol, startRow, ok1 := parseCell(parts[0])
	endCol, endRow, ok2 := parseCell(parts[1])
	if !ok1 || !ok2 {
		return rows
	}
	if startCol < 1 {
		startCol = 1
	}
	if startRow < 1 {
		startRow = 1
	}
	var out [][]string
	for r := startRow; r <= endRow && r <= len(rows); r++ {
		src := rows[r-1]
		if endCol > len(src) {
			endCol = len(src)
		}
		if startCol > endCol {
			continue
		}
		out = append(out, src[startCol-1:endCol])
	}
	return out
}

// parseCell turns "B3" into (col=2, row=3).
func parseCell(cell string) (col, row int, ok bool) {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return 0, 0, false
	}
	i := 0
	for i < len(cell) && ((cell[i] >= 'A' && cell[i] <= 'Z') || (cell[i] >= 'a' && cell[i] <= 'z')) {
		col = col*26 + int(toUpperByte(cell[i])-'A'+1)
		i++
	}
	for i < len(cell) && cell[i] >= '0' && cell[i] <= '9' {
		row = row*10 + int(cell[i]-'0')
		i++
	}
	if col == 0 || row == 0 || i != len(cell) {
		return 0, 0, false
	}
	return col, row, true
}

func toUpperByte(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 32
	}
	return b
}

// --- output formatters ---

func rowsToMarkdown(rows [][]string) string {
	if len(rows) == 0 {
		return "(empty)"
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	var b strings.Builder
	// header
	b.WriteString("| ")
	for i := 0; i < cols; i++ {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(cellOrBlank(rows, 0, i))
	}
	b.WriteString(" |\n|")
	for i := 0; i < cols; i++ {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	for r := 1; r < len(rows); r++ {
		b.WriteString("| ")
		for i := 0; i < cols; i++ {
			if i > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(cellOrBlank(rows, r, i))
		}
		b.WriteString(" |\n")
	}
	return b.String()
}

func rowsToJSON(rows [][]string) string {
	if len(rows) == 0 {
		return "[]"
	}
	header := rows[0]
	var b strings.Builder
	b.WriteByte('[')
	for r := 1; r < len(rows); r++ {
		if r > 1 {
			b.WriteByte(',')
		}
		b.WriteByte('{')
		for i, h := range header {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%q:%q", h, cellOrBlank(rows, r, i))
		}
		b.WriteByte('}')
	}
	b.WriteByte(']')
	return b.String()
}

func rowsToCSV(rows [][]string) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.WriteAll(rows)
	return b.String()
}

func cellOrBlank(rows [][]string, r, c int) string {
	if r < 0 || r >= len(rows) {
		return ""
	}
	if c < 0 || c >= len(rows[r]) {
		return ""
	}
	return strings.ReplaceAll(rows[r][c], "|", "\\|")
}

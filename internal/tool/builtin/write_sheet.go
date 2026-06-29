package builtin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/tool"

	"github.com/xuri/excelize/v2"
)

func init() {
	tool.RegisterBuiltin(writeSheet{})
}

// writeSheet writes rows to an xlsx/csv file. roots, when non-empty, confines
// the target to the workspace (see confine); the zero value registered at init
// is unconfined and is overridden per run by ConfineWriters. workDir, when
// non-empty, is the directory a relative path resolves against (see resolveIn).
type writeSheet struct {
	roots   []string
	workDir string
}

func (writeSheet) Name() string { return "write_sheet" }

func (writeSheet) Description() string {
	return "Write rows to an xlsx/csv file. 'data' may be a 2D array or a Markdown table. mode=overwrite (default) replaces the sheet; mode=append adds after the last used row."
}

func (writeSheet) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Path to the xlsx or csv file"
			},
			"data": {
				"oneOf": [
					{"type": "array", "items": {"type": "array", "items": {"type": "string"}}},
					{"type": "string"}
				],
				"description": "2D array of rows or a Markdown table string"
			},
			"sheet": {
				"type": "string",
				"description": "Sheet name for xlsx files (default: Sheet1)"
			},
			"mode": {
				"type": "string",
				"enum": ["overwrite", "append"],
				"description": "Write mode: overwrite (default) or append"
			},
			"start_cell": {
				"type": "string",
				"description": "Starting cell for writing (default: A1)"
			}
		},
		"required": ["path", "data"]
	}`)
}

func (writeSheet) ReadOnly() bool { return false }

func (w writeSheet) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path      string          `json:"path"`
		Data      json.RawMessage `json:"data"`
		Sheet     string          `json:"sheet"`
		Mode      string          `json:"mode"`
		StartCell string          `json:"start_cell"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	p.Path = resolveIn(w.workDir, p.Path)
	if err := confine(w.roots, p.Path); err != nil {
		return "", err
	}

	if p.Sheet == "" {
		p.Sheet = "Sheet1"
	}
	if p.Mode == "" {
		p.Mode = "overwrite"
	}
	if p.StartCell == "" {
		p.StartCell = "A1"
	}

	rows, err := extractRows(p.Data)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("data is empty")
	}

	if dir := filepath.Dir(p.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	ext := strings.ToLower(filepath.Ext(p.Path))
	var written int
	switch ext {
	case ".csv":
		written, err = writeCSV(p.Path, rows, p.Mode)
	case ".xlsx", ".xlsm":
		written, err = writeXLSX(p.Path, p.Sheet, rows, p.Mode, p.StartCell)
	default:
		return "", fmt.Errorf("unsupported file extension %q (want .xlsx/.xlsm/.csv)", ext)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d rows to %s (mode=%s)", written, p.Path, p.Mode), nil
}

// extractRows accepts either a 2D array (from JSON) or a Markdown table string.
func extractRows(v json.RawMessage) ([][]string, error) {
	// Try as string first (Markdown table)
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return parseMarkdownTable(s)
	}

	// Try as 2D array
	var arr [][]any
	if err := json.Unmarshal(v, &arr); err != nil {
		return nil, fmt.Errorf("data must be a 2D array or Markdown table string")
	}

	rows := make([][]string, 0, len(arr))
	for _, row := range arr {
		cells := make([]string, 0, len(row))
		for _, c := range row {
			cells = append(cells, toCellString(c))
		}
		rows = append(rows, cells)
	}
	return rows, nil
}

// parseMarkdownTable turns a Markdown pipe-table into rows. Tolerant: skips the
// separator row (|---|---|) and blank lines.
func parseMarkdownTable(s string) ([][]string, error) {
	lines := strings.Split(s, "\n")
	var rows [][]string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || !strings.HasPrefix(ln, "|") {
			continue
		}
		// strip leading/trailing pipe
		inner := strings.TrimPrefix(ln, "|")
		inner = strings.TrimSuffix(inner, "|")
		cells := strings.Split(inner, "|")
		for i, c := range cells {
			cells[i] = strings.TrimSpace(strings.ReplaceAll(c, "\\|", "|"))
		}
		// skip separator row like ["---","---"]
		if isSeparator(cells) {
			continue
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no table rows found in Markdown input")
	}
	return rows, nil
}

func isSeparator(cells []string) bool {
	for _, c := range cells {
		c = strings.ReplaceAll(c, ":", "")
		c = strings.ReplaceAll(c, "-", "")
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func writeCSV(path string, rows [][]string, mode string) (int, error) {
	if mode == "append" {
		if _, err := os.Stat(path); err == nil {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return 0, fmt.Errorf("open for append: %w", err)
			}
			defer f.Close()
			w := csv.NewWriter(f)
			if err := w.WriteAll(rows); err != nil {
				return 0, err
			}
			return len(rows), nil
		}
		// fall through to create if file doesn't exist
	}
	f, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		return 0, err
	}
	return len(rows), nil
}

func writeXLSX(path, sheet string, rows [][]string, mode, startCell string) (int, error) {
	var f *excelize.File
	if mode == "append" {
		if _, err := os.Stat(path); err == nil {
			ex, err := excelize.OpenFile(path)
			if err != nil {
				return 0, fmt.Errorf("open for append: %w", err)
			}
			f = ex
			if idx, _ := f.GetSheetIndex(sheet); idx < 0 {
				_, _ = f.NewSheet(sheet)
			}
		}
	}
	if f == nil {
		f = excelize.NewFile()
		if sheet != "Sheet1" {
			f.SetSheetName("Sheet1", sheet)
		}
	}

	startCol, startRow, ok := parseCell(startCell)
	if !ok {
		startCol, startRow = 1, 1
	}

	// For append, find last used row and start after it.
	if mode == "append" {
		rowsInSheet, err := f.GetRows(sheet)
		if err == nil && len(rowsInSheet) > 0 {
			startRow = len(rowsInSheet) + 1
		}
	}

	// Write data.
	for r, row := range rows {
		for c, val := range row {
			cell, _ := excelize.CoordinatesToCellName(startCol+c, startRow+r)
			_ = f.SetCellValue(sheet, cell, val)
		}
	}

	// --- Apply styles ---
	applySheetStyles(f, sheet, rows, startCol, startRow)

	if err := f.SaveAs(path); err != nil {
		return 0, fmt.Errorf("save: %w", err)
	}
	return len(rows), nil
}

// applySheetStyles adds professional styling to the written sheet:
//   - Header row: bold font, light blue background (#D9E1F2), bottom border
//   - All cells: thin border on all four sides
//   - Column widths: auto-fit based on max content length
func applySheetStyles(f *excelize.File, sheet string, rows [][]string, startCol, startRow int) {
	if len(rows) == 0 {
		return
	}

	numCols := 0
	for _, row := range rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	if numCols == 0 {
		return
	}

	// Header style: bold + light blue fill + thin border.
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Color:  "000000",
			Family: "Calibri",
			Size:   11,
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"D9E1F2"},
			Pattern: 1,
		},
		Border: []excelize.Border{
			{Type: "left", Color: "B4C6E7", Style: 1},
			{Type: "top", Color: "B4C6E7", Style: 1},
			{Type: "bottom", Color: "B4C6E7", Style: 1},
			{Type: "right", Color: "B4C6E7", Style: 1},
		},
	})

	// Body style: thin border only.
	bodyStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Family: "Calibri",
			Size:   11,
		},
		Border: []excelize.Border{
			{Type: "left", Color: "D9D9D9", Style: 1},
			{Type: "top", Color: "D9D9D9", Style: 1},
			{Type: "bottom", Color: "D9D9D9", Style: 1},
			{Type: "right", Color: "D9D9D9", Style: 1},
		},
	})

	// Apply header style to the first row.
	for c := 0; c < numCols; c++ {
		cell, _ := excelize.CoordinatesToCellName(startCol+c, startRow)
		_ = f.SetCellStyle(sheet, cell, cell, headerStyle)
	}

	// Apply body style to data rows.
	for r := 1; r < len(rows); r++ {
		for c := 0; c < numCols; c++ {
			cell, _ := excelize.CoordinatesToCellName(startCol+c, startRow+r)
			_ = f.SetCellStyle(sheet, cell, cell, bodyStyle)
		}
	}

	// Auto-fit column widths based on max content length.
	for c := 0; c < numCols; c++ {
		maxLen := 0
		for _, row := range rows {
			if c < len(row) {
				cellLen := len(row[c])
				// CJK characters are roughly 2x wider.
				wideChars := 0
				for _, r := range row[c] {
					if r >= 0x4E00 && r <= 0x9FFF || // CJK Unified
						r >= 0x3000 && r <= 0x303F || // CJK Symbols
						r >= 0xFF01 && r <= 0xFF60 { // Fullwidth
						wideChars++
					}
				}
				cellLen += wideChars // double-width chars count extra
				if cellLen > maxLen {
					maxLen = cellLen
				}
			}
		}
		// Minimum width 8, max 50. Add padding.
		width := float64(maxLen) + 2
		if width < 8 {
			width = 8
		}
		if width > 50 {
			width = 50
		}
		colName, _ := excelize.ColumnNumberToName(startCol + c)
		_ = f.SetColWidth(sheet, colName, colName, width)
	}
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

func toCellString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		// JSON numbers: render ints without trailing .0
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", x)
	}
}

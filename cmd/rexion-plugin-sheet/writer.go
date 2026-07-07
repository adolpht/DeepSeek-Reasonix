package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// writeSheetTool writes rows to an xlsx/csv file. Overwrite replaces the sheet;
// append adds after the last used row. NOT read-only: it mutates files, so the
// permission layer will prompt unless the user has allow-listed it.
var writeSheetTool = toolDef{
	name: "write_sheet",
	description: "Write rows to an xlsx/csv file. 'data' may be a 2D array or a Markdown table. " +
		"mode=overwrite (default) replaces the sheet; mode=append adds after the last used row.",
	readOnly: false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":       map[string]any{"type": "string", "description": "Absolute path to the target file"},
			"sheet":      map[string]any{"type": "string", "description": "Sheet name (xlsx); default Sheet1"},
			"data":       map[string]any{"type": "array", "description": "2D array of rows, or a Markdown table string"},
			"mode":       map[string]any{"type": "string", "enum": []string{"overwrite", "append"}, "description": "Write mode (default overwrite)"},
			"start_cell": map[string]any{"type": "string", "description": "Top-left cell for the write (default A1)"},
		},
		"required": []string{"path", "data"},
	},
	run: runWriteSheet,
}

func runWriteSheet(args map[string]any) (any, error) {
	path, err := argString(args, "path")
	if err != nil {
		return nil, err
	}
	sheet := argStringDefault(args, "sheet", "Sheet1")
	mode := argStringDefault(args, "mode", "overwrite")
	startCell := argStringDefault(args, "start_cell", "A1")

	rows, err := extractRows(args["data"])
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("data is empty")
	}

	ext := strings.ToLower(filepath.Ext(path))
	var written int
	switch ext {
	case ".csv":
		written, err = writeCSV(path, rows, mode)
	case ".xlsx", ".xlsm":
		written, err = writeXLSX(path, sheet, rows, mode, startCell)
	default:
		return nil, fmt.Errorf("unsupported file extension %q (want .xlsx/.xlsm/.csv)", ext)
	}
	if err != nil {
		return nil, err
	}
	return fmt.Sprintf("wrote %d rows to %s (mode=%s)", written, path, mode), nil
}

// extractRows accepts either a 2D array (from JSON) or a Markdown table string.
func extractRows(v any) ([][]string, error) {
	switch d := v.(type) {
	case string:
		return parseMarkdownTable(d)
	case []any:
		rows := make([][]string, 0, len(d))
		for _, row := range d {
			r, ok := row.([]any)
			if !ok {
				return nil, fmt.Errorf("data rows must be arrays, got %T", row)
			}
			cells := make([]string, 0, len(r))
			for _, c := range r {
				cells = append(cells, toCellString(c))
			}
			rows = append(rows, cells)
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("data must be a 2D array or Markdown table string, got %T", v)
	}
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
//   - Header row: bold font, light blue background (#D9E1F2), thin border
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

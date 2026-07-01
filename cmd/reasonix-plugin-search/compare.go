package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// compareTableTool generates a comparison table from structured data.
// Outputs markdown by default; if output_format is xlsx, also writes an xlsx file.
var compareTableTool = toolDef{
	name: "compare_table",
	description: "Generate a comparison table from data. Accepts dimensions (column headers) " +
		"and items (each with a name and data map). Output as markdown (default) or xlsx.",
	readOnly: false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dimensions":    map[string]any{"type": "array", "description": "Column headers for the comparison (e.g. ['Price', 'Speed', 'Rating'])"},
			"items":         map[string]any{"type": "array", "description": "Array of objects with 'name' (string) and 'data' (object mapping dimensions to values)"},
			"output_format": map[string]any{"type": "string", "enum": []string{"markdown", "xlsx"}, "description": "Output format (default markdown)"},
		},
		"required": []string{"dimensions", "items"},
	},
	run: runCompareTable,
}

func runCompareTable(args map[string]any) (any, error) {
	dimensions, err := argStringSlice(args, "dimensions")
	if err != nil {
		return nil, err
	}
	if len(dimensions) == 0 {
		return nil, fmt.Errorf("dimensions must be non-empty")
	}

	itemsRaw, ok := args["items"]
	if !ok || itemsRaw == nil {
		return nil, fmt.Errorf("missing required argument \"items\"")
	}
	itemsArr, ok := itemsRaw.([]any)
	if !ok {
		return nil, fmt.Errorf("argument \"items\" must be an array, got %T", itemsRaw)
	}
	if len(itemsArr) == 0 {
		return nil, fmt.Errorf("items must be non-empty")
	}

	// Parse items: each item has a "name" (string) and "data" (map[string]any).
	type compareItem struct {
		Name string
		Data map[string]string
	}
	items := make([]compareItem, 0, len(itemsArr))
	for i, item := range itemsArr {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("items[%d] must be an object, got %T", i, item)
		}
		nameVal, ok := obj["name"]
		if !ok {
			return nil, fmt.Errorf("items[%d] missing 'name' field", i)
		}
		name, ok := nameVal.(string)
		if !ok {
			return nil, fmt.Errorf("items[%d].name must be a string, got %T", i, nameVal)
		}
		dataVal, ok := obj["data"]
		if !ok {
			return nil, fmt.Errorf("items[%d] missing 'data' field", i)
		}
		dataMap, ok := dataVal.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("items[%d].data must be an object, got %T", i, dataVal)
		}
		// Convert data values to strings.
		data := make(map[string]string, len(dataMap))
		for k, v := range dataMap {
			data[k] = toCellString(v)
		}
		items = append(items, compareItem{Name: name, Data: data})
	}

	outputFormat := argStringDefault(args, "output_format", "markdown")

	// Build the table rows: header row + one row per item.
	rows := make([][]string, 0, len(items)+1)
	header := append([]string{"Item"}, dimensions...)
	rows = append(rows, header)
	for _, item := range items {
		row := make([]string, 0, len(dimensions)+1)
		row = append(row, item.Name)
		for _, dim := range dimensions {
			val, ok := item.Data[dim]
			if !ok {
				val = "—"
			}
			row = append(row, val)
		}
		rows = append(rows, row)
	}

	switch outputFormat {
	case "xlsx":
		path, ok := args["path"].(string)
		if !ok || path == "" {
			return nil, fmt.Errorf("xlsx output requires a 'path' argument specifying the output file path")
		}
		return writeCompareXLSX(path, rows)
	default: // markdown
		return buildMarkdownTable(rows), nil
	}
}

// buildMarkdownTable renders rows as a Markdown pipe table (first row = header).
func buildMarkdownTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}

	var b strings.Builder
	// Header row.
	b.WriteString("| ")
	for i := 0; i < cols; i++ {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(cellAt(rows, 0, i))
	}
	b.WriteString(" |\n|")
	for i := 0; i < cols; i++ {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	// Data rows.
	for r := 1; r < len(rows); r++ {
		b.WriteString("| ")
		for i := 0; i < cols; i++ {
			if i > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(cellAt(rows, r, i))
		}
		b.WriteString(" |\n")
	}
	return b.String()
}

// cellAt safely returns a cell value, escaping pipe characters.
func cellAt(rows [][]string, r, c int) string {
	if r < 0 || r >= len(rows) {
		return ""
	}
	if c < 0 || c >= len(rows[r]) {
		return ""
	}
	return strings.ReplaceAll(rows[r][c], "|", "\\|")
}

// writeCompareXLSX writes the comparison table as a styled xlsx file,
// following the same approach as the sheet plugin.
func writeCompareXLSX(path string, rows [][]string) (any, error) {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create directory: %w", err)
		}
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".xlsx", ".xlsm":
		// OK
	case ".csv":
		return writeCompareCSV(path, rows)
	default:
		return nil, fmt.Errorf("unsupported file extension %q (want .xlsx/.csv)", ext)
	}

	f := excelize.NewFile()
	sheet := "Comparison"
	f.SetSheetName("Sheet1", sheet)

	// Write data.
	for r, row := range rows {
		for c, val := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
			_ = f.SetCellValue(sheet, cell, val)
		}
	}

	// Apply styles.
	applyCompareStyles(f, sheet, rows)

	if err := f.SaveAs(path); err != nil {
		return nil, fmt.Errorf("save xlsx: %w", err)
	}

	return fmt.Sprintf("Comparison table written to %s (%d items)", path, len(rows)-1), nil
}

// writeCompareCSV writes the comparison table as a CSV file.
func writeCompareCSV(path string, rows [][]string) (any, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create csv: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		return nil, fmt.Errorf("write csv: %w", err)
	}

	return fmt.Sprintf("Comparison table written to %s (%d items)", path, len(rows)-1), nil
}

// applyCompareStyles adds professional styling to the comparison xlsx,
// mirroring the sheet plugin's approach.
func applyCompareStyles(f *excelize.File, sheet string, rows [][]string) {
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
		cell, _ := excelize.CoordinatesToCellName(c+1, 1)
		_ = f.SetCellStyle(sheet, cell, cell, headerStyle)
	}

	// Apply body style to data rows.
	for r := 1; r < len(rows); r++ {
		for c := 0; c < numCols; c++ {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
			_ = f.SetCellStyle(sheet, cell, cell, bodyStyle)
		}
	}

	// Auto-fit column widths.
	for c := 0; c < numCols; c++ {
		maxLen := 0
		for _, row := range rows {
			if c < len(row) {
				cellLen := len(row[c])
				// CJK characters are roughly 2x wider.
				wideChars := 0
				for _, r := range row[c] {
					if r >= 0x4E00 && r <= 0x9FFF ||
						r >= 0x3000 && r <= 0x303F ||
						r >= 0xFF01 && r <= 0xFF60 {
						wideChars++
					}
				}
				cellLen += wideChars
				if cellLen > maxLen {
					maxLen = cellLen
				}
			}
		}
		width := float64(maxLen) + 2
		if width < 8 {
			width = 8
		}
		if width > 50 {
			width = 50
		}
		colName, _ := excelize.ColumnNumberToName(c + 1)
		_ = f.SetColWidth(sheet, colName, colName, width)
	}
}

// toCellString converts an arbitrary value to a cell string representation.
func toCellString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
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

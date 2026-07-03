package main

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"os"
	"strings"

	"github.com/xuri/excelize/v2"
)

// previewRowLimit caps the number of data rows extracted from a spreadsheet
// for preview. Keeping this bounded avoids token explosion on very large
// sheets (consistent with the read_sheet max_rows=1000 convention).
const previewRowLimit = 500

// parseDocxToHTML opens a .docx file (OOXML zip) and returns a self-contained
// HTML document with paragraphs, headings, and tables rendered. The HTML is
// served to the frontend via a media-token URL and displayed in an <iframe>.
func parseDocxToHTML(absPath string) (string, error) {
	xmlBody, err := readDocxDocumentXML(absPath)
	if err != nil {
		return "", err
	}
	bodyHTML, err := docxBodyToHTML(xmlBody)
	if err != nil {
		return "", err
	}
	return wrapHTML(html.EscapeString(fileNameStem(absPath)), bodyHTML), nil
}

// readDocxDocumentXML extracts word/document.xml from the docx zip archive.
func readDocxDocumentXML(path string) ([]byte, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open docx: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open document.xml: %w", err)
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("word/document.xml not found in docx")
}

// docxBodyToHTML parses the OOXML body into HTML. Token-based: the body
// contains <w:p> (paragraph) and <w:tbl> (table) siblings.
func docxBodyToHTML(xmlBody []byte) (string, error) {
	dec := xml.NewDecoder(strings.NewReader(string(xmlBody)))
	var b strings.Builder

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("xml parse: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "body" {
			continue
		}
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			start, ok := tok.(xml.StartElement)
			if !ok {
				continue
			}
			switch start.Name.Local {
			case "p":
				text, level := readDocxParagraph(dec)
				trimmed := strings.TrimSpace(text)
				if trimmed == "" {
					b.WriteString("<p>&nbsp;</p>\n")
				} else if level > 0 {
					fmt.Fprintf(&b, "<h%d>%s</h%d>\n", level, html.EscapeString(trimmed), level)
				} else {
					fmt.Fprintf(&b, "<p>%s</p>\n", html.EscapeString(trimmed))
				}
			case "tbl":
				b.WriteString(readDocxTableHTML(dec))
			}
		}
		break
	}
	return b.String(), nil
}

// readDocxParagraph consumes one <w:p> and returns its text and heading level.
func readDocxParagraph(dec *xml.Decoder) (string, int) {
	var text strings.Builder
	level := 0
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				text.WriteString(readDocxTextContent(dec))
				continue
			}
			depth++
			if t.Name.Local == "pStyle" {
				if v := docxAttrVal(t, "val"); v != "" {
					level = docxHeadingLevel(v)
				}
			}
			if t.Name.Local == "tab" {
				text.WriteString("\t")
			}
			if t.Name.Local == "br" {
				text.WriteString("\n")
			}
		case xml.EndElement:
			depth--
		}
	}
	return text.String(), level
}

// readDocxTableHTML consumes one <w:tbl> and returns an HTML table string.
func readDocxTableHTML(dec *xml.Decoder) string {
	var b strings.Builder
	b.WriteString(`<table class="docx-table">`)
	depth := 1
	rowIdx := 0
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "tr" {
				cells := readDocxTableRow(dec, t)
				tag := "td"
				if rowIdx == 0 {
					tag = "th"
				}
				b.WriteString("<tr>")
				for _, c := range cells {
					fmt.Fprintf(&b, "<%s>%s</%s>", tag, html.EscapeString(c), tag)
				}
				b.WriteString("</tr>")
				rowIdx++
			} else {
				depth++
			}
		case xml.EndElement:
			depth--
		}
	}
	b.WriteString("</table>\n")
	return b.String()
}

func readDocxTableRow(dec *xml.Decoder, start xml.StartElement) []string {
	var cells []string
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "tc" {
				cells = append(cells, readDocxTableCell(dec, t))
			} else {
				depth++
			}
		case xml.EndElement:
			depth--
		}
	}
	return cells
}

func readDocxTableCell(dec *xml.Decoder, start xml.StartElement) string {
	var text strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				text.WriteString(readDocxTextContent(dec))
				continue
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return strings.TrimSpace(text.String())
}

func readDocxTextContent(dec *xml.Decoder) string {
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return b.String()
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			return b.String()
		}
	}
}

func docxAttrVal(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func docxHeadingLevel(style string) int {
	s := strings.ToLower(style)
	if !strings.HasPrefix(s, "heading") && !strings.HasPrefix(s, "标题") {
		return 0
	}
	digits := ""
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits += string(c)
		}
	}
	if digits == "" {
		return 1
	}
	var n int
	fmt.Sscanf(digits, "%d", &n)
	if n < 1 {
		return 1
	}
	if n > 6 {
		return 6
	}
	return n
}

// --- XLSX parsing ---

// sheetTableData holds the structured rows extracted from a spreadsheet.
type sheetTableData struct {
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

// parseXlsxToTable opens a .xlsx file with excelize and returns the first
// sheet's data as headers (first row) + data rows. Output is capped at
// previewRowLimit data rows.
func parseXlsxToTable(absPath string) (*sheetTableData, error) {
	f, err := excelize.OpenFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("xlsx has no sheets")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("read sheet %q: %w", sheets[0], err)
	}
	if len(rows) == 0 {
		return &sheetTableData{Headers: []string{}, Rows: [][]string{}}, nil
	}

	// Determine column count from the first (header) row.
	cols := len(rows[0])
	headers := make([]string, cols)
	for i := 0; i < cols; i++ {
		if i < len(rows[0]) {
			headers[i] = strings.TrimSpace(rows[0][i])
		}
	}

	data := rows[1:]
	if len(data) > previewRowLimit {
		data = data[:previewRowLimit]
	}
	// Normalise each row to the header column count.
	normalised := make([][]string, len(data))
	for i, r := range data {
		row := make([]string, cols)
		for j := 0; j < cols; j++ {
			if j < len(r) {
				row[j] = r[j]
			}
		}
		normalised[i] = row
	}
	return &sheetTableData{Headers: headers, Rows: normalised}, nil
}

// parseXlsxToHTML opens a .xlsx file and returns an HTML table document.
func parseXlsxToHTML(absPath string) (string, error) {
	td, err := parseXlsxToTable(absPath)
	if err != nil {
		return "", err
	}
	return tableDataToHTML(td, fileNameStem(absPath)), nil
}

// parseCSVToHTML reads a CSV file and returns an HTML table document.
func parseCSVToHTML(absPath string) (string, error) {
	td, err := parseCSVToTable(absPath)
	if err != nil {
		return "", err
	}
	return tableDataToHTML(td, fileNameStem(absPath)), nil
}

// parseCSVToTable reads a CSV file and returns structured table data.
func parseCSVToTable(absPath string) (*sheetTableData, error) {
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) == 0 {
		return &sheetTableData{Headers: []string{}, Rows: [][]string{}}, nil
	}
	return csvToTableData(lines), nil
}

// csvToTableData converts CSV lines into sheetTableData (first line = headers).
func csvToTableData(lines []string) *sheetTableData {
	parseLine := func(s string) []string {
		// Simple CSV parse: split by comma, trim quotes.
		var fields []string
		var field strings.Builder
		inQuote := false
		for _, c := range s {
			if c == '"' {
				inQuote = !inQuote
			} else if c == ',' && !inQuote {
				fields = append(fields, strings.TrimSpace(field.String()))
				field.Reset()
			} else {
				field.WriteRune(c)
			}
		}
		fields = append(fields, strings.TrimSpace(field.String()))
		return fields
	}

	if len(lines) == 0 {
		return &sheetTableData{Headers: []string{}, Rows: [][]string{}}
	}
	headers := parseLine(lines[0])
	var rows [][]string
	data := lines[1:]
	if len(data) > previewRowLimit {
		data = data[:previewRowLimit]
	}
	for _, line := range data {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows = append(rows, parseLine(line))
	}
	return &sheetTableData{Headers: headers, Rows: rows}
}

// tableDataToHTML renders sheetTableData as an HTML document with a styled table.
func tableDataToHTML(td *sheetTableData, title string) string {
	var b strings.Builder
	b.WriteString(`<table class="sheet-table">`)
	b.WriteString("<thead><tr>")
	for _, h := range td.Headers {
		fmt.Fprintf(&b, "<th>%s</th>", html.EscapeString(h))
	}
	b.WriteString("</tr></thead><tbody>")
	for _, row := range td.Rows {
		b.WriteString("<tr>")
		for _, cell := range row {
			fmt.Fprintf(&b, "<td>%s</td>", html.EscapeString(cell))
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table>")
	return wrapHTML(title, b.String())
}

// wrapHTML wraps a body fragment in a minimal HTML document with basic
// styling for readable inline preview in an <iframe>.
func wrapHTML(title, body string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
  :root { color-scheme: light dark; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
    font-size: 14px;
    line-height: 1.6;
    margin: 24px;
    padding: 0;
  }
  h1, h2, h3, h4, h5, h6 { margin: 1em 0 0.4em; }
  p { margin: 0.5em 0; }
  table { border-collapse: collapse; width: 100%%; margin: 1em 0; font-size: 13px; }
  th, td { border: 1px solid rgba(128,128,128,0.3); padding: 4px 8px; text-align: left; }
  th { background: rgba(128,128,128,0.12); font-weight: 600; }
  tr:nth-child(even) td { background: rgba(128,128,128,0.05); }
  @media (prefers-color-scheme: dark) {
    body { color: #e0e0e0; }
    th { background: rgba(255,255,255,0.08); }
    tr:nth-child(even) td { background: rgba(255,255,255,0.03); }
  }
</style>
</head>
<body>
%s
</body>
</html>`, title, body)
}

// fileNameStem returns the file name without extension, e.g. "report" for
// "/path/to/report.docx".
func fileNameStem(absPath string) string {
	base := absPath
	if idx := strings.LastIndexAny(base, `/\`); idx >= 0 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "."); idx > 0 {
		base = base[:idx]
	}
	return base
}

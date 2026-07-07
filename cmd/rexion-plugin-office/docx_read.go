package main

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

// readDocxTool reads a .docx file and returns its text as Markdown (headings,
// paragraphs, tables). Read-only.
//
// Implementation: a .docx is a zip whose word/document.xml holds the body. We
// parse it with a token-based XML decoder (the body mixes <w:p> paragraphs and
// <w:tbl> tables as siblings) and emit Markdown.
var readDocxTool = toolDef{
	name: "read_docx",
	description: "Read a .docx file and return its content as Markdown (headings, paragraphs, tables). " +
		"Use mode=outline for a headings-only view to save tokens. " +
		"max_chars caps output (default 20000).",
	readOnly: true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":      map[string]any{"type": "string", "description": "Absolute path to the .docx file"},
			"mode":      map[string]any{"type": "string", "enum": []string{"full", "outline"}, "description": "outline = headings only (default full)"},
			"max_chars": map[string]any{"type": "integer", "description": "Output cap (default 20000)"},
		},
		"required": []string{"path"},
	},
	run: runReadDocx,
}

func runReadDocx(args map[string]any) (any, error) {
	path, err := argString(args, "path")
	if err != nil {
		return nil, err
	}
	mode := argStringDefault(args, "mode", "full")
	maxChars := argIntDefault(args, "max_chars", 20000)

	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	xmlBody, err := readDocumentXML(path)
	if err != nil {
		return nil, err
	}

	md, paraN, tableN, err := documentXMLToMarkdown(xmlBody, mode, maxChars)
	if err != nil {
		return nil, err
	}
	summary := fmt.Sprintf("(paragraphs: %d, tables: %d, mode: %s, chars: %d)\n\n", paraN, tableN, mode, len(md))
	return summary + md, nil
}

// readDocumentXML extracts word/document.xml from the docx zip.
func readDocumentXML(path string) ([]byte, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open docx (not a valid zip?): %w", err)
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

// documentXMLToMarkdown parses the OOXML body into Markdown.
// Token-based: body contains <w:p> (paragraph) and <w:tbl> (table) siblings.
func documentXMLToMarkdown(xmlBody []byte, mode string, maxChars int) (string, int, int, error) {
	dec := xml.NewDecoder(strings.NewReader(string(xmlBody)))
	var b strings.Builder
	paraN, tableN := 0, 0

	// Walk tokens until we hit w:body, then process its children.
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", 0, 0, fmt.Errorf("xml parse: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local != "body" {
			continue
		}
		// Now inside w:body: iterate siblings.
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			start, ok := tok.(xml.StartElement)
			if !ok {
				continue
			}
			if start.Name.Local == "p" {
				text, level := readParagraph(dec, start)
				paraN++
				if mode == "outline" && level == 0 {
					continue // outline skips non-headings
				}
				if level > 0 {
					b.WriteString(strings.Repeat("#", level) + " " + text + "\n\n")
				} else if strings.TrimSpace(text) != "" {
					b.WriteString(text + "\n\n")
				}
			} else if start.Name.Local == "tbl" {
				if mode == "outline" {
					skipElement(dec, start.Name.Local)
					continue
				}
				tableN++
				b.WriteString(readTable(dec, start))
				b.WriteString("\n")
			}
			if b.Len() > maxChars {
				b.WriteString("\n... (truncated at max_chars)\n")
				return b.String(), paraN, tableN, nil
			}
		}
		break
	}
	return b.String(), paraN, tableN, nil
}

// readParagraph consumes one <w:p> and returns its text and heading level
// (0 = normal paragraph, 1-6 = heading depth). It reads until the matching </w:p>.
func readParagraph(dec *xml.Decoder, start xml.StartElement) (string, int) {
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
			// <w:t>'s closing tag is consumed by readTextContent; for every
			// other child we increment depth and wait for the matching EE.
			if t.Name.Local == "t" {
				text.WriteString(readTextContent(dec))
				continue
			}
			depth++
			if t.Name.Local == "pStyle" {
				if v := attrVal(t, "val"); v != "" {
					level = headingLevel(v)
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

// readTable consumes one <w:tbl> and returns a Markdown table.
func readTable(dec *xml.Decoder, start xml.StartElement) string {
	var rows [][]string
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "tr" {
				row := readTableRow(dec, t)
				rows = append(rows, row)
			} else {
				depth++
			}
		case xml.EndElement:
			depth--
		}
	}
	if len(rows) == 0 {
		return ""
	}
	return rowsToMarkdownTable(rows)
}

func readTableRow(dec *xml.Decoder, start xml.StartElement) []string {
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
				cells = append(cells, readTableCell(dec, t))
			} else {
				depth++
			}
		case xml.EndElement:
			depth--
		}
	}
	return cells
}

func readTableCell(dec *xml.Decoder, start xml.StartElement) string {
	var text strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			// <w:t>'s closing tag is consumed by readTextContent.
			if t.Name.Local == "t" {
				text.WriteString(readTextContent(dec))
				continue
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return strings.TrimSpace(text.String())
}

// readTextContent reads the character data inside a <w:t> element.
func readTextContent(dec *xml.Decoder) string {
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

// skipElement consumes tokens until the matching end of the named element.
func skipElement(dec *xml.Decoder, name string) {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
}

// attrVal reads the value of a named attribute from a start element.
func attrVal(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// headingLevel maps OOXML style names to Markdown heading depth.
func headingLevel(style string) int {
	s := strings.ToLower(style)
	if !strings.HasPrefix(s, "heading") && !strings.HasPrefix(s, "标题") {
		return 0
	}
	// "Heading1" → 1, "heading 2" → 2, "标题1" → 1
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

// rowsToMarkdownTable renders rows as a Markdown pipe table (first row = header).
func rowsToMarkdownTable(rows [][]string) string {
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

func cellAt(rows [][]string, r, c int) string {
	if r < 0 || r >= len(rows) {
		return ""
	}
	if c < 0 || c >= len(rows[r]) {
		return ""
	}
	return strings.ReplaceAll(rows[r][c], "|", "\\|")
}

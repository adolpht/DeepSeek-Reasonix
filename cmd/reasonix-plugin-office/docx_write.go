package main

import (
	"archive/zip"
	"fmt"
	"os"
	"strings"
)

// writeDocxTool writes Markdown content as a .docx file. NOT read-only.
//
// Implementation: a .docx is a zip of XML parts. We generate the minimal set
// ([Content_Types].xml, _rels/.rels, word/document.xml) so Word/WPS opens it.
// Markdown headings (#, ##, ###) map to Heading styles; paragraphs map to <w:p>;
// tables (| a | b |) map to <w:tbl>. Other Markdown inline syntax is preserved
// as plain text — we are not a full Markdown renderer.
var writeDocxTool = toolDef{
	name: "write_docx",
	description: "Write Markdown content to a .docx file. Headings (#/##/###) and tables (| a | b |) " +
		"are converted to native Word styles; other text becomes plain paragraphs. " +
		"Overwrites any existing file.",
	readOnly: false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "Absolute output path (.docx)"},
			"content": map[string]any{"type": "string", "description": "Markdown content"},
			"title":   map[string]any{"type": "string", "description": "Document title metadata (optional)"},
		},
		"required": []string{"path", "content"},
	},
	run: runWriteDocx,
}

func runWriteDocx(args map[string]any) (any, error) {
	path, err := argString(args, "path")
	if err != nil {
		return nil, err
	}
	content, err := argString(args, "content")
	if err != nil {
		return nil, err
	}
	title := argStringDefault(args, "title", "")

	// Reject .docx-incompatible extensions early.
	if !strings.HasSuffix(strings.ToLower(path), ".docx") {
		return nil, fmt.Errorf("output path must end with .docx, got %q", path)
	}

	blocks := parseMarkdownBlocks(content)
	xml := buildDocumentXML(blocks)
	relsXML := buildRelsXML()
	contentTypesXML := buildContentTypesXML()
	rootRelsXML := buildRootRelsXML()
	coreXML := buildCoreXML(title)

	if err := writeDocxZip(path, xml, relsXML, contentTypesXML, rootRelsXML, coreXML); err != nil {
		return nil, err
	}
	return fmt.Sprintf("wrote %d blocks to %s", len(blocks), path), nil
}

// mdBlock is one rendered paragraph/heading/table in the document body.
type mdBlock struct {
	kind  string // "h1".."h6" | "p" | "table" | "empty"
	text  string
	rows  [][]string
}

// parseMarkdownBlocks converts Markdown text into structured blocks.
// Intentionally simple: headings, pipe tables, blank lines, and paragraphs.
func parseMarkdownBlocks(md string) []mdBlock {
	lines := strings.Split(md, "\n")
	var blocks []mdBlock
	i := 0
	for i < len(lines) {
		ln := strings.TrimRight(lines[i], "\r")
		trimmed := strings.TrimSpace(ln)

		// Blank line.
		if trimmed == "" {
			blocks = append(blocks, mdBlock{kind: "empty"})
			i++
			continue
		}

		// Heading.
		if h, level := parseHeading(trimmed); level > 0 {
			blocks = append(blocks, mdBlock{kind: fmt.Sprintf("h%d", level), text: h})
			i++
			continue
		}

		// Table: a line starting with | followed by a separator line.
		if strings.HasPrefix(trimmed, "|") && i+1 < len(lines) && isTableSeparator(strings.TrimSpace(lines[i+1])) {
			rows := parseTableLines(lines[i:])
			i += len(rows) + 1 // rows + separator
			if len(rows) > 0 {
				blocks = append(blocks, mdBlock{kind: "table", rows: rows})
			}
			continue
		}

		// Plain paragraph.
		blocks = append(blocks, mdBlock{kind: "p", text: trimmed})
		i++
	}
	return blocks
}

func parseHeading(s string) (string, int) {
	if !strings.HasPrefix(s, "#") {
		return "", 0
	}
	level := 0
	for level < len(s) && s[level] == '#' {
		level++
	}
	if level > 6 {
		return "", 0
	}
	if level >= len(s) || s[level] != ' ' {
		return "", 0
	}
	return strings.TrimSpace(s[level+1:]), level
}

func isTableSeparator(s string) bool {
	if !strings.HasPrefix(s, "|") {
		return false
	}
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	for _, c := range s {
		if c != '-' && c != ':' && c != ' ' && c != '|' {
			return false
		}
	}
	return true
}

func parseTableLines(lines []string) [][]string {
	var rows [][]string
	for _, ln := range lines {
		ln = strings.TrimSpace(strings.TrimRight(ln, "\r"))
		if ln == "" || !strings.HasPrefix(ln, "|") {
			break
		}
		if isTableSeparator(ln) {
			continue
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(ln, "|"), "|")
		cells := strings.Split(inner, "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(strings.ReplaceAll(cells[i], "\\|", "|"))
		}
		rows = append(rows, cells)
	}
	return rows
}

// buildDocumentXML renders the word/document.xml body from blocks.
func buildDocumentXML(blocks []mdBlock) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	b.WriteString(`<w:body>`)
	for _, blk := range blocks {
		switch blk.kind {
		case "empty":
			b.WriteString(`<w:p/>`)
		case "h1", "h2", "h3", "h4", "h5", "h6":
			level := blk.kind[1]
			style := "Heading" + string(level)
			b.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val=%q/></w:pPr>`, style))
			b.WriteString(runXML(blk.text))
			b.WriteString(`</w:p>`)
		case "table":
			b.WriteString(buildTableXML(blk.rows))
		default: // "p"
			b.WriteString(`<w:p>`)
			b.WriteString(runXML(blk.text))
			b.WriteString(`</w:p>`)
		}
	}
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

// runXML wraps text in a <w:r><w:t xml:space="preserve">…</w:t></w:r>.
func runXML(text string) string {
	return fmt.Sprintf(`<w:r><w:t xml:space="preserve">%s</w:t></w:r>`, escapeXML(text))
}

func buildTableXML(rows [][]string) string {
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
	b.WriteString(`<w:tbl>`)
	// Table grid.
	b.WriteString(`<w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/></w:tblPr>`)
	b.WriteString(fmt.Sprintf(`<w:tblGrid>%s</w:tblGrid>`, strings.Repeat(`<w:gridCol w:w="2000"/>`, cols)))
	for _, row := range rows {
		b.WriteString(`<w:tr>`)
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			b.WriteString(`<w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/></w:tcPr>`)
			b.WriteString(`<w:p>`)
			b.WriteString(runXML(cell))
			b.WriteString(`</w:p></w:tc>`)
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	return b.String()
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// --- the other minimal docx parts ---

func buildRelsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
		`</Relationships>`
}

func buildContentTypesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
		`</Types>`
}

func buildRootRelsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
		`</Relationships>`
}

func buildCoreXML(title string) string {
	if title == "" {
		return ""
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
		`<dc:title>` + escapeXML(title) + `</dc:title>` +
		`</cp:coreProperties>`
}

// writeDocxZip assembles the parts into a .docx zip at path.
func writeDocxZip(path, documentXML, relsXML, contentTypesXML, rootRelsXML, coreXML string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	z := zip.NewWriter(f)
	defer z.Close()

	add := func(name, body string) error {
		w, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(body))
		return err
	}
	if err := add("[Content_Types].xml", contentTypesXML); err != nil {
		return err
	}
	if err := add("_rels/.rels", relsXML); err != nil {
		return err
	}
	if err := add("word/document.xml", documentXML); err != nil {
		return err
	}
	if coreXML != "" {
		if err := add("docProps/core.xml", coreXML); err != nil {
			return err
		}
	}
	return nil
}

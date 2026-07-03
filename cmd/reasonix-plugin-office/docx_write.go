package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nineya/wordZero/pkg/document"
	"github.com/nineya/wordZero/pkg/markdown"
)

// writeDocxTool writes Markdown content as a .docx file using the wordZero library.
// NOT read-only.
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

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			_ = os.MkdirAll(dir, 0o755)
		}
	}

	blocks := parseMarkdownBlocks(content)
	if err := writeDocxWordZero(path, blocks, title); err != nil {
		return nil, err
	}
	return fmt.Sprintf("wrote %d blocks to %s", len(blocks), path), nil
}

// mdBlock is one rendered paragraph/heading/table in the document body.
type mdBlock struct {
	kind string // "h1".."h6" | "p" | "table" | "empty"
	text string
	rows [][]string
}

// parseMarkdownBlocks converts Markdown text into structured blocks.
func parseMarkdownBlocks(md string) []mdBlock {
	lines := strings.Split(md, "\n")
	var blocks []mdBlock
	i := 0
	for i < len(lines) {
		ln := strings.TrimRight(lines[i], "\r")
		trimmed := strings.TrimSpace(ln)

		if trimmed == "" {
			blocks = append(blocks, mdBlock{kind: "empty"})
			i++
			continue
		}

		if h, level := parseHeading(trimmed); level > 0 {
			blocks = append(blocks, mdBlock{kind: fmt.Sprintf("h%d", level), text: h})
			i++
			continue
		}

		if strings.HasPrefix(trimmed, "|") && i+1 < len(lines) && isTableSeparator(strings.TrimSpace(lines[i+1])) {
			rows := parseTableLines(lines[i:])
			i += len(rows) + 1
			if len(rows) > 0 {
				blocks = append(blocks, mdBlock{kind: "table", rows: rows})
			}
			continue
		}

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

// writeDocxWordZero creates a .docx file using the wordZero library.
func writeDocxWordZero(path string, blocks []mdBlock, title string) error {
	mdText := rebuildMarkdown(blocks)
	converter := markdown.NewConverter(markdown.DefaultOptions())
	doc, err := converter.ConvertString(mdText, nil)
	if err == nil {
		if title != "" {
			hasTitleHeading := false
			for _, blk := range blocks {
				if blk.kind == "h1" && blk.text == title {
					hasTitleHeading = true
					break
				}
			}
			if !hasTitleHeading {
				doc.AddHeadingParagraph(title, 1)
			}
		}
		return doc.Save(path)
	}

	// Fallback: build document manually from parsed blocks.
	doc = document.New()
	for _, blk := range blocks {
		switch blk.kind {
		case "empty":
			doc.AddParagraph("")
		case "h1":
			doc.AddHeadingParagraph(blk.text, 1)
		case "h2":
			doc.AddHeadingParagraph(blk.text, 2)
		case "h3":
			doc.AddHeadingParagraph(blk.text, 3)
		case "h4":
			doc.AddHeadingParagraph(blk.text, 4)
		case "h5":
			doc.AddHeadingParagraph(blk.text, 5)
		case "h6":
			doc.AddHeadingParagraph(blk.text, 6)
		case "table":
			addTablePlugin(doc, blk.rows)
		default:
			doc.AddParagraph(blk.text)
		}
	}
	return doc.Save(path)
}

func rebuildMarkdown(blocks []mdBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		switch blk.kind {
		case "empty":
			b.WriteString("\n")
		case "h1", "h2", "h3", "h4", "h5", "h6":
			level := blk.kind[1]
			b.WriteString(strings.Repeat("#", int(level-'0')))
			b.WriteString(" ")
			b.WriteString(blk.text)
			b.WriteString("\n")
		case "table":
			if len(blk.rows) == 0 {
				continue
			}
			cols := 0
			for _, r := range blk.rows {
				if len(r) > cols {
					cols = len(r)
				}
			}
			b.WriteString("|")
			for c := 0; c < cols; c++ {
				cell := ""
				if c < len(blk.rows[0]) {
					cell = blk.rows[0][c]
				}
				b.WriteString(cell)
				b.WriteString("|")
			}
			b.WriteString("\n|")
			for c := 0; c < cols; c++ {
				b.WriteString("---|")
			}
			b.WriteString("\n")
			for r := 1; r < len(blk.rows); r++ {
				b.WriteString("|")
				for c := 0; c < cols; c++ {
					cell := ""
					if c < len(blk.rows[r]) {
						cell = blk.rows[r][c]
					}
					b.WriteString(cell)
					b.WriteString("|")
				}
				b.WriteString("\n")
			}
		default:
			b.WriteString(blk.text)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func addTablePlugin(doc *document.Document, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}

	cfg := &document.TableConfig{
		Rows: len(rows),
		Cols: cols,
	}
	tbl, _ := doc.AddTable(cfg)

	for r, row := range rows {
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			_ = tbl.SetCellText(r, c, cell)
		}
	}
}

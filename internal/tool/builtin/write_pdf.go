package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/signintech/gopdf"

	"rexion/internal/tool"
)

func init() { tool.RegisterBuiltin(writePdf{}) }

// writePdf writes Markdown content to a PDF file using the gopdf library.
// roots, when non-empty, confines the target to the workspace (see confine);
// the zero value registered at init is unconfined and is overridden per run
// by ConfineWriters. workDir, when non-empty, is the directory a relative
// path resolves against (see resolveIn).
type writePdf struct {
	roots   []string
	workDir string
}

func (writePdf) Name() string { return "write_pdf" }

func (writePdf) Description() string {
	return "Write Markdown content to a PDF file. Headings (#/##/###) become larger fonts; paragraphs become text blocks. Tables are rendered as grid. Overwrites any existing file."
}

func (writePdf) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"content":{"type":"string","description":"Markdown content to convert to PDF"},"title":{"type":"string","description":"Optional document title"}},"required":["path","content"]}`)
}

func (writePdf) ReadOnly() bool { return false }

func (w writePdf) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Title   string `json:"title"`
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
	if dir := filepath.Dir(p.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := generatePDF(p.Path, p.Content, p.Title); err != nil {
		return "", fmt.Errorf("generate PDF %s: %w", p.Path, err)
	}
	return fmt.Sprintf("wrote PDF to %s", p.Path), nil
}

// generatePDF creates a PDF file from Markdown content using gopdf.
func generatePDF(path, content, title string) error {
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{
		PageSize: *gopdf.PageSizeA4,
	})
	pdf.SetMargins(40, 40, 40, 40) // left, top, right, bottom (in points)

	// Add a built-in Helvetica-style font. gopdf requires TTF fonts;
	// we embed a standard font name "helvetica" as the default.
	// Note: gopdf requires TTF font files. If no font is loaded,
	// text rendering will fail, so we must always register one.
	err := pdf.AddTTFFont("helvetica", "")
	if err != nil {
		// No built-in font path available from gopdf without a TTF file.
		// As a fallback, we try the system's Arial/Helvetica.
		fontPaths := []string{
			"C:/Windows/Fonts/arial.ttf",                      // Windows
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", // Linux
			"/System/Library/Fonts/Helvetica.ttf",             // macOS
		}
		for _, fp := range fontPaths {
			if err := pdf.AddTTFFont("helvetica", fp); err == nil {
				break
			}
		}
	}

	// Also register a bold variant for headings.
	boldPaths := []string{
		"C:/Windows/Fonts/arialbd.ttf",                         // Windows
		"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", // Linux
		"/System/Library/Fonts/HelveticaBold.ttf",              // macOS
	}
	for _, fp := range boldPaths {
		if err := pdf.AddTTFFont("helvetica-bold", fp); err == nil {
			break
		}
	}

	// Set document metadata via PDF info.
	if title != "" {
		pdf.SetInfo(gopdf.PdfInfo{
			Title:  title,
			Author: "Rexion",
		})
	}

	pdf.AddPage()

	// Parse and render Markdown content.
	lines := strings.Split(content, "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]

		// Skip empty lines.
		if strings.TrimSpace(line) == "" {
			pdf.Br(8)
			i++
			continue
		}

		// Check for headings (try longer prefixes first so ### wins over ##, etc.).
		if strings.HasPrefix(line, "### ") {
			renderHeadingGopdf(&pdf, line[4:], 3)
			i++
			continue
		}
		if strings.HasPrefix(line, "## ") {
			renderHeadingGopdf(&pdf, line[3:], 2)
			i++
			continue
		}
		if strings.HasPrefix(line, "# ") {
			renderHeadingGopdf(&pdf, line[2:], 1)
			i++
			continue
		}

		// Check for tables (lines starting with |).
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			i = renderTableGopdf(&pdf, lines, i)
			continue
		}

		// Regular paragraph — collect consecutive non-empty, non-special lines.
		var paragraph []string
		for i < len(lines) {
			curr := lines[i]
			trimmed := strings.TrimSpace(curr)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "|") {
				break
			}
			paragraph = append(paragraph, curr)
			i++
		}
		if len(paragraph) > 0 {
			renderParagraphGopdf(&pdf, strings.Join(paragraph, " "))
		}
	}

	return pdf.WritePdf(path)
}

// renderHeadingGopdf renders a heading with appropriate font size.
func renderHeadingGopdf(pdf *gopdf.GoPdf, text string, level int) {
	var size int
	switch level {
	case 1:
		size = 20
	case 2:
		size = 16
	case 3:
		size = 14
	default:
		size = 12
	}

	pdf.Br(8)
	if err := pdf.SetFont("helvetica-bold", "", size); err != nil {
		pdf.SetFont("helvetica", "", size)
	}
	pdf.MultiCellWithOption(&gopdf.Rect{W: 515, H: float64(size) + 4}, text, gopdf.CellOption{Align: gopdf.Left | gopdf.Top})
	pdf.Br(6)
}

// renderParagraphGopdf renders a paragraph of text.
func renderParagraphGopdf(pdf *gopdf.GoPdf, text string) {
	pdf.SetFont("helvetica", "", 11)
	pdf.MultiCellWithOption(&gopdf.Rect{W: 515, H: 18}, text, gopdf.CellOption{Align: gopdf.Left | gopdf.Top})
	pdf.Br(8)
}

// renderTableGopdf renders a simple table from Markdown lines.
func renderTableGopdf(pdf *gopdf.GoPdf, lines []string, startIdx int) int {
	// Collect table rows.
	var rows [][]string
	i := startIdx

	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "|") {
			break
		}
		// Skip separator lines (e.g., |---|---|).
		if strings.Contains(line, "---") {
			i++
			continue
		}
		cells := parseTableRowGopdf(line)
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
		i++
	}

	if len(rows) == 0 {
		return i
	}

	// Calculate column count.
	numCols := 0
	for _, row := range rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}
	if numCols == 0 {
		return i
	}

	// Page width minus margins ≈ 515 points for A4 with 40pt margins.
	pageWidth := 515.0
	colWidth := pageWidth / float64(numCols)

	pdf.SetFont("helvetica", "", 10)
	lineHeight := 18.0

	for rowIdx, row := range rows {
		// Check if we need a new page.
		if pdf.GetY()+lineHeight > 750 {
			pdf.AddPage()
		}

		// Make header row bold.
		if rowIdx == 0 {
			pdf.SetFont("helvetica-bold", "", 10)
		} else {
			pdf.SetFont("helvetica", "", 10)
		}

		startX := pdf.GetX()
		startY := pdf.GetY()

		for colIdx := 0; colIdx < numCols; colIdx++ {
			cellText := ""
			if colIdx < len(row) {
				cellText = row[colIdx]
			}

			x := startX + float64(colIdx)*colWidth

			// Draw cell border.
			pdf.RectFromUpperLeftWithStyle(x, startY, colWidth, lineHeight, "D")

			// Write cell text inside the border.
			pdf.SetXY(x+2, startY+2)
			pdf.Cell(&gopdf.Rect{W: colWidth - 4, H: lineHeight - 4}, cellText)
		}
		pdf.SetXY(startX, startY+lineHeight)
	}

	pdf.Br(8)
	return i
}

// parseTableRowGopdf parses a Markdown table row into cells.
func parseTableRowGopdf(line string) []string {
	line = strings.Trim(line, "|")
	parts := strings.Split(line, "|")
	var cells []string
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

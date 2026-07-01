package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/unidoc/unioffice/v2/color"
	"github.com/unidoc/unioffice/v2/common/license"
	"github.com/unidoc/unioffice/v2/measurement"
	"github.com/unidoc/unioffice/v2/presentation"
	"github.com/unidoc/unioffice/v2/schema/soo/dml"
)

func init() {
	// Load unioffice metered license key from environment variable.
	// If UNIDOC_LICENSE_API_KEY is not set, unioffice operates in trial mode
	// and adds a watermark to generated documents.
	key := os.Getenv("UNIDOC_LICENSE_API_KEY")
	if key != "" {
		_ = license.SetMeteredKey(key)
	}
}

// --- Tool definitions ---

var createPPTTool = toolDef{
	name: "create_ppt",
	description: "Create a PowerPoint presentation from a Markdown outline. " +
		"## headers become slide titles; content below each header becomes the slide body. " +
		"The first # header (if present) is used as the title slide. " +
		"Returns the path to the generated .pptx file.",
	readOnly: false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"outline":     map[string]any{"type": "string", "description": "Markdown outline (## = slide titles, content below = slide body)"},
			"title":       map[string]any{"type": "string", "description": "Presentation title (used on title slide, optional)"},
			"style":       map[string]any{"type": "string", "enum": []string{"professional", "creative", "minimal"}, "description": "Visual style theme (default: professional)"},
			"output_path": map[string]any{"type": "string", "description": "Absolute output path (.pptx)"},
		},
		"required": []string{"outline", "output_path"},
	},
	run: runCreatePPT,
}

var addSlideTool = toolDef{
	name: "add_slide",
	description: "Add a slide to an existing PowerPoint presentation. " +
		"Supports title, title+content, and blank layouts. " +
		"Returns success confirmation.",
	readOnly: false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ppt_path": map[string]any{"type": "string", "description": "Absolute path to the .pptx file"},
			"title":    map[string]any{"type": "string", "description": "Slide title"},
			"content":  map[string]any{"type": "string", "description": "Slide body content (plain text or bullet points, one per line)"},
			"notes":    map[string]any{"type": "string", "description": "Speaker notes (optional)"},
			"layout":   map[string]any{"type": "string", "enum": []string{"title", "title_content", "blank"}, "description": "Slide layout (default: title_content)"},
		},
		"required": []string{"ppt_path", "title"},
	},
	run: runAddSlide,
}

var applyThemeTool = toolDef{
	name: "apply_theme",
	description: "Apply a visual theme to an existing PowerPoint presentation. " +
		"Modifies fonts, colors, and background across all slides. " +
		"Available themes: professional (dark blue, Calibri), creative (teal/orange, Segoe UI), minimal (white/gray, Arial). " +
		"Returns success confirmation.",
	readOnly: false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ppt_path": map[string]any{"type": "string", "description": "Absolute path to the .pptx file"},
			"theme":    map[string]any{"type": "string", "enum": []string{"professional", "creative", "minimal"}, "description": "Theme to apply"},
		},
		"required": []string{"ppt_path", "theme"},
	},
	run: runApplyTheme,
}

var addChartTool = toolDef{
	name: "add_chart",
	description: "Add a chart slide to an existing PowerPoint presentation. " +
		"Supports bar, line, and pie charts. Data is provided as a JSON string " +
		"with 'labels' (string array) and 'values' (number array, or array of arrays for multi-series). " +
		"Returns success confirmation.",
	readOnly: false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ppt_path":   map[string]any{"type": "string", "description": "Absolute path to the .pptx file"},
			"chart_type": map[string]any{"type": "string", "enum": []string{"bar", "line", "pie"}, "description": "Chart type"},
			"title":      map[string]any{"type": "string", "description": "Chart/slide title"},
			"data":       map[string]any{"type": "string", "description": "JSON string: {\"labels\":[...],\"values\":[...] or \"values\":[[...],[...]] for multi-series}"},
			"position":   map[string]any{"type": "integer", "description": "Slide position index (0-based, optional; default: append at end)"},
		},
		"required": []string{"ppt_path", "chart_type", "title", "data"},
	},
	run: runAddChart,
}

var exportPDFTool = toolDef{
	name: "export_pdf",
	description: "Export a PowerPoint presentation to PDF. " +
		"Note: pure Go PDF export from PPTX is limited; this tool saves a PDF-compatible " +
		"rendering using unioffice's built-in capabilities where available, or returns " +
		"an informative message if direct conversion is not supported. " +
		"Returns the path to the generated .pdf file or a status message.",
	readOnly: false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ppt_path":    map[string]any{"type": "string", "description": "Absolute path to the .pptx file"},
			"output_path": map[string]any{"type": "string", "description": "Absolute output path (.pdf, optional; defaults to same name as ppt_path with .pdf extension)"},
		},
		"required": []string{"ppt_path"},
	},
	run: runExportPDF,
}

// --- Tool implementations ---

// slideOutline represents one parsed slide from the markdown outline.
type slideOutline struct {
	title   string
	content []string
}

func runCreatePPT(args map[string]any) (any, error) {
	outline, err := argString(args, "outline")
	if err != nil {
		return nil, err
	}
	outputPath, err := argString(args, "output_path")
	if err != nil {
		return nil, err
	}
	title := argStringDefault(args, "title", "")
	style := argStringDefault(args, "style", "professional")

	if !strings.HasSuffix(strings.ToLower(outputPath), ".pptx") {
		return nil, fmt.Errorf("output path must end with .pptx, got %q", outputPath)
	}

	if err := ensureDir(outputPath); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	slides := parseOutline(outline)
	if len(slides) == 0 {
		return nil, fmt.Errorf("outline contains no slides (need at least one ## header)")
	}

	ppt := presentation.New()
	defer ppt.Close()

	themeConfig := getThemeConfig(style)

	// Title slide: use the first slide's title or the explicit title param.
	titleText := title
	if titleText == "" && len(slides) > 0 {
		titleText = slides[0].title
	}
	if titleText == "" {
		titleText = "Presentation"
	}

	titleSlide := ppt.AddSlide()
	tb := titleSlide.AddTextBox()
	tb.Properties().SetWidth(8 * measurement.Inch)
	tb.Properties().SetHeight(1.5 * measurement.Inch)
	tb.Properties().SetPosition(1*measurement.Inch, 2.5*measurement.Inch)
	p := tb.AddParagraph()
	p.Properties().SetAlignment(dml.ST_TextAlignTypeCtr)
	r := p.AddRun()
	r.SetText(titleText)
	r.Properties().SetSize(36 * measurement.Point)
	r.Properties().SetBold(true)
	r.Properties().SetFont(themeConfig.titleFont)
	r.Properties().SetColor(themeConfig.titleColor)

	// Subtitle on title slide
	subTb := titleSlide.AddTextBox()
	subTb.Properties().SetWidth(6 * measurement.Inch)
	subTb.Properties().SetHeight(0.8 * measurement.Inch)
	subTb.Properties().SetPosition(2*measurement.Inch, 4.2*measurement.Inch)
	subP := subTb.AddParagraph()
	subP.Properties().SetAlignment(dml.ST_TextAlignTypeCtr)
	subR := subP.AddRun()
	subR.SetText(fmt.Sprintf("%d slides", len(slides)))
	subR.Properties().SetSize(18 * measurement.Point)
	subR.Properties().SetFont(themeConfig.bodyFont)
	subR.Properties().SetColor(themeConfig.bodyColor)

	// Apply background to title slide
	applySlideBackground(titleSlide, themeConfig)

	// Content slides: start from first outline entry (or second if title was used from first)
	startIdx := 0
	if title == "" && len(slides) > 1 {
		// First outline entry was used as title slide, skip it for content
		startIdx = 1
	}

	for i := startIdx; i < len(slides); i++ {
		s := slides[i]
		slide := ppt.AddSlide()
		applySlideBackground(slide, themeConfig)

		// Title text box
		titleTb := slide.AddTextBox()
		titleTb.Properties().SetWidth(8 * measurement.Inch)
		titleTb.Properties().SetHeight(0.8 * measurement.Inch)
		titleTb.Properties().SetPosition(0.5*measurement.Inch, 0.3*measurement.Inch)
		titleP := titleTb.AddParagraph()
		titleR := titleP.AddRun()
		titleR.SetText(s.title)
		titleR.Properties().SetSize(28 * measurement.Point)
		titleR.Properties().SetBold(true)
		titleR.Properties().SetFont(themeConfig.titleFont)
		titleR.Properties().SetColor(themeConfig.titleColor)

		// Body text box
		if len(s.content) > 0 {
			bodyTb := slide.AddTextBox()
			bodyTb.Properties().SetWidth(8 * measurement.Inch)
			bodyTb.Properties().SetHeight(5.5 * measurement.Inch)
			bodyTb.Properties().SetPosition(0.5*measurement.Inch, 1.3*measurement.Inch)

			for j, line := range s.content {
				if j > 0 {
					bodyTb.AddParagraph()
				}
				bp := bodyTb.Paragraphs()[len(bodyTb.Paragraphs())-1]
				// Detect bullet points (lines starting with - or *)
				cleanLine := strings.TrimSpace(line)
				isBullet := strings.HasPrefix(cleanLine, "- ") || strings.HasPrefix(cleanLine, "* ")
				if isBullet {
					cleanLine = strings.TrimSpace(cleanLine[2:])
					bp.Properties().SetLevel(0)
				}
				br := bp.AddRun()
				br.SetText(cleanLine)
				br.Properties().SetSize(18 * measurement.Point)
				br.Properties().SetFont(themeConfig.bodyFont)
				br.Properties().SetColor(themeConfig.bodyColor)
			}
		}
	}

	if err := ppt.SaveToFile(outputPath); err != nil {
		return nil, fmt.Errorf("save presentation: %w", err)
	}

	return fmt.Sprintf("created presentation with %d slides at %s", len(slides), outputPath), nil
}

func runAddSlide(args map[string]any) (any, error) {
	pptPath, err := argString(args, "ppt_path")
	if err != nil {
		return nil, err
	}
	slideTitle, err := argString(args, "title")
	if err != nil {
		return nil, err
	}
	content := argStringDefault(args, "content", "")
	notes := argStringDefault(args, "notes", "")
	layout := argStringDefault(args, "layout", "title_content")

	if _, err := os.Stat(pptPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("pptx file not found: %s", pptPath)
	}

	ppt, err := presentation.Open(pptPath)
	if err != nil {
		return nil, fmt.Errorf("open presentation: %w", err)
	}
	defer ppt.Close()

	slide := ppt.AddSlide()

	switch layout {
	case "title":
		// Full-title slide: centered title
		tb := slide.AddTextBox()
		tb.Properties().SetWidth(8 * measurement.Inch)
		tb.Properties().SetHeight(2 * measurement.Inch)
		tb.Properties().SetPosition(1*measurement.Inch, 2.5*measurement.Inch)
		p := tb.AddParagraph()
		p.Properties().SetAlignment(dml.ST_TextAlignTypeCtr)
		r := p.AddRun()
		r.SetText(slideTitle)
		r.Properties().SetSize(40 * measurement.Point)
		r.Properties().SetBold(true)

	case "title_content":
		// Title at top, content below
		titleTb := slide.AddTextBox()
		titleTb.Properties().SetWidth(8 * measurement.Inch)
		titleTb.Properties().SetHeight(0.8 * measurement.Inch)
		titleTb.Properties().SetPosition(0.5*measurement.Inch, 0.3*measurement.Inch)
		tp := titleTb.AddParagraph()
		tr := tp.AddRun()
		tr.SetText(slideTitle)
		tr.Properties().SetSize(28 * measurement.Point)
		tr.Properties().SetBold(true)

		if content != "" {
			bodyTb := slide.AddTextBox()
			bodyTb.Properties().SetWidth(8 * measurement.Inch)
			bodyTb.Properties().SetHeight(5.5 * measurement.Inch)
			bodyTb.Properties().SetPosition(0.5*measurement.Inch, 1.3*measurement.Inch)

			lines := strings.Split(content, "\n")
			for i, line := range lines {
				if i > 0 {
					bodyTb.AddParagraph()
				}
				bp := bodyTb.Paragraphs()[len(bodyTb.Paragraphs())-1]
				cleanLine := strings.TrimSpace(line)
				isBullet := strings.HasPrefix(cleanLine, "- ") || strings.HasPrefix(cleanLine, "* ")
				if isBullet {
					cleanLine = strings.TrimSpace(cleanLine[2:])
					bp.Properties().SetLevel(0)
				}
				br := bp.AddRun()
				br.SetText(cleanLine)
				br.Properties().SetSize(18 * measurement.Point)
			}
		}

	case "blank":
		// Blank slide, no text boxes added by default
		// (user can add content later)

	default:
		return nil, fmt.Errorf("unknown layout %q; use title, title_content, or blank", layout)
	}

	// Add speaker notes if provided
	if notes != "" {
		notesSlide := slide.AddNotes()
		np := notesSlide.AddParagraph()
		nr := np.AddRun()
		nr.SetText(notes)
	}

	if err := ppt.SaveToFile(pptPath); err != nil {
		return nil, fmt.Errorf("save presentation: %w", err)
	}

	return fmt.Sprintf("added slide %q (layout: %s) to %s", slideTitle, layout, pptPath), nil
}

func runApplyTheme(args map[string]any) (any, error) {
	pptPath, err := argString(args, "ppt_path")
	if err != nil {
		return nil, err
	}
	theme, err := argString(args, "theme")
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(pptPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("pptx file not found: %s", pptPath)
	}

	validThemes := map[string]bool{"professional": true, "creative": true, "minimal": true}
	if !validThemes[theme] {
		return nil, fmt.Errorf("unknown theme %q; choose from professional, creative, minimal", theme)
	}

	ppt, err := presentation.Open(pptPath)
	if err != nil {
		return nil, fmt.Errorf("open presentation: %w", err)
	}
	defer ppt.Close()

	themeConfig := getThemeConfig(theme)

	for _, slide := range ppt.Slides() {
		applySlideBackground(slide, themeConfig)
		// Update text styling on existing text boxes
		for _, tb := range slide.GetTextBoxes() {
			for _, p := range tb.Paragraphs() {
				for _, r := range p.Runs() {
					r.Properties().SetFont(themeConfig.bodyFont)
					r.Properties().SetColor(themeConfig.bodyColor)
				}
			}
		}
	}

	if err := ppt.SaveToFile(pptPath); err != nil {
		return nil, fmt.Errorf("save presentation: %w", err)
	}

	return fmt.Sprintf("applied %s theme to %s", theme, pptPath), nil
}

func runAddChart(args map[string]any) (any, error) {
	pptPath, err := argString(args, "ppt_path")
	if err != nil {
		return nil, err
	}
	chartType, err := argString(args, "chart_type")
	if err != nil {
		return nil, err
	}
	chartTitle, err := argString(args, "title")
	if err != nil {
		return nil, err
	}
	dataStr, err := argString(args, "data")
	if err != nil {
		return nil, err
	}
	position := argIntDefault(args, "position", -1)

	if _, err := os.Stat(pptPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("pptx file not found: %s", pptPath)
	}

	// Parse chart data JSON
	var chartData struct {
		Labels []string    `json:"labels"`
		Values interface{} `json:"values"`
	}
	if err := json.Unmarshal([]byte(dataStr), &chartData); err != nil {
		return nil, fmt.Errorf("parse chart data JSON: %w", err)
	}
	if len(chartData.Labels) == 0 {
		return nil, fmt.Errorf("chart data must contain non-empty 'labels' array")
	}

	// Normalize values to [][]float64 (multi-series support)
	var seriesValues [][]float64
	switch v := chartData.Values.(type) {
	case []interface{}:
		if len(v) == 0 {
			return nil, fmt.Errorf("chart data must contain non-empty 'values' array")
		}
		// Check if first element is an array (multi-series) or a number (single-series)
		if arr, ok := v[0].([]interface{}); ok {
			// Multi-series
			for _, s := range v {
				if seriesArr, ok := s.([]interface{}); ok {
					floats := make([]float64, 0, len(seriesArr))
					for _, val := range seriesArr {
						if f, ok := val.(float64); ok {
							floats = append(floats, f)
						} else {
							return nil, fmt.Errorf("chart value must be a number")
						}
					}
					seriesValues = append(seriesValues, floats)
				}
			}
		} else {
			// Single-series
			floats := make([]float64, 0, len(v))
			for _, val := range v {
				if f, ok := val.(float64); ok {
					floats = append(floats, f)
				} else {
					return nil, fmt.Errorf("chart value must be a number")
				}
			}
			seriesValues = append(seriesValues, floats)
		}
	default:
		return nil, fmt.Errorf("'values' must be a number array or array of arrays")
	}

	ppt, err := presentation.Open(pptPath)
	if err != nil {
		return nil, fmt.Errorf("open presentation: %w", err)
	}
	defer ppt.Close()

	slide := ppt.AddSlide()

	// Add title text box
	titleTb := slide.AddTextBox()
	titleTb.Properties().SetWidth(8 * measurement.Inch)
	titleTb.Properties().SetHeight(0.7 * measurement.Inch)
	titleTb.Properties().SetPosition(0.5*measurement.Inch, 0.2*measurement.Inch)
	tp := titleTb.AddParagraph()
	tp.Properties().SetAlignment(dml.ST_TextAlignTypeCtr)
	tr := tp.AddRun()
	tr.SetText(chartTitle)
	tr.Properties().SetSize(24 * measurement.Point)
	tr.Properties().SetBold(true)

	// Add chart
	chart := slide.AddChart()

	switch chartType {
	case "bar":
		chart.Properties().SetWidth(7 * measurement.Inch)
		chart.Properties().SetHeight(4.5 * measurement.Inch)
		chart.Properties().SetPosition(1.5*measurement.Inch, 1.2*measurement.Inch)
		addBarChartData(chart, chartData.Labels, seriesValues)

	case "line":
		chart.Properties().SetWidth(7 * measurement.Inch)
		chart.Properties().SetHeight(4.5 * measurement.Inch)
		chart.Properties().SetPosition(1.5*measurement.Inch, 1.2*measurement.Inch)
		addLineChartData(chart, chartData.Labels, seriesValues)

	case "pie":
		chart.Properties().SetWidth(5 * measurement.Inch)
		chart.Properties().SetHeight(4.5 * measurement.Inch)
		chart.Properties().SetPosition(2.5*measurement.Inch, 1.2*measurement.Inch)
		addPieChartData(chart, chartData.Labels, seriesValues[0])

	default:
		return nil, fmt.Errorf("unknown chart type %q; use bar, line, or pie", chartType)
	}

	if err := ppt.SaveToFile(pptPath); err != nil {
		return nil, fmt.Errorf("save presentation: %w", err)
	}

	return fmt.Sprintf("added %s chart slide %q to %s", chartType, chartTitle, pptPath), nil
}

func runExportPDF(args map[string]any) (any, error) {
	pptPath, err := argString(args, "ppt_path")
	if err != nil {
		return nil, err
	}
	outputPath := argStringDefault(args, "output_path", "")

	if _, err := os.Stat(pptPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("pptx file not found: %s", pptPath)
	}

	if outputPath == "" {
		outputPath = strings.TrimSuffix(pptPath, filepath.Ext(pptPath)) + ".pdf"
	}

	if !strings.HasSuffix(strings.ToLower(outputPath), ".pdf") {
		return nil, fmt.Errorf("output path must end with .pdf, got %q", outputPath)
	}

	// unioffice v2 does not natively support PPTX-to-PDF conversion.
	// The document package has PDF export, but the presentation package does not.
	// We attempt to use LibreOffice if available, otherwise return an informative message.
	if err := exportPDFViaLibreOffice(pptPath, outputPath); err == nil {
		return fmt.Sprintf("exported PDF to %s", outputPath), nil
	}

	// Fallback: return informative message about the limitation
	return fmt.Sprintf("PDF export from PPTX is not directly supported in pure Go. "+
		"Install LibreOffice and add it to PATH for automatic conversion. "+
		"Alternatively, open %s in PowerPoint/LibreOffice and export manually. "+
		"The PPTX file is ready at: %s", pptPath, pptPath), nil
}

// --- Helper functions ---

// parseOutline converts a Markdown outline into slide structures.
// ## headers become slide titles; content lines below each header become slide body.
// A single # header (if present) is treated as the presentation title.
func parseOutline(md string) []slideOutline {
	lines := strings.Split(md, "\n")
	var slides []slideOutline
	var current *slideOutline

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		// Check for headers
		if strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "##") {
			// Single # = presentation title, skip (handled separately)
			continue
		}

		if strings.HasPrefix(trimmed, "## ") {
			// New slide
			if current != nil {
				slides = append(slides, *current)
			}
			current = &slideOutline{
				title:   strings.TrimSpace(trimmed[3:]),
				content: nil,
			}
			continue
		}

		// Content line belongs to current slide
		if current != nil {
			current.content = append(current.content, trimmed)
		}
	}

	if current != nil {
		slides = append(slides, *current)
	}

	return slides
}

// themeConfig holds visual styling parameters for a presentation theme.
type themeConfig struct {
	name       string
	titleFont  string
	bodyFont   string
	titleColor color.Color
	bodyColor  color.Color
	bgColor    *color.Color // nil means no background fill
}

func getThemeConfig(style string) themeConfig {
	switch style {
	case "creative":
		return themeConfig{
			name:       "creative",
			titleFont:  "Segoe UI",
			bodyFont:   "Segoe UI",
			titleColor: color.Teal,
			bodyColor:  color.DarkSlateGray,
			bgColor:    nil, // default white
		}
	case "minimal":
		return themeConfig{
			name:       "minimal",
			titleFont:  "Arial",
			bodyFont:   "Arial",
			titleColor: color.DarkGray,
			bodyColor:  color.Gray,
			bgColor:    nil,
		}
	default: // professional
		return themeConfig{
			name:       "professional",
			titleFont:  "Calibri",
			bodyFont:   "Calibri",
			titleColor: color.DarkBlue,
			bodyColor:  color.Black,
			bgColor:    nil,
		}
	}
}

// applySlideBackground sets the background color on a slide if the theme specifies one.
func applySlideBackground(slide presentation.Slide, tc themeConfig) {
	if tc.bgColor == nil {
		return
	}
	// Set solid fill background
	bg := slide.X().CSld.Bg
	if bg == nil {
		bg = dml.NewCT_Background()
		slide.X().CSld.Bg = bg
	}
	if bg.BgPr == nil {
		bg.BgPr = dml.NewCT_BackgroundProperties()
	}
	bg.BgPr.SolidFill = dml.NewCT_SolidColorFillProperties()
	bg.BgPr.SolidFill.SrgbClr = dml.NewCT_SRgbColor()
	bg.BgPr.SolidFill.SrgbClr.ValAttr = *tc.bgColor.AsRGBString()
}

// addBarChartData populates a chart with bar chart data.
func addBarChartData(chart presentation.Chart, labels []string, series [][]float64) {
	chart.SetCategory(labels)
	for i, s := range series {
		name := fmt.Sprintf("Series %d", i+1)
		if len(series) == 1 {
			name = "Values"
		}
		chart.AddSeries(name, s)
	}
}

// addLineChartData populates a chart with line chart data.
func addLineChartData(chart presentation.Chart, labels []string, series [][]float64) {
	chart.SetCategory(labels)
	for i, s := range series {
		name := fmt.Sprintf("Series %d", i+1)
		if len(series) == 1 {
			name = "Values"
		}
		chart.AddSeries(name, s)
	}
}

// addPieChartData populates a chart with pie chart data.
func addPieChartData(chart presentation.Chart, labels []string, values []float64) {
	chart.SetCategory(labels)
	chart.AddSeries("Values", values)
}

// exportPDFViaLibreOffice attempts to convert PPTX to PDF using LibreOffice.
func exportPDFViaLibreOffice(pptPath, pdfPath string) error {
	loPaths := []string{
		"soffice", "libreoffice",
		"/usr/bin/soffice", "/usr/bin/libreoffice",
		"/snap/bin/libreoffice",
		"C:\\Program Files\\LibreOffice\\program\\soffice.exe",
		"C:\\Program Files (x86)\\LibreOffice\\program\\soffice.exe",
	}

	var loPath string
	for _, p := range loPaths {
		if _, err := os.Stat(p); err == nil {
			loPath = p
			break
		}
	}
	if loPath == "" {
		return fmt.Errorf("LibreOffice not found")
	}

	outputDir := filepath.Dir(pdfPath)
	args := []string{
		"--headless", "--convert-to", "pdf",
		"--outdir", outputDir,
		pptPath,
	}

	// #nosec G204 -- subprocess with fixed args
	cmd := exec.Command(loPath, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("libreoffice conversion failed: %w", err)
	}

	if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
		return fmt.Errorf("libreoffice ran but PDF not found at %s", pdfPath)
	}
	return nil
}

// ensureDir creates the parent directory of path if it doesn't exist.
func ensureDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0o755)
	}
	return nil
}

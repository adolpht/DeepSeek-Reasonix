package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/wcharczuk/go-chart/v2"
)

// chartSheetTool renders a chart from sheet data and returns a PNG image
// (base64) plus a caption. Read-only: it never mutates the source file.
//
// The image is returned as an MCP image content block; the desktop frontend
// renders it via the existing mediaTokenStore path.
var chartSheetTool = toolDef{
	name: "chart_sheet",
	description: "Render a chart from xlsx/csv data and return a PNG image. " +
		"Supports bar / line / pie / scatter. Specify x column and one or more y columns. " +
		"Returns a base64 PNG plus a caption.",
	readOnly: true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":  map[string]any{"type": "string", "description": "Absolute path to the file"},
			"sheet": map[string]any{"type": "string", "description": "Sheet name (xlsx only)"},
			"range": map[string]any{"type": "string", "description": "Optional range to bound the data (e.g. A1:B10)"},
			"x":     map[string]any{"type": "string", "description": "Column name (header) for the X axis"},
			"y":     map[string]any{"type": "array", "description": "Column names for Y values"},
			"type":  map[string]any{"type": "string", "enum": []string{"bar", "line", "pie", "scatter"}, "description": "Chart type (default bar)"},
			"title": map[string]any{"type": "string", "description": "Chart title (optional)"},
		},
		"required": []string{"path", "x", "y"},
	},
	run: runChartSheet,
}

func runChartSheet(args map[string]any) (any, error) {
	path, err := argString(args, "path")
	if err != nil {
		return nil, err
	}
	sheet := argStringDefault(args, "sheet", "")
	xCol, err := argString(args, "x")
	if err != nil {
		return nil, err
	}
	yCols, err := toStringSlice(args["y"])
	if err != nil {
		return nil, fmt.Errorf("y: %w", err)
	}
	if len(yCols) == 0 {
		return nil, fmt.Errorf("y must be non-empty")
	}
	chartType := argStringDefault(args, "type", "bar")
	title := argStringDefault(args, "title", "")

	header, records, err := loadRecords(path, sheet)
	_ = header
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("no data rows in file")
	}

	// Validate columns exist.
	if !columnExists(records, xCol) {
		return nil, fmt.Errorf("x column %q not found in data", xCol)
	}
	for _, y := range yCols {
		if !columnExists(records, y) {
			return nil, fmt.Errorf("y column %q not found in data", y)
		}
	}

	var png []byte
	switch chartType {
	case "pie":
		png, err = renderPie(records, xCol, yCols[0], title)
	case "line":
		png, err = renderLine(records, xCol, yCols, title)
	case "scatter":
		png, err = renderScatter(records, xCol, yCols, title)
	default: // bar
		png, err = renderBar(records, xCol, yCols, title)
	}
	if err != nil {
		return nil, fmt.Errorf("render chart: %w", err)
	}

	b64 := base64.StdEncoding.EncodeToString(png)
	caption := fmt.Sprintf("chart type=%s, x=%s, y=%v, points=%d", chartType, xCol, yCols, len(records))
	return imageResult(b64, caption), nil
}

func columnExists(records []map[string]string, col string) bool {
	if len(records) == 0 {
		return false
	}
	_, ok := records[0][col]
	return ok
}

func parseFloats(records []map[string]string, col string) []float64 {
	out := make([]float64, 0, len(records))
	for _, r := range records {
		v := strings.TrimSpace(r[col])
		if v == "" {
			continue
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			continue
		}
		out = append(out, f)
	}
	return out
}

func xLabels(records []map[string]string, col string) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r[col])
	}
	return out
}

func renderBar(records []map[string]string, xCol string, yCols []string, title string) ([]byte, error) {
	labels := xLabels(records, xCol)
	bars := make([]chart.Value, 0, len(labels))
	yvals := parseFloats(records, yCols[0])
	n := len(labels)
	if len(yvals) < n {
		n = len(yvals)
	}
	for i := 0; i < n; i++ {
		bars = append(bars, chart.Value{Label: labels[i], Value: yvals[i]})
	}
	graph := chart.BarChart{
		Title:  title,
		Width:  800,
		Height: 400,
		Bars:   bars,
	}
	var buf bytes.Buffer
	if err := graph.Render(chart.PNG, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderLine(records []map[string]string, xCol string, yCols []string, title string) ([]byte, error) {
	labels := xLabels(records, xCol)
	var series []chart.Series
	for _, y := range yCols {
		yvals := parseFloats(records, y)
		xvals := make([]float64, 0, len(yvals))
		for i := range yvals {
			xvals = append(xvals, float64(i+1))
		}
		series = append(series, chart.ContinuousSeries{
			Name:    y,
			XValues: xvals,
			YValues: yvals,
		})
	}
	graph := chart.Chart{
		Title:  title,
		Width:  800,
		Height: 400,
		XAxis: chart.XAxis{
			Name: xCol,
			Style: chart.Style{
				TextRotationDegrees: 45,
			},
		},
		Series: series,
	}
	_ = labels
	var buf bytes.Buffer
	if err := graph.Render(chart.PNG, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderScatter(records []map[string]string, xCol string, yCols []string, title string) ([]byte, error) {
	var series []chart.Series
	for _, y := range yCols {
		xv := make([]float64, 0, len(records))
		yv := make([]float64, 0, len(records))
		for _, r := range records {
			xf, err := strconv.ParseFloat(strings.TrimSpace(r[xCol]), 64)
			if err != nil {
				continue
			}
			yf, err := strconv.ParseFloat(strings.TrimSpace(r[y]), 64)
			if err != nil {
				continue
			}
			xv = append(xv, xf)
			yv = append(yv, yf)
		}
		series = append(series, chart.ContinuousSeries{
			Name:    y,
			XValues: xv,
			YValues: yv,
		})
	}
	graph := chart.Chart{
		Title:  title,
		Width:  800,
		Height: 400,
		Series: series,
	}
	var buf bytes.Buffer
	if err := graph.Render(chart.PNG, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderPie(records []map[string]string, xCol, yCol, title string) ([]byte, error) {
	vals := parseFloats(records, yCol)
	labels := xLabels(records, xCol)
	pie := make([]chart.Value, 0, len(vals))
	n := len(labels)
	if len(vals) < n {
		n = len(vals)
	}
	for i := 0; i < n; i++ {
		pie = append(pie, chart.Value{Label: labels[i], Value: vals[i]})
	}
	graph := chart.PieChart{
		Title:  title,
		Width:  600,
		Height: 600,
		Values: pie,
	}
	var buf bytes.Buffer
	if err := graph.Render(chart.PNG, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

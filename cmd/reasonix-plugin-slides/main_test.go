package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// --- Tool registration tests ---

func TestToolListRegistration(t *testing.T) {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.name)
	}
	expected := []string{"create_ppt", "add_slide", "apply_theme", "add_chart", "export_pdf"}
	for _, e := range expected {
		if !containsStr(names, e) {
			t.Errorf("missing tool %q in %v", e, names)
		}
	}
	if len(tools) != 5 {
		t.Errorf("expected 5 tools, got %d", len(tools))
	}
}

func TestToolListHasSchemas(t *testing.T) {
	list := toolList()
	if len(list) != 5 {
		t.Fatalf("expected 5 tool entries, got %d", len(list))
	}
	for _, entry := range list {
		name, _ := entry["name"].(string)
		if name == "" {
			t.Error("tool entry missing name")
		}
		if _, ok := entry["inputSchema"]; !ok {
			t.Errorf("tool %q missing inputSchema", name)
		}
		if _, ok := entry["description"]; !ok {
			t.Errorf("tool %q missing description", name)
		}
		if ann, ok := entry["annotations"].(map[string]any); ok {
			if _, ok := ann["readOnlyHint"]; !ok {
				t.Errorf("tool %q annotations missing readOnlyHint", name)
			}
		} else {
			t.Errorf("tool %q missing annotations", name)
		}
	}
}

// --- Outline parser tests ---

func TestParseOutlineBasic(t *testing.T) {
	md := `# My Presentation

## Introduction
Welcome to the talk
- Key point one
- Key point two

## Features
Feature A is great
Feature B is also good

## Summary
That's all folks`
	slides := parseOutline(md)
	if len(slides) != 3 {
		t.Fatalf("expected 3 slides, got %d", len(slides))
	}
	if slides[0].title != "Introduction" {
		t.Errorf("first slide title = %q, want 'Introduction'", slides[0].title)
	}
	if len(slides[0].content) != 3 {
		t.Errorf("first slide content lines = %d, want 3", len(slides[0].content))
	}
	if slides[1].title != "Features" {
		t.Errorf("second slide title = %q, want 'Features'", slides[1].title)
	}
	if slides[2].title != "Summary" {
		t.Errorf("third slide title = %q, want 'Summary'", slides[2].title)
	}
}

func TestParseOutlineEmpty(t *testing.T) {
	slides := parseOutline("")
	if len(slides) != 0 {
		t.Errorf("expected 0 slides for empty input, got %d", len(slides))
	}
}

func TestParseOutlineNoH2(t *testing.T) {
	md := `# Title Only
Some text without any slide headers`
	slides := parseOutline(md)
	if len(slides) != 0 {
		t.Errorf("expected 0 slides (no ## headers), got %d", len(slides))
	}
}

func TestParseOutlineSingleSlide(t *testing.T) {
	md := `## Solo Slide
Just one line`
	slides := parseOutline(md)
	if len(slides) != 1 {
		t.Fatalf("expected 1 slide, got %d", len(slides))
	}
	if slides[0].title != "Solo Slide" {
		t.Errorf("title = %q, want 'Solo Slide'", slides[0].title)
	}
	if len(slides[0].content) != 1 || slides[0].content[0] != "Just one line" {
		t.Errorf("content = %v, want ['Just one line']", slides[0].content)
	}
}

// --- Theme config tests ---

func TestGetThemeConfigProfessional(t *testing.T) {
	tc := getThemeConfig("professional")
	if tc.name != "professional" {
		t.Errorf("name = %q, want 'professional'", tc.name)
	}
	if tc.titleFont != "Calibri" {
		t.Errorf("titleFont = %q, want 'Calibri'", tc.titleFont)
	}
}

func TestGetThemeConfigCreative(t *testing.T) {
	tc := getThemeConfig("creative")
	if tc.name != "creative" {
		t.Errorf("name = %q, want 'creative'", tc.name)
	}
	if tc.titleFont != "Segoe UI" {
		t.Errorf("titleFont = %q, want 'Segoe UI'", tc.titleFont)
	}
}

func TestGetThemeConfigMinimal(t *testing.T) {
	tc := getThemeConfig("minimal")
	if tc.name != "minimal" {
		t.Errorf("name = %q, want 'minimal'", tc.name)
	}
	if tc.titleFont != "Arial" {
		t.Errorf("titleFont = %q, want 'Arial'", tc.titleFont)
	}
}

func TestGetThemeConfigDefault(t *testing.T) {
	tc := getThemeConfig("unknown")
	if tc.name != "professional" {
		t.Errorf("default name = %q, want 'professional'", tc.name)
	}
}

// --- Arg helper tests ---

func TestArgString(t *testing.T) {
	val, err := argString(map[string]any{"key": "value"}, "key")
	if err != nil || val != "value" {
		t.Errorf("got %q, %v; want 'value', nil", val, err)
	}
}

func TestArgStringMissing(t *testing.T) {
	_, err := argString(map[string]any{}, "key")
	if err == nil {
		t.Error("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "missing required argument") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestArgStringWrongType(t *testing.T) {
	_, err := argString(map[string]any{"key": 42}, "key")
	if err == nil {
		t.Error("expected error for wrong type")
	}
	if !strings.Contains(err.Error(), "must be a string") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestArgStringDefault(t *testing.T) {
	val := argStringDefault(map[string]any{}, "key", "fallback")
	if val != "fallback" {
		t.Errorf("got %q, want 'fallback'", val)
	}
	val = argStringDefault(map[string]any{"key": "actual"}, "key", "fallback")
	if val != "actual" {
		t.Errorf("got %q, want 'actual'", val)
	}
	val = argStringDefault(map[string]any{"key": ""}, "key", "fallback")
	if val != "fallback" {
		t.Errorf("got %q, want 'fallback' for empty string", val)
	}
}

func TestArgIntDefault(t *testing.T) {
	val := argIntDefault(map[string]any{}, "key", 99)
	if val != 99 {
		t.Errorf("got %d, want 99", val)
	}
	val = argIntDefault(map[string]any{"key": float64(7)}, "key", 99)
	if val != 7 {
		t.Errorf("got %d, want 7", val)
	}
	val = argIntDefault(map[string]any{"key": 3}, "key", 99)
	if val != 3 {
		t.Errorf("got %d, want 3", val)
	}
}

// --- Tool run function tests ---

func TestRunCreatePPTMissingOutline(t *testing.T) {
	dir := t.TempDir()
	outputPath := dir + "\\test.pptx"
	_, err := runCreatePPT(map[string]any{
		"output_path": outputPath,
	})
	if err == nil {
		t.Fatal("expected error for missing outline")
	}
	if !strings.Contains(err.Error(), "outline") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCreatePPTMissingOutputPath(t *testing.T) {
	_, err := runCreatePPT(map[string]any{
		"outline": "## Test\nContent",
	})
	if err == nil {
		t.Fatal("expected error for missing output_path")
	}
	if !strings.Contains(err.Error(), "output_path") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCreatePPTBadExtension(t *testing.T) {
	_, err := runCreatePPT(map[string]any{
		"outline":     "## Test\nContent",
		"output_path": "C:\\tmp\\test.docx",
	})
	if err == nil {
		t.Fatal("expected error for non-.pptx extension")
	}
	if !strings.Contains(err.Error(), ".pptx") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCreatePPTNoSlides(t *testing.T) {
	dir := t.TempDir()
	outputPath := dir + "\\test.pptx"
	_, err := runCreatePPT(map[string]any{
		"outline":     "Just some text without headers",
		"output_path": outputPath,
	})
	if err == nil {
		t.Fatal("expected error for outline with no slides")
	}
	if !strings.Contains(err.Error(), "no slides") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCreatePPTBasic(t *testing.T) {
	dir := t.TempDir()
	outputPath := dir + "\\test.pptx"
	res, err := runCreatePPT(map[string]any{
		"outline":     "## Introduction\nWelcome\n## Summary\nDone",
		"output_path": outputPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}
	if !strings.Contains(s, "created presentation") {
		t.Errorf("unexpected result: %s", s)
	}
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Errorf("output file not created at %s", outputPath)
	}
}

func TestRunCreatePPTWithStyle(t *testing.T) {
	dir := t.TempDir()
	outputPath := dir + "\\styled.pptx"
	res, err := runCreatePPT(map[string]any{
		"outline":     "## Slide One\nContent here",
		"output_path": outputPath,
		"style":       "creative",
	})
	if err != nil {
		t.Fatalf("create_ppt with style failed: %v", err)
	}
	s, _ := res.(string)
	if !strings.Contains(s, "created presentation") {
		t.Errorf("unexpected result: %s", s)
	}
}

func TestRunAddSlideMissingPptPath(t *testing.T) {
	_, err := runAddSlide(map[string]any{
		"title": "Test Slide",
	})
	if err == nil {
		t.Fatal("expected error for missing ppt_path")
	}
	if !strings.Contains(err.Error(), "ppt_path") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunAddSlideMissingTitle(t *testing.T) {
	_, err := runAddSlide(map[string]any{
		"ppt_path": "C:\\nonexistent.pptx",
	})
	if err == nil {
		t.Fatal("expected error for missing title")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunAddSlideFileNotFound(t *testing.T) {
	_, err := runAddSlide(map[string]any{
		"ppt_path": "C:\\nonexistent_file.pptx",
		"title":    "Test Slide",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunAddSlideBasic(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\test.pptx"

	// First create a presentation.
	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	// Then add a slide.
	res, err := runAddSlide(map[string]any{
		"ppt_path": pptPath,
		"title":    "New Slide",
		"content":  "Line one\nLine two",
		"layout":   "title_content",
	})
	if err != nil {
		t.Fatalf("add_slide failed: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}
	if !strings.Contains(s, "added slide") {
		t.Errorf("unexpected result: %s", s)
	}
}

func TestRunAddSlideWithNotes(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\test.pptx"

	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	res, err := runAddSlide(map[string]any{
		"ppt_path": pptPath,
		"title":    "Slide with Notes",
		"notes":    "Remember to explain this",
	})
	if err != nil {
		t.Fatalf("add_slide with notes failed: %v", err)
	}
	s, _ := res.(string)
	if !strings.Contains(s, "added slide") {
		t.Errorf("unexpected result: %s", s)
	}
}

func TestRunAddSlideInvalidLayout(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\test.pptx"

	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	_, err = runAddSlide(map[string]any{
		"ppt_path": pptPath,
		"title":    "Bad Layout",
		"layout":   "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for invalid layout")
	}
	if !strings.Contains(err.Error(), "unknown layout") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunApplyThemeMissingArgs(t *testing.T) {
	_, err := runApplyTheme(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

func TestRunApplyThemeInvalidTheme(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\test.pptx"

	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	_, err = runApplyTheme(map[string]any{
		"ppt_path": pptPath,
		"theme":    "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for invalid theme")
	}
	if !strings.Contains(err.Error(), "unknown theme") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunApplyThemeFileNotFound(t *testing.T) {
	_, err := runApplyTheme(map[string]any{
		"ppt_path": "C:\\nonexistent.pptx",
		"theme":    "professional",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunApplyThemeBasic(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\test.pptx"

	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	res, err := runApplyTheme(map[string]any{
		"ppt_path": pptPath,
		"theme":    "creative",
	})
	if err != nil {
		t.Fatalf("apply_theme failed: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}
	if !strings.Contains(s, "applied creative theme") {
		t.Errorf("unexpected result: %s", s)
	}
}

func TestRunAddChartMissingArgs(t *testing.T) {
	_, err := runAddChart(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

func TestRunAddChartFileNotFound(t *testing.T) {
	_, err := runAddChart(map[string]any{
		"ppt_path":   "C:\\nonexistent.pptx",
		"chart_type": "bar",
		"title":      "Test Chart",
		"data":       `{"labels":["A","B"],"values":[1,2]}`,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunAddChartBadDataJSON(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\test.pptx"

	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	_, err = runAddChart(map[string]any{
		"ppt_path":   pptPath,
		"chart_type": "bar",
		"title":      "Bad Data",
		"data":       "not-json",
	})
	if err == nil {
		t.Fatal("expected error for bad JSON data")
	}
	if !strings.Contains(err.Error(), "parse chart data") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunAddChartEmptyLabels(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\test.pptx"

	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	_, err = runAddChart(map[string]any{
		"ppt_path":   pptPath,
		"chart_type": "bar",
		"title":      "Empty Labels",
		"data":       `{"labels":[],"values":[]}`,
	})
	if err == nil {
		t.Fatal("expected error for empty labels")
	}
	if !strings.Contains(err.Error(), "non-empty 'labels'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunAddChartBasic(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\test.pptx"

	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	res, err := runAddChart(map[string]any{
		"ppt_path":   pptPath,
		"chart_type": "bar",
		"title":      "Sales Chart",
		"data":       `{"labels":["Q1","Q2","Q3"],"values":[100,200,150]}`,
	})
	if err != nil {
		t.Fatalf("add_chart failed: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}
	if !strings.Contains(s, "added bar chart") {
		t.Errorf("unexpected result: %s", s)
	}
}

func TestRunAddChartInvalidType(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\test.pptx"

	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	_, err = runAddChart(map[string]any{
		"ppt_path":   pptPath,
		"chart_type": "scatter",
		"title":      "Bad Type",
		"data":       `{"labels":["A"],"values":[1]}`,
	})
	if err == nil {
		t.Fatal("expected error for invalid chart type")
	}
	if !strings.Contains(err.Error(), "unknown chart type") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunExportPDFMissingPptPath(t *testing.T) {
	_, err := runExportPDF(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing ppt_path")
	}
	if !strings.Contains(err.Error(), "ppt_path") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunExportPDFFileNotFound(t *testing.T) {
	_, err := runExportPDF(map[string]any{
		"ppt_path": "C:\\nonexistent.pptx",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunExportPDFBadOutputExtension(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\test.pptx"

	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	_, err = runExportPDF(map[string]any{
		"ppt_path":    pptPath,
		"output_path": dir + "\\test.docx",
	})
	if err == nil {
		t.Fatal("expected error for non-.pdf output extension")
	}
	if !strings.Contains(err.Error(), ".pdf") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunExportPDFBasic(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\test.pptx"

	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	res, err := runExportPDF(map[string]any{
		"ppt_path": pptPath,
	})
	if err != nil {
		t.Fatalf("export_pdf failed: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", res)
	}
	// Without LibreOffice, it returns an informative message.
	if !strings.Contains(s, "PDF") && !strings.Contains(s, "pptx") {
		t.Errorf("unexpected result: %s", s)
	}
}

// --- MCP protocol end-to-end tests ---

func TestMCPInitialize(t *testing.T) {
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "initialize", nil, 1)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize missing result: %v", resp)
	}
	if pv, _ := result["protocolVersion"].(string); pv != protocolVersion {
		t.Errorf("protocolVersion = %q, want %q", pv, protocolVersion)
	}
	serverInfo, _ := result["serverInfo"].(map[string]any)
	if name, _ := serverInfo["name"].(string); name != "reasonix-plugin-slides" {
		t.Errorf("serverInfo.name = %q, want 'reasonix-plugin-slides'", name)
	}

	wIn.Close()
	<-errCh
}

func TestMCPToolsList(t *testing.T) {
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "tools/list", nil, 1)
	toolsRaw, _ := resp["result"].(map[string]any)["tools"]
	toolsArr, ok := toolsRaw.([]any)
	if !ok || len(toolsArr) != 5 {
		t.Fatalf("tools/list expected 5 tools, got %v", toolsArr)
	}

	// Verify tool names.
	names := make([]string, 0, len(toolsArr))
	for _, tool := range toolsArr {
		if m, ok := tool.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	expected := []string{"create_ppt", "add_slide", "apply_theme", "add_chart", "export_pdf"}
	for _, e := range expected {
		if !containsStr(names, e) {
			t.Errorf("missing tool %q in tools/list response", e)
		}
	}

	wIn.Close()
	<-errCh
}

func TestMCPToolsCallUnknownTool(t *testing.T) {
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "tools/call", map[string]any{
		"name":      "nonexistent_tool",
		"arguments": map[string]any{},
	}, 1)

	rpcErr, _ := resp["error"].(map[string]any)
	if rpcErr == nil {
		t.Fatalf("expected error for unknown tool, got: %v", resp)
	}
	code, _ := rpcErr["code"].(float64)
	if int(code) != codeInvalidParams {
		t.Errorf("error code = %d, want %d", int(code), codeInvalidParams)
	}
	msg, _ := rpcErr["message"].(string)
	if !strings.Contains(msg, "unknown tool") {
		t.Errorf("error message = %q, want 'unknown tool'", msg)
	}

	wIn.Close()
	<-errCh
}

func TestMCPMethodNotFound(t *testing.T) {
	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "nonexistent/method", nil, 1)

	rpcErr, _ := resp["error"].(map[string]any)
	if rpcErr == nil {
		t.Fatalf("expected error for unknown method, got: %v", resp)
	}
	code, _ := rpcErr["code"].(float64)
	if int(code) != codeMethodNotFound {
		t.Errorf("error code = %d, want %d", int(code), codeMethodNotFound)
	}
	msg, _ := rpcErr["message"].(string)
	if !strings.Contains(msg, "method not found") {
		t.Errorf("error message = %q, want 'method not found'", msg)
	}

	wIn.Close()
	<-errCh
}

func TestMCPToolsCallCreatePPT(t *testing.T) {
	dir := t.TempDir()
	outputPath := dir + "\\mcp_test.pptx"

	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "tools/call", map[string]any{
		"name": "create_ppt",
		"arguments": map[string]any{
			"outline":     "## Test Slide\nHello world",
			"output_path": outputPath,
		},
	}, 1)

	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("tools/call create_ppt missing result: %v", resp)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("tools/call create_ppt missing content: %v", result)
	}
	textEntry, _ := content[0].(map[string]any)
	text, _ := textEntry["text"].(string)
	if !strings.Contains(text, "created presentation") {
		t.Errorf("unexpected text result: %s", text)
	}

	wIn.Close()
	<-errCh
}

func TestMCPToolsCallAddSlide(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\mcp_addslide.pptx"

	// Create a PPT first.
	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "tools/call", map[string]any{
		"name": "add_slide",
		"arguments": map[string]any{
			"ppt_path": pptPath,
			"title":    "Added Slide",
			"content":  "Some content",
		},
	}, 1)

	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("tools/call add_slide missing result: %v", resp)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("tools/call add_slide missing content: %v", result)
	}
	textEntry, _ := content[0].(map[string]any)
	text, _ := textEntry["text"].(string)
	if !strings.Contains(text, "added slide") {
		t.Errorf("unexpected text result: %s", text)
	}

	wIn.Close()
	<-errCh
}

func TestMCPToolsCallApplyTheme(t *testing.T) {
	dir := t.TempDir()
	pptPath := dir + "\\mcp_theme.pptx"

	_, err := runCreatePPT(map[string]any{
		"outline":     "## Intro\nHello",
		"output_path": pptPath,
	})
	if err != nil {
		t.Fatalf("create_ppt failed: %v", err)
	}

	rIn, wIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	resp := sendMCP(t, wIn, rOut, "tools/call", map[string]any{
		"name": "apply_theme",
		"arguments": map[string]any{
			"ppt_path": pptPath,
			"theme":    "minimal",
		},
	}, 1)

	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("tools/call apply_theme missing result: %v", resp)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("tools/call apply_theme missing content: %v", result)
	}
	textEntry, _ := content[0].(map[string]any)
	text, _ := textEntry["text"].(string)
	if !strings.Contains(text, "applied minimal theme") {
		t.Errorf("unexpected text result: %s", text)
	}

	wIn.Close()
	<-errCh
}

// --- JSON-RPC message parsing tests ---

func TestHandleLineEmpty(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	bw := bufio.NewWriter(w)
	// Empty/whitespace lines should be silently skipped.
	if err := handleLine([]byte("   \n"), bw); err != nil {
		t.Errorf("handleLine on empty input returned error: %v", err)
	}
	if err := handleLine([]byte(""), bw); err != nil {
		t.Errorf("handleLine on empty byte slice returned error: %v", err)
	}
	w.Close()
	r.Close()
}

func TestHandleLineUnparseable(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	bw := bufio.NewWriter(w)
	// Unparseable JSON should be silently skipped (not crash).
	if err := handleLine([]byte("not json\n"), bw); err != nil {
		t.Errorf("handleLine on unparseable input returned error: %v", err)
	}
	w.Close()
	r.Close()
}

func TestHandleLineNotification(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	bw := bufio.NewWriter(w)
	// JSON-RPC notification (no id) should be silently skipped.
	if err := handleLine([]byte(`{"jsonrpc":"2.0","method":"initialized"}` + "\n"), bw); err != nil {
		t.Errorf("handleLine on notification returned error: %v", err)
	}
	w.Close()
	r.Close()
}

func TestTextResult(t *testing.T) {
	tr := textResult("hello", false)
	content, _ := tr["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 content entry, got %d", len(content))
	}
	entry, _ := content[0].(map[string]any)
	text, _ := entry["text"].(string)
	if text != "hello" {
		t.Errorf("text = %q, want 'hello'", text)
	}
	isError, _ := tr["isError"].(bool)
	if isError {
		t.Error("isError should be false")
	}
}

func TestTextResultError(t *testing.T) {
	tr := textResult("something failed", true)
	isError, _ := tr["isError"].(bool)
	if !isError {
		t.Error("isError should be true")
	}
}

// --- ensureDir tests ---

func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	path := dir + "\\subdir\\nested\\file.txt"
	if err := ensureDir(path); err != nil {
		t.Fatalf("ensureDir failed: %v", err)
	}
	stat, err := os.Stat(dir + "\\subdir\\nested")
	if err != nil || !stat.IsDir() {
		t.Errorf("directory not created: %v", err)
	}
}

func TestEnsureDirEmptyPath(t *testing.T) {
	if err := ensureDir(""); err != nil {
		t.Errorf("ensureDir on empty path should not error: %v", err)
	}
	if err := ensureDir("file.txt"); err != nil {
		t.Errorf("ensureDir on relative file should not error: %v", err)
	}
}

// --- trimSpace tests ---

func TestTrimSpace(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"  hello  ", "hello"},
		{"\n\tfoo\r\n", "foo"},
		{"no-space", "no-space"},
		{"  ", ""},
	}
	for _, tc := range cases {
		got := string(trimSpace([]byte(tc.input)))
		if got != tc.want {
			t.Errorf("trimSpace(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --- helpers ---

func containsStr(arr []string, s string) bool {
	for _, x := range arr {
		if x == s {
			return true
		}
	}
	return false
}

func sendMCP(t *testing.T, wIn, rOut *os.File, method string, params any, id int) map[string]any {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if _, err := wIn.Write(b); err != nil {
		t.Fatal(err)
	}

	rOut.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer rOut.SetReadDeadline(time.Time{})
	br := bufio.NewReader(rOut)
	line, err := br.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nraw: %s", err, line)
	}
	return resp
}

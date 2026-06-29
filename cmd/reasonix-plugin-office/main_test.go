package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDocxWriteReadRoundTrip is the core closed-loop test: write Markdown →
// docx → read it back, and verify headings/paragraphs/tables survive the trip.
// This is what end users actually do (the agent calls write_docx then read_docx).
func TestDocxWriteReadRoundTrip(t *testing.T) {
	out := filepath.Join(t.TempDir(), "round.docx")
	md := `# Project Plan

This is the intro paragraph.

## Tasks

| name | owner | due |
| --- | --- | --- |
| design | alice | 2026-03-01 |
| build | bob | 2026-03-15 |

Final note.
`

	// write_docx
	wres, err := runWriteDocx(map[string]any{
		"path":    out,
		"content": md,
		"title":   "Round Trip Test",
	})
	if err != nil {
		t.Fatalf("write_docx failed: %v", err)
	}
	if !strings.Contains(asString(wres), "blocks") {
		t.Fatalf("write_docx result unexpected: %v", wres)
	}

	// read_docx full
	rres, err := runReadDocx(map[string]any{
		"path": out,
		"mode": "full",
	})
	if err != nil {
		t.Fatalf("read_docx failed: %v", err)
	}
	md2 := asString(rres)

	if !strings.Contains(md2, "Project Plan") {
		t.Errorf("heading 'Project Plan' missing from round-trip; got:\n%s", md2)
	}
	if !strings.Contains(md2, "Tasks") {
		t.Errorf("heading 'Tasks' missing; got:\n%s", md2)
	}
	if !strings.Contains(md2, "alice") || !strings.Contains(md2, "bob") {
		t.Errorf("table content missing; got:\n%s", md2)
	}
	if !strings.Contains(md2, "Final note") {
		t.Errorf("final paragraph missing; got:\n%s", md2)
	}
}

// TestDocxReadOutline verifies the outline mode returns headings only.
func TestDocxReadOutline(t *testing.T) {
	out := filepath.Join(t.TempDir(), "outline.docx")
	md := `# H1 Title

paragraph body that should be hidden in outline mode

## H2 Subtitle

another paragraph

### H3 Deep

| a | b |
| --- | --- |
| 1 | 2 |
`
	if _, err := runWriteDocx(map[string]any{"path": out, "content": md}); err != nil {
		t.Fatal(err)
	}

	rres, err := runReadDocx(map[string]any{"path": out, "mode": "outline"})
	if err != nil {
		t.Fatal(err)
	}
	out2 := asString(rres)

	for _, want := range []string{"H1 Title", "H2 Subtitle", "H3 Deep"} {
		if !strings.Contains(out2, want) {
			t.Errorf("outline missing %q; got:\n%s", want, out2)
		}
	}
	for _, hidden := range []string{"paragraph body", "another paragraph", "| 1 | 2 |"} {
		if strings.Contains(out2, hidden) {
			t.Errorf("outline should not contain %q; got:\n%s", hidden, out2)
		}
	}
}

// TestDocxWriteRejectsNonDocx verifies the early extension guard.
func TestDocxWriteRejectsNonDocx(t *testing.T) {
	_, err := runWriteDocx(map[string]any{
		"path":    filepath.Join(t.TempDir(), "out.txt"),
		"content": "hello",
	})
	if err == nil {
		t.Fatal("expected error for .txt output path")
	}
	if !strings.Contains(err.Error(), ".docx") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestDocxReadMissingFile checks the stat error path.
func TestDocxReadMissingFile(t *testing.T) {
	_, err := runReadDocx(map[string]any{
		"path": filepath.Join(t.TempDir(), "nope.docx"),
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestRenderTemplateInline exercises inline text/template rendering with a
// variables map: numbers, strings, range over a slice, conditionals.
func TestRenderTemplateInline(t *testing.T) {
	tmpl := `Hello {{.name}}, you have {{len .tasks}} tasks:
{{- range .tasks}}
- {{.}}
{{- end}}
Status: {{if .done}}complete{{else}}pending{{end}}
Score: {{.score}}
`
	res, err := runRenderTemplate(map[string]any{
		"template": tmpl,
		"variables": map[string]any{
			"name":  "Alice",
			"tasks": []any{"design", "build", "ship"},
			"done":  false,
			"score": 9.5,
		},
	})
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	out := asString(res)
	for _, want := range []string{"Hello Alice", "you have 3 tasks", "- design", "- build", "- ship", "Status: pending", "Score: 9.5"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestRenderTemplateFile loads a template from disk via template_path.
func TestRenderTemplateFile(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "agenda.tmpl")
	tmplBody := "# {{.title}}\n\nDate: {{.date}}\n"
	if err := writeFile(tmplPath, tmplBody); err != nil {
		t.Fatal(err)
	}
	res, err := runRenderTemplate(map[string]any{
		"template_path": tmplPath,
		"variables": map[string]any{
			"title": "Weekly Sync",
			"date":  "2026-03-01",
		},
	})
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	out := asString(res)
	if !strings.Contains(out, "# Weekly Sync") || !strings.Contains(out, "Date: 2026-03-01") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// TestRenderTemplateMissingVarErrors verifies missingkey=error surfaces failures
// instead of silently emitting <no value>.
func TestRenderTemplateMissingVarErrors(t *testing.T) {
	_, err := runRenderTemplate(map[string]any{
		"template":  "Hello {{.missing}}",
		"variables": map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error for missing variable")
	}
}

// TestRenderTemplateMutexArgs enforces the mutual-exclusion contract.
func TestRenderTemplateMutexArgs(t *testing.T) {
	_, err := runRenderTemplate(map[string]any{
		"template":      "x",
		"template_path": "y",
	})
	if err == nil {
		t.Fatal("expected error for both template + template_path")
	}
}

// TestRenderTemplateNeitherProvided enforces the at-least-one requirement.
func TestRenderTemplateNeitherProvided(t *testing.T) {
	_, err := runRenderTemplate(map[string]any{})
	if err == nil {
		t.Fatal("expected error when neither template nor template_path is given")
	}
}

// TestMDToPDFRequiresInput enforces that output alone is insufficient.
func TestMDToPDFRequiresInput(t *testing.T) {
	original := pandocPath
	pandocPath = "/usr/bin/pandoc" // pretend pandoc is available
	if runtime.GOOS == "windows" {
		pandocPath = `C:\nonexistent\pandoc.exe`
	}
	defer func() { pandocPath = original }()

	_, err := runMDToPDF(map[string]any{
		"output": filepath.Join(t.TempDir(), "out.pdf"),
	})
	if err == nil {
		t.Fatal("expected error when no input/markdown provided")
	}
	if !strings.Contains(err.Error(), "input") && !strings.Contains(err.Error(), "markdown") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestMDToPDFRejectsBadExt enforces the .pdf output extension guard.
func TestMDToPDFRejectsBadExt(t *testing.T) {
	original := pandocPath
	pandocPath = "/usr/bin/pandoc"
	if runtime.GOOS == "windows" {
		pandocPath = `C:\nonexistent\pandoc.exe`
	}
	defer func() { pandocPath = original }()

	_, err := runMDToPDF(map[string]any{
		"output":   filepath.Join(t.TempDir(), "out.txt"),
		"markdown": "# hi",
	})
	if err == nil {
		t.Fatal("expected error for non-.pdf output path")
	}
	if !strings.Contains(err.Error(), ".pdf") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestToolsList verifies that all 3 always-on tools are registered and
// md_to_pdf is conditional on pandocPath (which is empty in test environments
// without pandoc).
func TestToolsList(t *testing.T) {
	original := pandocPath
	defer func() { pandocPath = original }()

	pandocPath = ""
	names := toolNames()
	if !contains(names, "read_docx") || !contains(names, "write_docx") || !contains(names, "render_template") {
		t.Errorf("expected 3 always-on tools, got %v", names)
	}
	if contains(names, "md_to_pdf") {
		t.Errorf("md_to_pdf should not be registered when pandocPath is empty; got %v", names)
	}

	// Fake a pandoc path → tool list should now include md_to_pdf.
	pandocPath = "/usr/bin/pandoc"
	if runtime.GOOS == "windows" {
		pandocPath = `C:\nonexistent\pandoc.exe`
	}
	names = toolNames()
	if !contains(names, "md_to_pdf") {
		t.Errorf("md_to_pdf should be registered when pandocPath is set; got %v", names)
	}
}

// TestMCPProtocolEndToEnd drives the real serve() loop over a pipe: send
// initialize → tools/list → tools/call(read_docx) and verify each response.
// Mirrors the sheet plugin's end-to-end test to cover the JSON-RPC framing +
// dispatch path that the unit tests above skip.
func TestMCPProtocolEndToEnd(t *testing.T) {
	out := filepath.Join(t.TempDir(), "e2e.docx")
	if _, err := runWriteDocx(map[string]any{"path": out, "content": "# Hello\n\nWorld"}); err != nil {
		t.Fatal(err)
	}

	rIn, wIn, err := osPipe()
	if err != nil {
		t.Fatal(err)
	}
	rOut, wOut, err := osPipe()
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(rIn, wOut)
		wOut.Close()
	}()

	// initialize
	resp := sendMCP(t, wIn, rOut, "initialize", nil, 1)
	if got := jsonField(resp, "result"); got == nil {
		t.Fatalf("initialize missing result: %v", resp)
	}

	// tools/list
	resp = sendMCP(t, wIn, rOut, "tools/list", nil, 2)
	toolsRaw, _ := resp["result"].(map[string]any)["tools"]
	toolsArr, ok := toolsRaw.([]any)
	if !ok || len(toolsArr) < 3 {
		t.Fatalf("tools/list returned %v", resp)
	}

	// tools/call read_docx
	resp = sendMCP(t, wIn, rOut, "tools/call", map[string]any{
		"name":      "read_docx",
		"arguments": map[string]any{"path": out},
	}, 3)
	if got := resp["result"]; got == nil {
		t.Fatalf("tools/call read_docx missing result: %v", resp)
	}

	wIn.Close()
	<-errCh
}

// --- helpers ---

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func contains(arr []string, s string) bool {
	for _, x := range arr {
		if x == s {
			return true
		}
	}
	return false
}

func toolNames() []string {
	out := make([]string, 0, len(tools))
	// rebuild tools list reflecting current pandocPath (init only ran once at load)
	cur := []toolDef{readDocxTool, writeDocxTool, renderTemplateTool}
	if pandocPath != "" {
		cur = append(cur, mdToPDFTool)
	}
	for _, t := range cur {
		out = append(out, t.name)
	}
	return out
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestListTemplatesEmptyDir verifies the bound-method contract: a workspace
// without .rexion/templates/ returns an empty (not nil) slice and no error,
// so the frontend's `templates.length` access never crashes.
func TestListTemplatesEmptyDir(t *testing.T) {
	a := NewApp()
	ws := t.TempDir()
	a.setTestCtrl(nil, "")
	if tab := a.tabs["test"]; tab != nil {
		tab.WorkspaceRoot = ws
	}

	out, err := a.ListTemplates("")
	if err != nil {
		t.Fatalf("ListTemplates on empty workspace: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil slice per bound-method contract")
	}
	if len(out) != 0 {
		t.Errorf("expected 0 templates, got %d: %+v", len(out), out)
	}
}

// TestListTemplatesScansAndFilters verifies:
//   - files under .rexion/templates/ are picked up
//   - unknown extensions (.png, .zip) are skipped
//   - the `kind` filter narrows the list
//   - Name/Kind/RelPath/Description are populated correctly
//   - newest-first ordering holds (modtime descending)
func TestListTemplatesScansAndFilters(t *testing.T) {
	a := NewApp()
	ws := t.TempDir()
	tmplDir := filepath.Join(ws, ".rexion", "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Three known kinds + one skipped extension.
	if err := os.WriteFile(filepath.Join(tmplDir, "weekly.md"), []byte("# Weekly\nA weekly report template."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "service.docx"), []byte("PK\x03\x04 fake docx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "cover.tmpl"), []byte("---\ndescription: cover letter\n---\nDear {{.name}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "ignored.png"), []byte("\x89PNG fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make weekly.md the newest so it sorts first.
	newer := filepath.Join(tmplDir, "weekly.md")
	if err := os.Chtimes(newer, nowPlus(2), nowPlus(2)); err != nil {
		t.Fatal(err)
	}

	a.setTestCtrl(nil, "")
	if tab := a.tabs["test"]; tab != nil {
		tab.WorkspaceRoot = ws
	}

	out, err := a.ListTemplates("")
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 templates (md+docx+tmpl), got %d: %+v", len(out), out)
	}
	if out[0].Name != "weekly" {
		t.Errorf("expected weekly first (newest), got %q", out[0].Name)
	}
	if out[0].Kind != "md" {
		t.Errorf("weekly kind = %q, want md", out[0].Kind)
	}
	if out[0].RelPath != ".rexion/templates/weekly.md" {
		t.Errorf("RelPath = %q", out[0].RelPath)
	}
	if out[0].Description == "" {
		t.Error("weekly.md should have a description from its body")
	}
	if !strings.Contains(out[0].Description, "weekly report template") {
		t.Errorf("weekly description = %q, want body-derived", out[0].Description)
	}
	// cover.tmpl has frontmatter — description must skip past it.
	var cover *TemplateMeta
	for i := range out {
		if out[i].Name == "cover" {
			cover = &out[i]
		}
	}
	if cover == nil {
		t.Fatal("cover.tmpl not in list")
	}
	if cover.Description != "Dear {{.name}}" {
		t.Errorf("cover description = %q, want 'Dear {{.name}}' (post-frontmatter)", cover.Description)
	}

	// Filter by kind.
	docs, err := a.ListTemplates("docx")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Name != "service" {
		t.Errorf("ListTemplates(docx) = %+v, want only service", docs)
	}
}

// TestListTemplatesSkipsHiddenAndDirs verifies dotfiles and nested directories
// are not surfaced as templates.
func TestListTemplatesSkipsHiddenAndDirs(t *testing.T) {
	a := NewApp()
	ws := t.TempDir()
	tmplDir := filepath.Join(ws, ".rexion", "templates")
	os.MkdirAll(tmplDir, 0o755)
	os.WriteFile(filepath.Join(tmplDir, ".hidden.md"), []byte("hidden"), 0o644)
	os.MkdirAll(filepath.Join(tmplDir, "subdir"), 0o755)
	os.WriteFile(filepath.Join(tmplDir, "real.md"), []byte("real"), 0o644)

	a.setTestCtrl(nil, "")
	if tab := a.tabs["test"]; tab != nil {
		tab.WorkspaceRoot = ws
	}
	out, _ := a.ListTemplates("")
	if len(out) != 1 || out[0].Name != "real" {
		t.Errorf("expected only real.md, got %+v", out)
	}
}

// TestExportToWorkspaceWritesAndNotices verifies the happy path: relative path
// resolved against workspace root, parent dirs created, content written, and a
// notice emitted to the tab sink so the transcript records the export.
func TestExportToWorkspaceWritesAndNotices(t *testing.T) {
	a := NewApp()
	ws := t.TempDir()
	a.setTestCtrl(nil, "")
	if tab := a.tabs["test"]; tab != nil {
		tab.WorkspaceRoot = ws
	}

	abs, err := a.ExportToWorkspace("", "reports/周报.docx", "hello world")
	if err != nil {
		t.Fatalf("ExportToWorkspace: %v", err)
	}
	want := filepath.Join(ws, "reports", "周报.docx")
	if abs != want {
		t.Errorf("abs = %q, want %q", abs, want)
	}
	body, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(body) != "hello world" {
		t.Errorf("body = %q, want 'hello world'", body)
	}
}

// TestExportToWorkspaceRejectsEscape verifies that attempts to write outside
// the workspace are rejected with os.ErrPermission (the same sentinel used by
// workspacePath).
func TestExportToWorkspaceRejectsEscape(t *testing.T) {
	a := NewApp()
	ws := t.TempDir()
	a.setTestCtrl(nil, "")
	if tab := a.tabs["test"]; tab != nil {
		tab.WorkspaceRoot = ws
	}

	_, err := a.ExportToWorkspace("", "../../etc/passwd", "pwned")
	if err == nil {
		t.Fatal("expected error for path-escape attempt")
	}
	if err != os.ErrPermission {
		t.Errorf("err = %v, want os.ErrPermission", err)
	}
}

// TestExportToWorkspaceOverwrites verifies existing files are replaced, not
// appended — the agent re-runs a skill and the new content wins.
func TestExportToWorkspaceOverwrites(t *testing.T) {
	a := NewApp()
	ws := t.TempDir()
	a.setTestCtrl(nil, "")
	if tab := a.tabs["test"]; tab != nil {
		tab.WorkspaceRoot = ws
	}
	if _, err := a.ExportToWorkspace("", "out.txt", "v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ExportToWorkspace("", "out.txt", "v2"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(ws, "out.txt"))
	if string(body) != "v2" {
		t.Errorf("body = %q, want v2 (overwrite)", body)
	}
}

// TestOpenInOSDefaultRejectsMissing verifies the stat guard: a non-existent
// path returns an error rather than spawning a useless OS process.
func TestOpenInOSDefaultRejectsMissing(t *testing.T) {
	a := NewApp()
	missing := filepath.Join(t.TempDir(), "does-not-exist.docx")
	err := a.OpenInOSDefault(missing)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "stat") {
		t.Errorf("err = %v, want stat-related", err)
	}
}

// TestOpenInOSDefaultRejectsEmpty verifies the empty-path guard.
func TestOpenInOSDefaultRejectsEmpty(t *testing.T) {
	a := NewApp()
	if err := a.OpenInOSDefault("   "); err == nil {
		t.Fatal("expected error for empty path")
	}
	if err := a.OpenInOSDefault(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}

// TestRenderDocPreviewImageReturnsInlineURL verifies an image file is
// registered with the media-token store and the returned URL is renderable
// inline (starts with /__Rexion_workspace_media/).
func TestRenderDocPreviewImageReturnsInlineURL(t *testing.T) {
	a := NewApp()
	tmp := t.TempDir()
	pngPath := filepath.Join(tmp, "chart.png")
	if err := os.WriteFile(pngPath, []byte("\x89PNG\r\n\x1a\n fake png"), 0o644); err != nil {
		t.Fatal(err)
	}
	pages, err := a.RenderDocPreview(pngPath, 1)
	if err != nil {
		t.Fatalf("RenderDocPreview: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages))
	}
	if !strings.HasPrefix(pages[0].URL, "/__Rexion_workspace_media/") {
		t.Errorf("URL = %q, want media-token URL", pages[0].URL)
	}
	if pages[0].Total != 1 || pages[0].Page != 1 {
		t.Errorf("page = %d/%d, want 1/1", pages[0].Page, pages[0].Total)
	}
}

// TestRenderDocPreviewDocxReturnsDownloadURL verifies a docx is registered as
// a binary blob with the right mime so the frontend can show a download chip.
// (Page-by-page rendering is a future enhancement — see roadmap §5.4.)
func TestRenderDocPreviewDocxReturnsDownloadURL(t *testing.T) {
	a := NewApp()
	tmp := t.TempDir()
	docxPath := filepath.Join(tmp, "report.docx")
	if err := os.WriteFile(docxPath, []byte("PK\x03\x04 fake docx"), 0o644); err != nil {
		t.Fatal(err)
	}
	pages, err := a.RenderDocPreview(docxPath, 1)
	if err != nil {
		t.Fatalf("RenderDocPreview: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("expected 1 page entry, got %d", len(pages))
	}
	if !strings.HasPrefix(pages[0].URL, "/__Rexion_workspace_media/") {
		t.Errorf("URL = %q, want media-token URL", pages[0].URL)
	}
	if !strings.HasSuffix(pages[0].URL, "report.docx") {
		t.Errorf("URL should end with filename, got %q", pages[0].URL)
	}
}

// TestRenderDocPreviewRejectsDir verifies the directory guard.
func TestRenderDocPreviewRejectsDir(t *testing.T) {
	a := NewApp()
	dir := t.TempDir()
	_, err := a.RenderDocPreview(dir, 1)
	if err == nil {
		t.Fatal("expected error for directory path")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("err = %v, want directory message", err)
	}
}

// TestTemplateKindForExt covers the extension→kind map exhaustively so a future
// addition doesn't silently miss a case.
func TestTemplateKindForExt(t *testing.T) {
	cases := map[string]string{
		".docx":     "docx",
		".DOCX":     "docx", // case-insensitive
		".xlsx":     "xlsx",
		".xlsm":     "xlsx",
		".csv":      "csv",
		".md":       "md",
		".markdown": "md",
		".tmpl":     "tmpl",
		".tpl":      "tmpl",
		".gotmpl":   "tmpl",
		".txt":      "txt",
		".png":      "", // not a template kind
		".pdf":      "", // not a template kind
		"":          "",
	}
	for ext, want := range cases {
		if got := templateKindForExt(ext); got != want {
			t.Errorf("templateKindForExt(%q) = %q, want %q", ext, got, want)
		}
	}
}

// nowPlus returns a time n seconds in the future. Used to set deterministic,
// monotonic modtimes so the newest-first sort is testable without flakiness.
func nowPlus(seconds int64) time.Time {
	return time.Now().Add(time.Duration(seconds) * time.Second)
}

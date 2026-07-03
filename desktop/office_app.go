// Desktop preview & template UI.
//
// Wails-bound surface for exporting agent artifacts to the workspace, opening
// files in the OS default app, previewing docx/pdf via the media-token
// channel, and listing the user's template library. Kept in a separate file
// from app.go so this surface is easy to review and roll back; the methods
// follow the same conventions as the rest of the bound surface (nil-safe on
// controller, non-nil slice returns, relative-path containment via
// workspacePath).
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// TemplateMeta describes one user template from .reasonix/templates/.
type TemplateMeta struct {
	Name        string `json:"name"`        // file stem, e.g. "weekly-report"
	Kind        string `json:"kind"`        // "docx" | "xlsx" | "md" | "tmpl" | "txt" | "csv"
	Path        string `json:"path"`        // absolute path
	RelPath     string `json:"relPath"`     // relative to workspace root ("" if outside)
	Description string `json:"description"` // first non-empty line of file (for text templates)
	Size        int64  `json:"size"`
	ModTime     int64  `json:"modTime"` // unix seconds
}

// DocPreviewPage is one page of a rendered document preview.
type DocPreviewPage struct {
	URL   string `json:"url"`   // media token URL, e.g. /__reasonix_workspace_media/<tok>/<name>
	Page  int    `json:"page"`  // 1-based
	Total int    `json:"total"` // total pages available
}

// templateKindForExt maps a file extension to the template "kind" surfaced to
// the frontend. Extensions not in this map return "" and are skipped by
// ListTemplates — the template library only shows files the UI knows how to
// preview or apply.
func templateKindForExt(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "docx":
		return "docx"
	case "xlsx", "xlsm":
		return "xlsx"
	case "csv":
		return "csv"
	case "md", "markdown":
		return "md"
	case "tmpl", "tpl", "gotmpl":
		return "tmpl"
	case "txt":
		return "txt"
	default:
		return ""
	}
}

// previewKindForExt reports whether the extension is renderable inline (image).
// docx/pdf return false here — RenderDocPreview still registers them with the
// media-token store so the frontend gets a stable URL, but the URL triggers a
// download rather than an inline <img> render until an external renderer
// (LibreOffice/pandoc) is wired in.
func previewKindForExt(ext string) (kind, mime string, ok bool) {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "png":
		return "image", "image/png", true
	case "jpg", "jpeg":
		return "image", "image/jpeg", true
	case "gif":
		return "image", "image/gif", true
	case "svg":
		return "image", "image/svg+xml", true
	case "webp":
		return "image", "image/webp", true
	}
	return "", "", false
}

// ListTemplates lists user templates under <workspace>/.reasonix/templates/.
// kind filters by file type ("docx", "xlsx", "md", "tmpl", "txt", "csv"); pass
// "" to list all known kinds. The library is scanned fresh on every call so
// newly added templates appear without a restart.
func (a *App) ListTemplates(kind string) ([]TemplateMeta, error) {
	out := []TemplateMeta{} // non-nil per the bound-method contract
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return out, err
	}
	root := filepath.Join(base, ".reasonix", "templates")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil // no library yet — empty list, not an error
		}
		return out, fmt.Errorf("read templates dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		ext := filepath.Ext(name)
		k := templateKindForExt(ext)
		if k == "" {
			continue
		}
		if kind != "" && k != kind {
			continue
		}
		abs := filepath.Join(root, name)
		info, err := e.Info()
		if err != nil {
			continue
		}
		rel := ""
		if r, err := filepath.Rel(base, abs); err == nil && !strings.HasPrefix(r, "..") {
			rel = filepath.ToSlash(r)
		}
		out = append(out, TemplateMeta{
			Name:        strings.TrimSuffix(name, ext),
			Kind:        k,
			Path:        abs,
			RelPath:     rel,
			Description: readTemplateDescription(abs, k),
			Size:        info.Size(),
			ModTime:     info.ModTime().Unix(),
		})
	}
	// Newest first — the user just edited a template, it surfaces on top.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ModTime != out[j].ModTime {
			return out[i].ModTime > out[j].ModTime
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// readTemplateDescription pulls a one-line description out of a text template
// for the template library grid. For binary kinds (docx/xlsx) we return "" —
// extracting real descriptions requires parsing the binary format, which the
// plugin layer handles, not the desktop surface.
func readTemplateDescription(path, kind string) string {
	if kind == "docx" || kind == "xlsx" || kind == "csv" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	body := strings.TrimSpace(string(buf[:n]))
	// Skip frontmatter (--- ... ---) and pick the first non-empty,
	// non-heading line as the description.
	if strings.HasPrefix(body, "---") {
		if end := strings.Index(body[3:], "---"); end >= 0 {
			body = strings.TrimSpace(body[3+end+3:])
		}
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > 120 {
			line = line[:120] + "…"
		}
		return line
	}
	return ""
}

// ExportToWorkspace writes an agent artifact (e.g. the docx produced by the
// office plugin) to the workspace at relPath and returns the absolute path
// written. The frontend wraps the call with ApprovalModal (roadmap §5.4) so the
// user signs off before any file lands on disk — this method itself never
// prompts; it just writes when called.
//
// relPath may be relative (resolved against the active workspace root) or
// absolute (must already be inside the workspace). Path-escape attempts
// ("../../etc/passwd") return os.ErrPermission.
//
// Parent directories are created if missing; existing files are overwritten.
func (a *App) ExportToWorkspace(tabID, relPath, content string) (string, error) {
	abs, ok, err := a.workspacePath(relPath)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", os.ErrPermission
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("create parent dir: %w", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	// Emit a notice so the active tab's transcript records the export — the
	// user can see "wrote /path/to/周报.docx" in the conversation history.
	a.noticeForTab(tabID, fmt.Sprintf("exported %d bytes to %s", len(content), abs))
	return abs, nil
}

// OpenInOSDefault opens an arbitrary absolute path with the OS default app.
// Unlike OpenWorkspacePath (which takes a workspace-relative path), this
// accepts absolute paths from the agent's tool output — e.g. a docx produced
// by mcp__office__write_docx surfaces as an absolute path the user can click
// to open in Word/WPS.
//
// On Windows the path is opened via ShellExecute; on macOS via `open`; on
// Linux via `xdg-open` (see open_workspace_*.go).
func (a *App) OpenInOSDefault(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return os.ErrInvalid
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	return openWorkspacePath(path)
}

// RenderDocPreview registers a document (docx/xlsx/pdf/image) with the
// media-token store and returns URLs the frontend can use to preview it.
//
//   - Images (png/jpg/gif/svg/webp): inline <img> URL
//   - PDF: inline <iframe> URL (browser's native PDF viewer)
//   - docx: parsed to HTML and served via inline media token (renders in <iframe>)
//   - xlsx/csv: parsed to JSON {headers, rows} and placed in the URL field
//     directly (SheetViewer parses it client-side; no media token needed)
//
// The `page` argument is accepted for forward compatibility but currently
// ignored — docx/xlsx previews are single-page.
//
// Returns a non-nil slice (possibly empty on error) per the bound-method
// contract; callers can safely index [0] only when len > 0.
func (a *App) RenderDocPreview(absPath string, page int) ([]DocPreviewPage, error) {
	out := []DocPreviewPage{}
	absPath = strings.TrimSpace(absPath)
	if absPath == "" {
		return out, os.ErrInvalid
	}
	if abs, err := filepath.Abs(absPath); err == nil {
		absPath = abs
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return out, fmt.Errorf("stat: %w", err)
	}
	if info.IsDir() {
		return out, fmt.Errorf("path is a directory")
	}
	name := filepath.Base(absPath)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))

	// Images: register with media token store and return the inline URL.
	if kind, mime, ok := previewKindForExt(ext); ok {
		tok := a.ensureMediaTokenStore().create(absPath, name, mime, kind, info.Size(), info.ModTime())
		return []DocPreviewPage{{
			URL:   "/__reasonix_workspace_media/" + tok + "/" + url.PathEscape(name),
			Page:  1,
			Total: 1,
		}}, nil
	}

	// PDF: serve the raw file with application/pdf MIME so the browser's
	// native PDF viewer renders it inside an <iframe>.
	if ext == "pdf" {
		tok := a.ensureMediaTokenStore().create(absPath, name, "application/pdf", "pdf", info.Size(), info.ModTime())
		return []DocPreviewPage{{
			URL:   "/__reasonix_workspace_media/" + tok + "/" + url.PathEscape(name),
			Page:  1,
			Total: 1,
		}}, nil
	}

	// docx: parse to HTML and serve via inline media token. The frontend
	// renders it in an <iframe>.
	if ext == "docx" {
		htmlStr, err := parseDocxToHTML(absPath)
		if err != nil {
			return out, fmt.Errorf("parse docx: %w", err)
		}
		htmlName := strings.TrimSuffix(name, filepath.Ext(name)) + ".html"
		tok := a.ensureMediaTokenStore().createInline(htmlName, "text/html; charset=utf-8", "docx", []byte(htmlStr))
		return []DocPreviewPage{{
			URL:   "/__reasonix_workspace_media/" + tok + "/" + url.PathEscape(htmlName),
			Page:  1,
			Total: 1,
		}}, nil
	}

	// xlsx: parse to structured JSON and return it directly in the URL
	// field. SheetViewer does JSON.parse(p.url) to extract {headers, rows}
	// — no media token is needed because the data is self-contained.
	if ext == "xlsx" || ext == "xlsm" {
		td, err := parseXlsxToTable(absPath)
		if err != nil {
			return out, fmt.Errorf("parse xlsx: %w", err)
		}
		js, err := json.Marshal(td)
		if err != nil {
			return out, fmt.Errorf("marshal xlsx data: %w", err)
		}
		return []DocPreviewPage{{
			URL:   string(js),
			Page:  1,
			Total: 1,
		}}, nil
	}

	// csv: parse to structured JSON like xlsx (SheetViewer handles it).
	if ext == "csv" {
		td, err := parseCSVToTable(absPath)
		if err != nil {
			return out, fmt.Errorf("parse csv: %w", err)
		}
		js, err := json.Marshal(td)
		if err != nil {
			return out, fmt.Errorf("marshal csv data: %w", err)
		}
		return []DocPreviewPage{{
			URL:   string(js),
			Page:  1,
			Total: 1,
		}}, nil
	}

	// Fallback: register as binary blob so the frontend gets a download URL.
	mime := "application/octet-stream"
	tok := a.ensureMediaTokenStore().create(absPath, name, mime, "binary", info.Size(), info.ModTime())
	return []DocPreviewPage{{
		URL:   "/__reasonix_workspace_media/" + tok + "/" + url.PathEscape(name),
		Page:  1,
		Total: 1,
	}}, nil
}

// SetWorkspaceType switches the active tab between "coding", "office", and
// "assistant" mode. In office mode the frontend shows the office-capability
// panel (skill cards); in assistant mode the frontend shows the AI assistant
// panel; coding is the default code-centric workspace chrome.
func (a *App) SetWorkspaceType(wt string) error {
	wt = normalizeWorkspaceType(wt)
	tab := a.activeTab()
	if tab == nil {
		return os.ErrInvalid
	}
	a.mu.Lock()
	tab.workspaceType = wt
	a.saveTabsLocked()
	a.mu.Unlock()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, eventChannel, map[string]any{
			"kind":          "workspace-type-changed",
			"tabId":         tab.ID,
			"workspaceType": wt,
		})
	}
	return nil
}

// WorkspaceType returns the active tab's workspace type ("coding", "office", or "assistant").
func (a *App) WorkspaceType() string {
	tab := a.activeTab()
	if tab == nil {
		return "coding"
	}
	return normalizeWorkspaceType(tab.workspaceType)
}

// UploadTemplate opens a native file-picker and copies the selected file into
// <workspace>/.reasonix/templates/. Only files with recognised template
// extensions (.docx/.xlsx/.xlsm/.csv/.md/.markdown/.tmpl/.tpl/.gotmpl/.txt)
// are accepted. Returns the absolute path of the written file so the frontend
// can refresh the list and highlight the new entry.
func (a *App) UploadTemplate() (string, error) {
	if a.ctx == nil {
		return "", os.ErrInvalid
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Upload Template",
		Filters: []runtime.FileFilter{
			{DisplayName: "Template files", Pattern: "*.docx;*.xlsx;*.xlsm;*.csv;*.md;*.markdown;*.tmpl;*.tpl;*.gotmpl;*.txt"},
			{DisplayName: "Word documents", Pattern: "*.docx"},
			{DisplayName: "Spreadsheets", Pattern: "*.xlsx;*.xlsm;*.csv"},
			{DisplayName: "Markdown", Pattern: "*.md;*.markdown"},
			{DisplayName: "Go templates", Pattern: "*.tmpl;*.tpl;*.gotmpl"},
			{DisplayName: "Text files", Pattern: "*.txt"},
		},
	})
	if err != nil || path == "" {
		return "", err // user cancelled or OS error
	}

	ext := filepath.Ext(path)
	if templateKindForExt(ext) == "" {
		return "", fmt.Errorf("unsupported template extension: %s", ext)
	}

	base, err := a.activeWorkspaceBase()
	if err != nil {
		return "", err
	}
	tmplDir := filepath.Join(base, ".reasonix", "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		return "", fmt.Errorf("create templates dir: %w", err)
	}

	dst := filepath.Join(tmplDir, filepath.Base(path))
	// If a file with the same name already exists, append a numeric suffix.
	if _, err := os.Stat(dst); err == nil {
		stem := strings.TrimSuffix(filepath.Base(path), ext)
		for i := 1; ; i++ {
			candidate := filepath.Join(tmplDir, fmt.Sprintf("%s-%d%s", stem, i, ext))
			if _, err := os.Stat(candidate); err != nil {
				dst = candidate
				break
			}
		}
	}

	if err := copyFile(dst, path); err != nil {
		return "", fmt.Errorf("copy template: %w", err)
	}
	return dst, nil
}

// UploadTemplateDataURL receives a file name and a data-URL (base64-encoded)
// and writes it into <workspace>/.reasonix/templates/. This is the
// drag-and-drop / paste counterpart to UploadTemplate (which uses the native
// file picker). Returns the absolute path of the written file.
func (a *App) UploadTemplateDataURL(name, dataURL string) (string, error) {
	ext := filepath.Ext(name)
	if templateKindForExt(ext) == "" {
		return "", fmt.Errorf("unsupported template extension: %s", ext)
	}
	const marker = ";base64,"
	i := strings.Index(dataURL, marker)
	if !strings.HasPrefix(dataURL, "data:") || i < 0 {
		return "", fmt.Errorf("invalid data URL")
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[i+len(marker):])
	if err != nil {
		return "", fmt.Errorf("decode data URL: %w", err)
	}
	if len(raw) == 0 || len(raw) > 25*1024*1024 {
		return "", fmt.Errorf("file must be between 1 byte and 25 MB")
	}

	base, err := a.activeWorkspaceBase()
	if err != nil {
		return "", err
	}
	tmplDir := filepath.Join(base, ".reasonix", "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		return "", fmt.Errorf("create templates dir: %w", err)
	}

	dst := filepath.Join(tmplDir, filepath.Base(name))
	if _, err := os.Stat(dst); err == nil {
		stem := strings.TrimSuffix(filepath.Base(name), ext)
		for i := 1; ; i++ {
			candidate := filepath.Join(tmplDir, fmt.Sprintf("%s-%d%s", stem, i, ext))
			if _, err := os.Stat(candidate); err != nil {
				dst = candidate
				break
			}
		}
	}

	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return "", fmt.Errorf("write template: %w", err)
	}
	return dst, nil
}

// copyFile copies src to dst, creating any missing parent directories.
func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

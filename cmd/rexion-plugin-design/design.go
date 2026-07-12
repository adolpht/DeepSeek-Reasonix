// design.go — design-to-code tool implementations and the shared layout types
// used by both design_analyze (vision API) and figma_import (Figma REST API).
//
// Tools exposed (registered in main.go):
//   - design_upload    save a base64 mockup to a temp file
//   - design_analyze   call the vision API to extract a structured LayoutTree
//   - design_to_code   turn a LayoutTree (JSON) into HTML/Vue/React source
//   - figma_import     fetch a Figma file and project it to a LayoutTree
//   - design_compare   diff a design image against a code screenshot via vision
//
// The LayoutTree shape is the canonical intermediate representation. Both the
// vision API (design_analyze) and Figma API (figma_import) produce it, and
// design_to_code consumes it — so code generation is source-agnostic.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- shared layout types (used by figma.go too) ---

// LayoutTree is the intermediate representation produced by design_analyze and
// figma_import, and consumed by design_to_code.
type LayoutTree struct {
	Type     string       `json:"type"`               // e.g. FRAME, TEXT, RECTANGLE, COMPONENT
	Name     string       `json:"name"`               // layer name
	Text     string       `json:"text,omitempty"`     // text content (TEXT nodes)
	BBox     *LayoutBox   `json:"bbox,omitempty"`     // absolute bounding box
	Colors   []string     `json:"colors,omitempty"`   // fill hex tokens, e.g. "#1f2937"
	Font     *LayoutFont  `json:"font,omitempty"`     // typography (TEXT nodes)
	Children []LayoutTree `json:"children,omitempty"` // nested layers
}

// LayoutBox is an axis-aligned bounding box in design pixels.
type LayoutBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// LayoutFont captures the typography of a TEXT node.
type LayoutFont struct {
	Family string  `json:"family,omitempty"`
	Size   float64 `json:"size,omitempty"`
	Weight string  `json:"weight,omitempty"`
}

// --- design_upload ---

// runDesignUpload decodes a base64 image, writes it to a temp directory, and
// returns the saved path plus a data URL the frontend can render inline.
func runDesignUpload(args map[string]any) (any, error) {
	raw, err := argString(args, "image_base64")
	if err != nil {
		return nil, err
	}
	cleaned := stripDataURLPrefix(raw)

	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("decode base64 image: %w", err)
	}

	filename := argStringDefault(args, "filename", "")
	if filename == "" {
		filename = fmt.Sprintf("design-%d.png", time.Now().Unix())
	}
	// Sanitize: keep the basename only so nothing escapes the temp dir.
	filename = sanitizeFilename(filename)

	dir, err := os.MkdirTemp("", "rexion-design-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	fullPath := filepath.Join(dir, filename)
	if err := os.WriteFile(fullPath, decoded, 0o644); err != nil {
		return nil, fmt.Errorf("write design image: %w", err)
	}

	mime := sniffMIME(decoded)
	preview := "data:" + mime + ";base64," + cleaned

	return map[string]any{
		"image_path": fullPath,
		"preview":    preview,
		"size":       len(decoded),
		"mime":       mime,
	}, nil
}

// --- design_analyze ---

// runDesignAnalyze calls the vision API with a layout-extraction prompt and
// returns the structured LayoutTree as JSON text (the model returns JSON; we
// pass it through verbatim so design_to_code can re-parse it).
func runDesignAnalyze(args map[string]any) (any, error) {
	base64Img, err := resolveImageBase64(args)
	if err != nil {
		return nil, err
	}
	framework := argStringDefault(args, "framework", "html")
	style := argStringDefault(args, "style", "css")

	prompt := buildAnalyzePrompt(framework, style)
	reply, err := CallVisionAPI(base64Img, prompt)
	if err != nil {
		return nil, err
	}

	analysis := extractJSONBlock(reply)
	// Validate it parses as a LayoutTree so callers get a contract guarantee.
	var tree LayoutTree
	if jerr := json.Unmarshal([]byte(analysis), &tree); jerr != nil {
		// Still return the raw text — the model may have wrapped the JSON. Caller
		// (design_to_code or the agent) can still see the structure.
		return map[string]any{
			"analysis":     analysis,
			"raw":          reply,
			"framework":    framework,
			"style":        style,
			"parse_error":  jerr.Error(),
		}, nil
	}
	return map[string]any{
		"analysis":  analysis,
		"framework": framework,
		"style":     style,
	}, nil
}

// --- design_to_code ---

// runDesignToCode turns a LayoutTree JSON (from design_analyze or figma_import)
// into complete framework source. For html it returns a single self-contained
// HTML document; for vue/react it returns one component file.
func runDesignToCode(args map[string]any) (any, error) {
	analysis, err := argString(args, "analysis")
	if err != nil {
		return nil, err
	}
	framework := argStringDefault(args, "framework", "html")
	style := argStringDefault(args, "style", "css")
	componentName := argStringDefault(args, "component_name", "DesignComponent")

	var tree LayoutTree
	if err := json.Unmarshal([]byte(analysis), &tree); err != nil {
		return nil, fmt.Errorf("parse analysis JSON: %w", err)
	}

	var filename, code string
	switch framework {
	case "vue":
		filename = componentName + ".vue"
		code = renderVue(tree, style, componentName)
	case "react":
		filename = componentName + ".tsx"
		code = renderReact(tree, style, componentName)
	default: // html
		filename = "index.html"
		code = renderHTML(tree, style)
	}

	return map[string]any{
		"filename": filename,
		"framework": framework,
		"style":    style,
		"code":     code,
	}, nil
}

// --- figma_import ---

// runFigmaImport fetches a Figma file and projects it to a LayoutTree.
func runFigmaImport(args map[string]any) (any, error) {
	figmaURL, err := argString(args, "figma_url")
	if err != nil {
		return nil, err
	}
	token := resolveFigmaToken(argStringDefault(args, "figma_token", ""))
	nodeID := argStringDefault(args, "node_id", "")

	fileKey, err := parseFigmaFileKey(figmaURL)
	if err != nil {
		return nil, err
	}

	root, err := FetchFigmaFile(fileKey, token)
	if err != nil {
		return nil, err
	}

	target := root
	if nodeID != "" {
		if found := findNodeByID(root, nodeID); found != nil {
			target = *found
		}
	}

	tree := ExtractLayout(target)
	payload, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal layout tree: %w", err)
	}
	return map[string]any{
		"file_key": fileKey,
		"node_id":  nodeID,
		"layout":   string(payload),
	}, nil
}

// --- design_compare ---

// runDesignCompare sends both images to the vision API and asks for a structured
// diff (what matches, what differs, severity, fix suggestions).
func runDesignCompare(args map[string]any) (any, error) {
	designImg, err := argString(args, "design_image")
	if err != nil {
		return nil, err
	}
	screenshotImg, err := argString(args, "screenshot_image")
	if err != nil {
		return nil, err
	}

	prompt := `You are comparing a design mockup against a screenshot of generated code.
Return a JSON object with these fields:
{
  "match_score": <0-100 integer>,
  "matches": [<strings describing what matches>],
  "differences": [<strings describing what differs>],
  "severity": "low" | "medium" | "high",
  "suggestions": [<actionable fix suggestions>]
}
Only return the JSON, no prose.`

	// Send the design as the primary image and describe the screenshot in the
	// prompt. OpenAI vision supports multiple image_url parts, so we compose a
	// two-image message directly.
	designURL := normalizeDataURL(designImg)
	shotURL := normalizeDataURL(screenshotImg)

	cfg, err := loadVisionConfig()
	if err != nil {
		return nil, err
	}
	body := struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}{
		Model: cfg.Model,
		Messages: []struct {
			Role    string `json:"role"`
			Content []map[string]any `json:"content"`
		}{
			{
				Role: "user",
				Content: []map[string]any{
					{"type": "text", "text": prompt},
					{"type": "text", "text": "Design mockup:"},
					{"type": "image_url", "image_url": map[string]string{"url": designURL}},
					{"type": "text", "text": "Generated code screenshot:"},
					{"type": "image_url", "image_url": map[string]string{"url": shotURL}},
				},
			},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal compare request: %w", err)
	}
	reply, err := postVision(cfg, payload)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"comparison": extractJSONBlock(reply),
		"raw":        reply,
	}, nil
}

// --- helpers ---

// resolveImageBase64 returns base64 image bytes from either image_path or
// image_base64. When image_path is given, the file is re-encoded to base64.
func resolveImageBase64(args map[string]any) (string, error) {
	if p := argStringDefault(args, "image_path", ""); p != "" {
		data, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("read image_path: %w", err)
		}
		return base64.StdEncoding.EncodeToString(data), nil
	}
	b, err := argString(args, "image_base64")
	if err != nil {
		return "", err
	}
	return stripDataURLPrefix(b), nil
}

// buildAnalyzePrompt constructs the vision prompt that asks for a structured
// LayoutTree JSON. Keeping the target framework/style in the prompt lets the
// model bias its token choices (e.g. tailwind class names) for downstream code.
func buildAnalyzePrompt(framework, style string) string {
	return fmt.Sprintf(`You are a design-to-code analyst. Analyze this design mockup and return a
structured layout tree as JSON with this exact shape:

{
  "type": "FRAME",
  "name": "root",
  "bbox": {"x": 0, "y": 0, "width": <num>, "height": <num>},
  "colors": ["#hex", ...],
  "children": [
    {
      "type": "FRAME|TEXT|RECTANGLE|COMPONENT",
      "name": "<layer name>",
      "text": "<text content if TEXT>",
      "bbox": {"x": <num>, "y": <num>, "width": <num>, "height": <num>},
      "colors": ["#hex", ...],
      "font": {"family": "<font>", "size": <num>, "weight": "normal|bold|..."},
      "children": [...]
    }
  ]
}

Rules:
- Capture the full component hierarchy (header, nav, cards, buttons, footers...).
- Extract dominant fill colors as hex tokens (#rrggbb).
- For TEXT nodes, include the actual text content and typography.
- bbox is in design pixels relative to the root.
- Target framework: %s; styling: %s.
- Return ONLY the JSON, no prose or code fences.`, framework, style)
}

// extractJSONBlock pulls the first JSON object out of a model reply that may
// wrap it in ```json ... ``` fences or surround it with prose.
func extractJSONBlock(reply string) string {
	s := strings.TrimSpace(reply)
	if strings.HasPrefix(s, "```") {
		// Strip an opening fence (with optional language tag) and a closing fence.
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	// If there's still surrounding prose, find the first { and the last }.
	if start := strings.Index(s, "{"); start >= 0 {
		if end := strings.LastIndex(s, "}"); end > start {
			return s[start : end+1]
		}
	}
	return s
}

func stripDataURLPrefix(s string) string {
	if idx := strings.Index(s, ","); strings.HasPrefix(s, "data:") && idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// sniffMIME returns a best-effort MIME type from the leading image bytes.
func sniffMIME(data []byte) string {
	switch {
	case len(data) >= 8 && string(data[0:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 3 && string(data[0:3]) == "\xff\xd8\xff":
		return "image/jpeg"
	case len(data) >= 6 && (string(data[0:6]) == "GIF87a" || string(data[0:6]) == "GIF89a"):
		return "image/gif"
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	default:
		return "image/png"
	}
}

// sanitizeFilename keeps only the basename and strips path separators / parent
// refs so the upload can never escape its temp dir.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	if name == "." || name == "" || name == string(os.PathSeparator) {
		return "design.png"
	}
	// Replace any character that's illegal on Windows just to be safe.
	name = strings.Map(func(r rune) rune {
		if r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, name)
	return name
}

// findNodeByID does a depth-first search for a node with the given id.
func findNodeByID(root FigmaNode, id string) *FigmaNode {
	if root.ID == id {
		return &root
	}
	for i := range root.Children {
		if found := findNodeByID(root.Children[i], id); found != nil {
			return found
		}
	}
	return nil
}

// --- code renderers ---

// renderHTML produces a self-contained HTML document from the layout tree.
func renderHTML(tree LayoutTree, style string) string {
	var css strings.Builder
	css.WriteString("body{margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#fff;color:#1f2937;}\n")
	body := renderHTMLNode(tree, &css, "")
	if style == "tailwind" {
		return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<script src="https://cdn.tailwindcss.com"></script>
<style>
%s</style>
</head>
<body>
%s
</body>
</html>`, escapeHTML(tree.Name), css.String(), body)
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<style>
%s</style>
</head>
<body>
%s
</body>
</html>`, escapeHTML(tree.Name), css.String(), body)
}

// renderHTMLNode recursively emits HTML for a layout node, accumulating CSS
// rules keyed by an auto-incrementing class name.
func renderHTMLNode(node LayoutTree, css *strings.Builder, parentClass string) string {
	cls := uniqueClass(node.Name)
	bg := ""
	if len(node.Colors) > 0 {
		bg = node.Colors[0]
	}
	css.WriteString(fmt.Sprintf(".%s{", cls))
	if node.BBox != nil {
		css.WriteString(fmt.Sprintf("width:%.0fpx;height:%.0fpx;", node.BBox.Width, node.BBox.Height))
	}
	if bg != "" {
		css.WriteString(fmt.Sprintf("background:%s;", bg))
	}
	css.WriteString("box-sizing:border-box;position:relative;")
	css.WriteString("}\n")

	if node.Font != nil {
		css.WriteString(fmt.Sprintf(".%s{font-family:%s;font-size:%.0fpx;font-weight:%s;}\n",
			cls, fallback(node.Font.Family, "inherit"), node.Font.Size, fallback(node.Font.Weight, "normal")))
	}

	var inner strings.Builder
	for _, child := range node.Children {
		inner.WriteString(renderHTMLNode(child, css, cls))
	}
	if node.Text != "" {
		inner.WriteString(escapeHTML(node.Text))
	}

	tag := "div"
	if node.Type == "TEXT" {
		tag = "span"
	}
	return fmt.Sprintf(`<%s class="%s">%s</%s>`, tag, cls, inner.String(), tag)
}

// renderVue emits a single-file Vue 3 component (script setup + template).
func renderVue(tree LayoutTree, style, name string) string {
	var css strings.Builder
	body := renderHTMLNode(tree, &css, "")
	return fmt.Sprintf(`<script setup lang="ts">
// %s — generated from a design mockup by rexion-plugin-design.
</script>

<template>
%s
</template>

<style scoped>
%s</style>
`, name, indent(body, "  "), css.String())
}

// renderReact emits a functional React + TypeScript component.
func renderReact(tree LayoutTree, style, name string) string {
	var css strings.Builder
	body := renderHTMLNode(tree, &css, "")
	// Built with string concatenation because the styles template literal uses a
	// backtick, which can't appear inside a Go raw-string literal.
	return "// " + name + " — generated from a design mockup by rexion-plugin-design.\n" +
		"import React from \"react\";\n\n" +
		"export function " + name + "(): React.ReactElement {\n" +
		"  return (\n" +
		"    <>\n" +
		indent(body, "      ") + "\n" +
		"    </>\n" +
		"  );\n" +
		"}\n\n" +
		"export default " + name + ";\n\n" +
		"const styles = `\n" +
		css.String() + "`;\n"
}

// --- small render helpers ---

var classCounter int

func uniqueClass(name string) string {
	classCounter++
	safe := strings.ToLower(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, name))
	if safe == "" || strings.Trim(safe, "-") == "" {
		safe = "el"
	}
	return fmt.Sprintf("d%d-%s", classCounter, safe)
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

func fallback(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

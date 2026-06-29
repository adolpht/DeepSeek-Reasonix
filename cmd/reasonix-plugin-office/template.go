package main

import (
	"bytes"
	"fmt"
	"os"
	"text/template"
)

// renderTemplateTool renders a Go text/template against a caller-supplied
// variable map. Read-only: it only writes to its return value.
//
// Why text/template over a third-party engine? It ships with the stdlib, so the
// single-binary story stays intact and the template syntax is well-documented
// at https://pkg.go.dev/text/template. The agent can compose meeting agendas,
// weekly reports, and similar documents without needing a heavyweight Markdown
// engine or yet another dependency.
var renderTemplateTool = toolDef{
	name: "render_template",
	description: "Render a Go text/template against a variables map. " +
		"Use `template_path` to load a .tmpl/.md file, or `template` for inline source. " +
		"Variables: keys map to {{.key}} references. Returns the rendered text.",
	readOnly: true,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"template":      map[string]any{"type": "string", "description": "Inline template source (mutually exclusive with template_path)"},
			"template_path": map[string]any{"type": "string", "description": "Absolute path to a .tmpl/.md/.txt template file"},
			"variables": map[string]any{
				"type":                 "object",
				"description":          "JSON object of variable name → value (string|number|bool|nested object|array)",
				"additionalProperties": true,
			},
		},
		"required": []string{},
	},
	run: runRenderTemplate,
}

func runRenderTemplate(args map[string]any) (any, error) {
	tmplSrc := argStringDefault(args, "template", "")
	tmplPath := argStringDefault(args, "template_path", "")
	if tmplSrc == "" && tmplPath == "" {
		return nil, fmt.Errorf("either `template` (inline) or `template_path` (file) must be provided")
	}
	if tmplSrc != "" && tmplPath != "" {
		return nil, fmt.Errorf("`template` and `template_path` are mutually exclusive")
	}

	name := "inline"
	if tmplPath != "" {
		b, err := os.ReadFile(tmplPath)
		if err != nil {
			return nil, fmt.Errorf("read template file: %w", err)
		}
		tmplSrc = string(b)
		name = tmplPath
	}

	// variables may be nil/missing → empty data map.
	vars, _ := args["variables"].(map[string]any)
	if vars == nil {
		vars = map[string]any{}
	}

	tmpl, err := template.New(name).Option("missingkey=error").Parse(tmplSrc)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// toStringSlice converts an MCP JSON array arg into []string. Lives here because
// the office plugin doesn't carry the query.go file from the sheet plugin; a
// shared copy of the helper is intentionally small to keep the package self-contained.
func toStringSlice(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array, got %T", v)
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, ok := x.(string)
		if !ok {
			return nil, fmt.Errorf("array elements must be strings, got %T", x)
		}
		out = append(out, s)
	}
	return out, nil
}

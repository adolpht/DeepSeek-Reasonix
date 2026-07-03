package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"reasonix/internal/proc"
)

// mdToPDFTool converts Markdown (or a markdown-ish docx/html input pandoc can
// read) to a PDF file via the external pandoc binary. Only registered when
// probePandoc() found pandoc on PATH at startup.
//
// We shell out to pandoc rather than implement a PDF renderer in Go: pandoc is
// the de-facto standard, produces high-quality output, and supports LaTeX /
// wkhtmltopdf / weasyprint back-ends via --pdf-engine. The plugin stays a thin
// wrapper so users pick their own engine.
var mdToPDFTool = toolDef{
	name: "md_to_pdf",
	description: "Convert Markdown (or any pandoc-readable file) to PDF via the external pandoc binary. " +
		"Requires pandoc installed and on PATH. Pass pdf_engine to select LaTeX/wkhtmltopdf/weasyprint/etc. " +
		"Output path must end in .pdf.",
	readOnly: false,
	schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input":      map[string]any{"type": "string", "description": "Absolute path to the input .md/.html/.docx file"},
			"output":     map[string]any{"type": "string", "description": "Absolute output .pdf path"},
			"pdf_engine": map[string]any{"type": "string", "description": "Pandoc --pdf-engine, e.g. pdflatex|wkhtmltopdf|weasyprint|tectonic (optional, pandoc default if omitted)"},
			"extra_args": map[string]any{"type": "array", "description": "Extra pandoc CLI args, e.g. [\"-V\",\"geometry:margin=1in\"] (optional)"},
			"markdown":   map[string]any{"type": "string", "description": "Inline markdown content; if set, piped to pandoc via stdin and `input` is ignored"},
		},
		"required": []string{"output"},
	},
	run: runMDToPDF,
}

func runMDToPDF(args map[string]any) (any, error) {
	if pandocPath == "" {
		// Should not happen (tool not registered), but be defensive.
		return nil, fmt.Errorf("md_to_pdf unavailable: pandoc not found on PATH")
	}

	output, err := argString(args, "output")
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(strings.ToLower(output), ".pdf") {
		return nil, fmt.Errorf("output path must end with .pdf, got %q", output)
	}

	input := argStringDefault(args, "input", "")
	inlineMD := argStringDefault(args, "markdown", "")
	if input == "" && inlineMD == "" {
		return nil, fmt.Errorf("either `input` (path) or `markdown` (inline content) must be provided")
	}

	pdfEngine := argStringDefault(args, "pdf_engine", "")
	extraArgs, err := toStringSlice(args["extra_args"])
	if err != nil {
		return nil, fmt.Errorf("extra_args: %w", err)
	}

	// Build the pandoc command. Output is forced via -o; format inferred from extension.
	cmdArgs := []string{"-o", output}
	if pdfEngine != "" {
		cmdArgs = append(cmdArgs, "--pdf-engine", pdfEngine)
	}
	cmdArgs = append(cmdArgs, extraArgs...)

	var cmd *exec.Cmd
	if inlineMD != "" {
		// Pipe markdown via stdin.
		cmd = exec.Command(pandocPath, cmdArgs...)
		cmd.Stdin = strings.NewReader(inlineMD)
	} else {
		if _, statErr := os.Stat(input); statErr != nil {
			return nil, fmt.Errorf("stat input: %w", statErr)
		}
		cmdArgs = append(cmdArgs, input)
		cmd = exec.Command(pandocPath, cmdArgs...)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	proc.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		stderrTrim := strings.TrimSpace(stderr.String())
		if stderrTrim != "" {
			return nil, fmt.Errorf("pandoc failed: %w; stderr: %s", err, stderrTrim)
		}
		return nil, fmt.Errorf("pandoc failed: %w", err)
	}

	absOutput := output
	if abs, err := absPath(output); err == nil {
		absOutput = abs
	}
	return fmt.Sprintf("wrote PDF to %s", absOutput), nil
}

// absPath is a thin wrapper to absolutize output for the success message.
func absPath(p string) (string, error) {
	if strings.HasPrefix(p, "/") || (len(p) >= 2 && p[1] == ':') {
		return p, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return p, err
	}
	return strings.TrimRight(wd, "\\/") + string(os.PathSeparator) + p, nil
}

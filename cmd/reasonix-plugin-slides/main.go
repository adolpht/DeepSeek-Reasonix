// Command reasonix-plugin-slides is an MCP stdio server exposing PowerPoint
// (pptx) creation/editing/theming/export tools to Reasonix.
//
// This version uses python-pptx (MIT License, free open-source) instead of
// the commercial unioffice library. Python and python-pptx must be installed:
//
//	pip install python-pptx
//
// Protocol: newline-delimited JSON-RPC 2.0 on stdin/stdout. Logs go to stderr;
// stdout is reserved for JSON-RPC.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// version is overridable via -ldflags "-X main.version=...".
var version = "dev"

// pythonScriptPath is the path to ppt_gen.py (embedded beside this binary).
var pythonScriptPath string

func init() {
	// Find ppt_gen.py beside the binary
	exePath, err := os.Executable()
	if err == nil {
		pythonScriptPath = filepath.Join(filepath.Dir(exePath), "ppt_gen.py")
	}
	if pythonScriptPath == "" {
		pythonScriptPath = "ppt_gen.py" // fallback
	}
}

func main() {
	log.SetPrefix("reasonix-plugin-slides: ")
	log.SetFlags(0)
	probePython()
	if err := serve(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// --- JSON-RPC framing ---

type request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	protocolVersion    = "2024-11-05"
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

func serve(in *os.File, out *os.File) error {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	defer w.Flush()
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if rerr := handleLine(line, w); rerr != nil {
				return rerr
			}
			if ferr := w.Flush(); ferr != nil {
				return ferr
			}
		}
		if err != nil {
			return nil
		}
	}
}

func handleLine(line []byte, w *bufio.Writer) error {
	line = trimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		log.Printf("skipping unparseable line: %v", err)
		return nil
	}
	if req.ID == nil {
		return nil
	}

	resp := response{JSONRPC: "2.0", ID: *req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "reasonix-plugin-slides", "version": version},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolList()}
	case "tools/call":
		resp.Result, resp.Error = callTool(req.Params)
	default:
		resp.Error = &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// --- tools ---

type toolDef struct {
	name        string
	description string
	schema      map[string]any
	readOnly    bool
	run         func(args map[string]any) (any, error)
}

var tools = []toolDef{
	createPPTTool,
	addSlideTool,
	applyThemeTool,
	exportPDFTool,
}

func toolList() []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":        t.name,
			"description": t.description,
			"inputSchema": t.schema,
			"annotations": map[string]any{
				"readOnlyHint": t.readOnly,
				"title":        t.name,
			},
		})
	}
	return out
}

func callTool(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	for _, t := range tools {
		if t.name != p.Name {
			continue
		}
		result, err := t.run(p.Arguments)
		if err != nil {
			return textResult(err.Error(), true), nil
		}
		if s, ok := result.(string); ok {
			return textResult(s, false), nil
		}
		return result, nil
	}
	return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
}

func textResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// --- Tool definitions ---

var createPPTTool = toolDef{
	name: "create_ppt",
	description: "Create a PowerPoint presentation from a Markdown outline. " +
		"## headers become slide titles; content below each header becomes the slide body. " +
		"The first # header (if present) is used as the title slide. " +
		"Returns the path to the generated .pptx file. " +
		"(Uses python-pptx - free open-source, no license required)",
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
		"Modifies fonts and colors across all slides. " +
		"Available themes: professional (dark blue), creative (teal/orange), minimal (gray). " +
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

var exportPDFTool = toolDef{
	name: "export_pdf",
	description: "Export a PowerPoint presentation to PDF. " +
		"Requires LibreOffice installed for PDF conversion. " +
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

// --- Tool implementations (via Python) ---

func runPython(action string, args map[string]any) (string, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("marshal args: %w", err)
	}

	// Find Python
	pythonCmd := findPython()
	if pythonCmd == "" {
		return "", fmt.Errorf("Python not found. Install Python 3 to continue.")
	}

	// Ensure python-pptx is installed (auto-install if needed)
	if err := ensurePptxInstalled(pythonCmd); err != nil {
		return "", err
	}

	cmd := exec.Command(pythonCmd, pythonScriptPath, action, string(argsJSON))
	hideWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("python error: %s", string(ee.Stderr))
		}
		return "", fmt.Errorf("python execution: %w", err)
	}

	// Parse Python output
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return string(output), nil // Return raw output if not JSON
	}

	if errMsg, ok := result["error"].(string); ok && errMsg != "" {
		return "", fmt.Errorf(errMsg)
	}

	if msg, ok := result["message"].(string); ok && msg != "" {
		return msg, nil
	}

	if success, ok := result["success"].(bool); ok && success {
		if path, ok := result["path"].(string); ok {
			return fmt.Sprintf("created presentation at %s", path), nil
		}
		if pdfPath, ok := result["pdf_path"].(string); ok {
			return fmt.Sprintf("exported PDF to %s", pdfPath), nil
		}
		return "success", nil
	}

	return string(output), nil
}

func findPython() string {
	candidates := []string{"python3", "python", "py"}
	for _, c := range candidates {
		if _, err := exec.LookPath(c); err == nil {
			return c
		}
	}
	return ""
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

	if !strings.HasSuffix(strings.ToLower(outputPath), ".pptx") {
		return nil, fmt.Errorf("output path must end with .pptx, got %q", outputPath)
	}

	return runPython("create_ppt", map[string]any{
		"outline":     outline,
		"title":       argStringDefault(args, "title", ""),
		"style":       argStringDefault(args, "style", "professional"),
		"output_path": outputPath,
	})
}

func runAddSlide(args map[string]any) (any, error) {
	pptPath, err := argString(args, "ppt_path")
	if err != nil {
		return nil, err
	}
	title, err := argString(args, "title")
	if err != nil {
		return nil, err
	}

	return runPython("add_slide", map[string]any{
		"ppt_path": pptPath,
		"title":    title,
		"content":  argStringDefault(args, "content", ""),
		"layout":   argStringDefault(args, "layout", "title_content"),
		"notes":    argStringDefault(args, "notes", ""),
	})
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

	return runPython("apply_theme", map[string]any{
		"ppt_path": pptPath,
		"theme":    theme,
	})
}

func runExportPDF(args map[string]any) (any, error) {
	pptPath, err := argString(args, "ppt_path")
	if err != nil {
		return nil, err
	}

	return runPython("export_pdf", map[string]any{
		"ppt_path":    pptPath,
		"output_path": argStringDefault(args, "output_path", ""),
	})
}

// --- Helpers ---

func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	return s, nil
}

func argStringDefault(args map[string]any, key, def string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return def
	}
	if s == "" {
		return def
	}
	return s
}

func probePython() {
	python := findPython()
	if python == "" {
		log.Printf("Python not found. Install Python 3 to use the slides plugin.")
		return
	}

	// Check python-pptx
	cmd := exec.Command(python, "-c", "import pptx; print('ok')")
	output, err := cmd.Output()
	if err != nil || string(output) != "ok\n" {
		log.Printf("python-pptx not installed. Attempting auto-install via pip...")
		if autoInstallPptx(python) {
			log.Printf("python-pptx auto-installed successfully; slides plugin ready")
		} else {
			log.Printf("Auto-install failed. Please run manually: pip install python-pptx")
		}
		return
	}

	log.Printf("Python and python-pptx found; slides plugin ready (free open-source, no license required)")
}

// autoInstallPptx attempts to install python-pptx via pip automatically.
// Returns true if installation succeeded.
func autoInstallPptx(python string) bool {
	// Try pip first, then pip3
	pipCandidates := []string{"pip", "pip3"}

	// On Windows, also try python -m pip
	if _, err := exec.LookPath("pip"); err != nil {
		// pip not in PATH, try python -m pip
		pipCandidates = []string{}
	}

	for _, pip := range pipCandidates {
		if _, err := exec.LookPath(pip); err != nil {
			continue
		}
		cmd := exec.Command(pip, "install", "-q", "python-pptx")
		hideWindow(cmd)
		if err := cmd.Run(); err == nil {
			// Verify installation
			verify := exec.Command(python, "-c", "import pptx; print('ok')")
			if out, err := verify.Output(); err == nil && string(out) == "ok\n" {
				return true
			}
		}
	}

	// Fallback: use python -m pip
	cmd := exec.Command(python, "-m", "pip", "install", "-q", "python-pptx")
	hideWindow(cmd)
	if err := cmd.Run(); err != nil {
		log.Printf("python -m pip install failed: %v", err)
		return false
	}

	// Verify installation
	verify := exec.Command(python, "-c", "import pptx; print('ok')")
	if out, err := verify.Output(); err == nil && string(out) == "ok\n" {
		return true
	}

	return false
}

// ensurePptxInstalled checks if python-pptx is available and auto-installs if not.
// Returns error if installation fails.
func ensurePptxInstalled(python string) error {
	// Quick check
	cmd := exec.Command(python, "-c", "import pptx; print('ok')")
	if out, err := cmd.Output(); err == nil && string(out) == "ok\n" {
		return nil // Already installed
	}

	// Not installed, attempt auto-install
	log.Printf("python-pptx not found, auto-installing...")
	if !autoInstallPptx(python) {
		return fmt.Errorf("python-pptx not installed and auto-install failed. Please run: pip install python-pptx")
	}
	return nil
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 && (b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r' || b[len(b)-1] == '\n') {
		b = b[:len(b)-1]
	}
	return b
}

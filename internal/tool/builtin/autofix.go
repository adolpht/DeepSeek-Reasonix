package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"rexion/internal/tool"
)

func init() { tool.RegisterBuiltin(autoFixCITool{}) }

// autoFixCITool pulls failed CI logs for a GitHub PR via `gh`, analyses the
// failures, and emits a structured fix plan. When auto_apply is true AND the
// caller has approved fixes through the ApprovalModal flow (signalled via the
// "rexion.autofix.approved" context key), each suggested fix is applied by
// delegating to the edit_file built-in. Without approval, or with auto_apply
// false (the default), it returns the analysis only — no files are touched.
type autoFixCITool struct{}

func (autoFixCITool) Name() string { return "auto_fix_ci" }

func (autoFixCITool) Description() string {
	return "Analyse failed CI checks on a GitHub PR and produce a fix plan. " +
		"Pulls failing checks via `gh pr checks --json`, fetches failed-step logs via " +
		"`gh run view --log-failed`, and returns a Markdown report: failing step, error " +
		"summary, and suggested fixes (file:line). Set auto_apply=true to apply the " +
		"suggested edits — each edit still requires approval through the ApprovalModal flow."
}

func (autoFixCITool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "pr_url":{"type":"string","description":"Full PR URL, e.g. https://github.com/owner/repo/pull/123"},
  "auto_apply":{"type":"boolean","description":"If true, apply suggested fixes via edit_file (still requires per-fix approval). Default false: analyse only."}
},
"required":["pr_url"]
}`)
}

// ReadOnly reports false: with auto_apply=true the tool mutates files. The
// agent keeps auto_fix_ci out of parallel batches so edit ordering is safe.
// With auto_apply=false there are no host effects, but ReadOnly is a static
// property of the tool type, not per-call, so we conservatively return false.
func (autoFixCITool) ReadOnly() bool { return false }

// approvalCtxKey is the context key the ApprovalModal flow sets to signal that
// the user has approved applying auto-fix edits for this turn. It mirrors the
// "rexion.worktree.*" string-key convention used by merge_worktree.go.
const approvalCtxKey = "rexion.autofix.approved"

// fixSuggestion is one proposed edit: a target location and the replacement.
type fixSuggestion struct {
	File       string `json:"file"`
	Line       int    `json:"line,omitempty"`
	OldSnippet string `json:"old_snippet"`
	NewSnippet string `json:"new_snippet"`
	Reason     string `json:"reason"`
}

// failureAnalysis is the structured payload embedded in the Markdown report.
type failureAnalysis struct {
	PRURL        string          `json:"pr_url"`
	FailedStep   string          `json:"failed_step"`
	CheckName    string          `json:"check_name"`
	ErrorSummary string          `json:"error_summary"`
	Suggestions  []fixSuggestion `json:"suggestions"`
	Applied      []string        `json:"applied,omitempty"`
}

func (t autoFixCITool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if err := checkGHInstalled(); err != nil {
		return "", err
	}

	var p struct {
		PRURL     string `json:"pr_url"`
		AutoApply bool   `json:"auto_apply"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.PRURL == "" {
		return "", fmt.Errorf("pr_url is required")
	}

	repo, num, err := resolvePRTarget(p.PRURL, 0, "")
	if err != nil {
		return "", err
	}

	// Step 1: find failing checks. gh pr checks exits non-zero on failures but
	// still prints JSON; we discard the error and parse the payload.
	checksOut, _ := runGH(ctx, "pr", "checks", fmt.Sprintf("%d", num),
		"--repo", repo, "--json", "name,state,bucket,link")
	var checks []ghCheck
	if checksOut != "" {
		_ = json.Unmarshal([]byte(checksOut), &checks)
	}

	failed := pickFailedChecks(checks)
	if len(failed) == 0 {
		// No failing checks: nothing to fix. Distinguish "all green" from
		// "no checks at all" so the model can decide whether to wait.
		rollup := rollupCIStatus(checks)
		return fmt.Sprintf("# CI Auto-Fix Report\n\nNo failing checks found for %s (CI status: %s).\n"+
			"Nothing to fix.", p.PRURL, rollup), nil
	}

	// Step 2: for the first failing check, pull the failed-step log. We pick
	// the first because a single CI run usually fails one job; iterating all
	// would duplicate the same root cause across near-identical logs.
	primary := failed[0]
	logs, logErr := fetchFailedLogs(ctx, primary.Link)
	if logErr != nil {
		// Log fetch can fail when the run is too old or the check is a
		// non-Actions external system. Still report which check failed so the
		// model can fall back to reading the diff.
		logs = fmt.Sprintf("(could not fetch failed logs: %v)", logErr)
	}

	analysis := t.analyseFailure(p.PRURL, primary, logs)

	// Step 3: optionally apply fixes. Approval is checked per-turn via context,
	// mirroring the merge_worktree.go pattern. Without approval we return the
	// report and let the model ask the user to approve.
	if p.AutoApply {
		approved, _ := ctx.Value(approvalCtxKey).(bool)
		if !approved {
			return t.renderReport(analysis, "auto_apply requested but no approval found in context — "+
				"present the plan to the user and re-run after they approve via the ApprovalModal"), nil
		}
		t.applySuggestions(ctx, &analysis)
	}

	return t.renderReport(analysis, ""), nil
}

// pickFailedChecks returns the checks whose state is FAILURE or whose bucket
// is "fail" (gh uses both depending on check type).
func pickFailedChecks(checks []ghCheck) []ghCheck {
	var out []ghCheck
	for _, c := range checks {
		state := strings.ToUpper(c.State)
		bucket := strings.ToLower(c.Bucket)
		if state == "FAILURE" || bucket == "fail" {
			out = append(out, c)
		}
	}
	return out
}

// fetchFailedLogs extracts the run ID from the check's link (the Actions URL
// contains /runs/<id>) and calls `gh run view --log-failed`. The log is huge
// and line-oriented; we return it raw and let analyseFailure grep for clues.
func fetchFailedLogs(ctx context.Context, link string) (string, error) {
	runID := extractRunID(link)
	if runID == "" {
		return "", fmt.Errorf("no Actions run ID found in check link %q", link)
	}
	out, err := runGH(ctx, "run", "view", runID, "--log-failed")
	if err != nil {
		return "", err
	}
	// Cap to a sane size so we don't blow the model's context window on a
	// giant log. 32 KiB is enough to capture the failing step's tail.
	const maxLog = 32 * 1024
	if len(out) > maxLog {
		out = "...\n" + out[len(out)-maxLog:]
	}
	return out, nil
}

// actionsRunRe matches a GitHub Actions run URL and captures the run ID.
// Example: https://github.com/owner/repo/actions/runs/1234567890
var actionsRunRe = regexp.MustCompile(`/actions/runs/(\d+)`)

func extractRunID(link string) string {
	if link == "" {
		return ""
	}
	m := actionsRunRe.FindStringSubmatch(link)
	if m == nil {
		return ""
	}
	return m[1]
}

// analyseFailure inspects the failed-step log for common, mechanically-fixable
// error patterns and produces structured suggestions. The patterns are
// deliberately conservative: each suggestion must point at a real file:line
// found in the log, never a guess.
func (t autoFixCITool) analyseFailure(prURL string, check ghCheck, logs string) failureAnalysis {
	a := failureAnalysis{
		PRURL:        prURL,
		CheckName:    check.Name,
		FailedStep:   check.Name,
		ErrorSummary: firstErrorLine(logs),
	}

	// Pattern 1: Go compile errors — `file.go:line:col: error: message`.
	for _, m := range goCompileRe.FindAllStringSubmatch(logs, -1) {
		a.Suggestions = append(a.Suggestions, fixSuggestion{
			File:       m[1],
			Line:       atoiSafe(m[2]),
			OldSnippet: "",
			NewSnippet: "",
			Reason:     "Go compile error: " + strings.TrimSpace(m[3]),
		})
	}

	// Pattern 2: generic `file:line: error:` (lint, tsc, ruff, etc.).
	for _, m := range genericFileLineRe.FindAllStringSubmatch(logs, -1) {
		// Skip duplicates already captured by the Go-specific pattern.
		if alreadySuggested(a.Suggestions, m[1], m[2]) {
			continue
		}
		a.Suggestions = append(a.Suggestions, fixSuggestion{
			File:   m[1],
			Line:   atoiSafe(m[2]),
			Reason: "Error reported at this location: " + strings.TrimSpace(m[3]),
		})
	}

	// Pattern 3: failing test name — `--- FAIL: TestXxx (N.NNs)`.
	for _, m := range goTestFailRe.FindAllStringSubmatch(logs, -1) {
		a.Suggestions = append(a.Suggestions, fixSuggestion{
			File:   "",
			Line:   0,
			Reason: "Failing test: " + m[1] + " — inspect the test and the code it exercises",
		})
	}

	if a.ErrorSummary == "" {
		a.ErrorSummary = "No single error line identified; see the raw log tail in the report."
	}
	return a
}

var (
	goCompileRe       = regexp.MustCompile(`(?m)^([^\s:]+\.go):(\d+):\d+:\s+(.+)$`)
	genericFileLineRe = regexp.MustCompile(`(?m)^([^\s:]+\.[a-z]+):(\d+):\s+(.+)$`)
	goTestFailRe      = regexp.MustCompile(`(?m)^--- FAIL:\s+(.+?)\s+\(`)
)

// alreadySuggested reports whether a file:line pair is already in the
// suggestion list, so the generic pattern doesn't duplicate the Go-specific one.
func alreadySuggested(s []fixSuggestion, file, line string) bool {
	for _, f := range s {
		if f.File == file && fmt.Sprintf("%d", f.Line) == line {
			return true
		}
	}
	return false
}

// firstErrorLine returns the first line of the log that looks like an error —
// the most useful one-line summary of why the step failed.
func firstErrorLine(logs string) string {
	for _, line := range strings.Split(logs, "\n") {
		t := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(t, "error") || strings.Contains(t, "fail") ||
			strings.Contains(t, "panic") || strings.Contains(t, "fatal") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func atoiSafe(s string) int {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0
	}
	return n
}

// applySuggestions delegates each suggestion to the edit_file built-in. We
// only apply suggestions that carry an OldSnippet (a concrete replacement);
// location-only suggestions (file:line with no snippet) are reported but not
// auto-applied, since guessing the exact text to replace is unsafe.
func (t autoFixCITool) applySuggestions(ctx context.Context, a *failureAnalysis) {
	ed, ok := tool.LookupBuiltin("edit_file")
	if !ok {
		return
	}
	for _, s := range a.Suggestions {
		if s.OldSnippet == "" {
			continue
		}
		args, _ := json.Marshal(map[string]string{
			"path":       s.File,
			"old_string": s.OldSnippet,
			"new_string": s.NewSnippet,
		})
		if _, err := ed.Execute(ctx, args); err != nil {
			a.Applied = append(a.Applied, fmt.Sprintf("FAILED %s:%d — %v", s.File, s.Line, err))
		} else {
			a.Applied = append(a.Applied, fmt.Sprintf("applied %s:%d", s.File, s.Line))
		}
	}
}

// renderReport produces the Markdown analysis report the model returns to the
// user. A trailing note explains the approval state when auto_apply was
// requested but not honoured.
func (t autoFixCITool) renderReport(a failureAnalysis, note string) string {
	// fence is a three-backtick Markdown code fence. Defined here as a const
	// so the format strings below stay single-line and compile (Go forbids
	// multi-line double-quoted string literals, and raw literals can't hold
	// backticks).
	const fence = "```"
	var b strings.Builder
	b.WriteString("# CI Auto-Fix Report\n\n")
	fmt.Fprintf(&b, "- **PR**: %s\n", a.PRURL)
	fmt.Fprintf(&b, "- **Failed check**: %s\n", a.CheckName)
	fmt.Fprintf(&b, "- **Error summary**: %s\n\n", a.ErrorSummary)

	if len(a.Suggestions) == 0 {
		b.WriteString("No mechanically-fixable patterns found in the failed log. " +
			"Inspect the failing step manually.\n")
	} else {
		b.WriteString("## Suggested fixes\n\n")
		for i, s := range a.Suggestions {
			fmt.Fprintf(&b, "%d. **%s", i+1, s.File)
			if s.Line > 0 {
				fmt.Fprintf(&b, ":%d", s.Line)
			}
			b.WriteString("**\n")
			fmt.Fprintf(&b, "   - %s\n", s.Reason)
			if s.OldSnippet != "" {
				// Render the old/new snippets as fenced code blocks, indented
				// under the bullet so they nest cleanly in the list item.
				b.WriteString("   - Replace:\n")
				b.WriteString("     " + fence + "\n")
				b.WriteString(indentBlock(s.OldSnippet, "        ") + "\n")
				b.WriteString("     " + fence + "\n")
				b.WriteString("   with:\n")
				b.WriteString("     " + fence + "\n")
				b.WriteString(indentBlock(s.NewSnippet, "        ") + "\n")
				b.WriteString("     " + fence + "\n")
			}
		}
	}

	if len(a.Applied) > 0 {
		b.WriteString("\n## Applied edits\n\n")
		for _, line := range a.Applied {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}

	if note != "" {
		fmt.Fprintf(&b, "\n> %s\n", note)
	}
	return b.String()
}

// indentBlock prefixes every line of s with pad, so multi-line code snippets
// inside the Markdown report stay indented under their bullet.
func indentBlock(s, pad string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

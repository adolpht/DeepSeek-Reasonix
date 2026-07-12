package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"rexion/internal/tool"
)

func init() { tool.RegisterBuiltin(prMonitorTool{}) }

// prMonitorTool queries the status of a GitHub Pull Request via the gh CLI:
// PR state (open/merged/closed), CI check rollup (pending/success/failure),
// and recent review comments. It shells out to `gh pr view --json` and
// `gh pr checks --json`; if gh is not installed or not authenticated, it
// returns a friendly error so the model can fall back to git-only review.
type prMonitorTool struct{}

func (prMonitorTool) Name() string { return "pr_monitor" }

func (prMonitorTool) Description() string {
	return "Monitor a GitHub Pull Request via `gh`: returns PR state (open/merged/closed), " +
		"CI check rollup (pending/success/failure), and recent comments. " +
		"Pass either pr_url (https://github.com/OWNER/REPO/pull/N) or pr_number + repo (OWNER/REPO). " +
		"Requires the GitHub CLI (gh) to be installed and authenticated."
}

func (prMonitorTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "pr_url":{"type":"string","description":"Full PR URL, e.g. https://github.com/owner/repo/pull/123"},
  "pr_number":{"type":"integer","description":"PR number (used with repo)"},
  "repo":{"type":"string","description":"Repository as owner/name, e.g. owner/repo (used with pr_number)"}
}
}`)
}

// ReadOnly is true: pr_monitor only reads PR state through gh, never mutates
// the PR, branch, or filesystem, so it never needs approval and parallelises
// with other read-only tools.
func (prMonitorTool) ReadOnly() bool { return true }

// ghPRURLRe matches a github.com PR URL and captures owner, repo, number.
var ghPRURLRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/pull/(\d+)/?$`)

// prMonitorResult is the structured JSON returned to the model. Field names
// are stable so downstream skills (review-pr --auto-fix) can parse them.
type prMonitorResult struct {
	PRNumber int          `json:"pr_number"`
	Repo     string       `json:"repo"`
	Title    string       `json:"title"`
	State    string       `json:"state"` // OPEN | MERGED | CLOSED
	URL      string       `json:"url"`
	CIStatus string       `json:"ci_status"` // pending | success | failure | neutral | unknown
	Checks   []ghCheck    `json:"checks"`
	Comments []commentOut `json:"comments"`
}

// commentOut is the flattened comment shape we expose: gh returns author as
// {"login": "..."}, but downstream skills just want the username string.
type commentOut struct {
	Author string `json:"author"`
	Body   string `json:"body"`
}

type ghCheck struct {
	Name   string `json:"name"`
	State  string `json:"state"`  // SUCCESS | FAILURE | PENDING | SKIPPED | NEUTRAL
	Bucket string `json:"bucket"` // pass | fail | pending | skipping
	Link   string `json:"link,omitempty"`
}

type ghComment struct {
	Author ghAuthor `json:"author"`
	Body   string   `json:"body"`
}

// ghAuthor mirrors gh's nested author object (`{"login": "name"}`).
type ghAuthor struct {
	Login string `json:"login"`
}

func (prMonitorTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if err := checkGHInstalled(); err != nil {
		return "", err
	}

	var p struct {
		PRURL    string `json:"pr_url"`
		PRNumber int    `json:"pr_number"`
		Repo     string `json:"repo"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	repo, num, err := resolvePRTarget(p.PRURL, p.PRNumber, p.Repo)
	if err != nil {
		return "", err
	}

	// gh pr view --json: state, title, url, comments. We request a bounded set
	// of fields so the JSON stays small and stable across gh versions.
	viewFields := "number,title,state,url,comments"
	viewOut, viewErr := runGH(ctx, "pr", "view", fmt.Sprintf("%d", num),
		"--repo", repo, "--json", viewFields)
	if viewErr != nil {
		return "", fmt.Errorf("gh pr view %d in %s: %w", num, repo, viewErr)
	}

	var view struct {
		Number   int         `json:"number"`
		Title    string      `json:"title"`
		State    string      `json:"state"` // OPEN | MERGED | CLOSED
		URL      string      `json:"url"`
		Comments []ghComment `json:"comments"`
	}
	if err := json.Unmarshal([]byte(viewOut), &view); err != nil {
		return "", fmt.Errorf("parse gh pr view output: %w", err)
	}

	// gh pr checks --json: name, state, bucket, link. Returns non-zero when
	// any check is failing, but still emits JSON on stdout; we ignore the
	// exit code and parse what we got.
	checksOut, _ := runGH(ctx, "pr", "checks", fmt.Sprintf("%d", num),
		"--repo", repo, "--json", "name,state,bucket,link")
	var checks []ghCheck
	if checksOut != "" {
		_ = json.Unmarshal([]byte(checksOut), &checks)
	}

	comments := make([]commentOut, 0, len(view.Comments))
	for _, c := range view.Comments {
		comments = append(comments, commentOut{Author: c.Author.Login, Body: c.Body})
	}

	result := prMonitorResult{
		PRNumber: view.Number,
		Repo:     repo,
		Title:    view.Title,
		State:    strings.ToUpper(view.State),
		URL:      view.URL,
		CIStatus: rollupCIStatus(checks),
		Checks:   checks,
		Comments: comments,
	}

	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(b), nil
}

// checkGHInstalled returns a friendly error if the gh CLI is missing. LookPath
// is enough: a missing gh is the only failure we can detect without forking,
// and authentication problems surface later from gh itself with a clear message.
func checkGHInstalled() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("GitHub CLI (gh) is not installed or not on PATH: %w\n"+
			"Install from https://cli.github.com and run `gh auth login`.", err)
	}
	return nil
}

// runGH runs `gh` with the given args under the caller's context, returning
// combined stdout. stderr is captured into the error message on failure so the
// model can self-correct (e.g. report "needs authentication" verbatim).
func runGH(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		// gh writes human-readable diagnostics to stderr; surface them.
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return string(out), fmt.Errorf("%s: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return string(out), err
	}
	return string(out), nil
}

// resolvePRTarget turns the (pr_url | pr_number+repo) inputs into a repo slug
// and PR number that gh understands. pr_url wins when both are supplied.
func resolvePRTarget(prURL string, prNumber int, repo string) (string, int, error) {
	if prURL != "" {
		m := ghPRURLRe.FindStringSubmatch(prURL)
		if m == nil {
			return "", 0, fmt.Errorf("pr_url does not look like a GitHub PR URL: %s", prURL)
		}
		owner, name, numStr := m[1], m[2], m[3]
		var num int
		if _, err := fmt.Sscanf(numStr, "%d", &num); err != nil {
			return "", 0, fmt.Errorf("parse PR number from url: %w", err)
		}
		return owner + "/" + name, num, nil
	}
	if prNumber <= 0 {
		return "", 0, fmt.Errorf("either pr_url or pr_number (with repo) is required")
	}
	if repo == "" {
		return "", 0, fmt.Errorf("repo (owner/name) is required when pr_number is used")
	}
	if !strings.Contains(repo, "/") {
		return "", 0, fmt.Errorf("repo must be owner/name, got %q", repo)
	}
	return repo, prNumber, nil
}

// rollupCIStatus reduces per-check states to a single rollup label matching
// GitHub's own PR checks summary: failure dominates, then pending, then success.
// Empty / nil checks return "unknown" rather than "success" so the caller
// doesn't mistake "no data" for "green".
func rollupCIStatus(checks []ghCheck) string {
	if len(checks) == 0 {
		return "unknown"
	}
	hasPending, hasFail := false, false
	for _, c := range checks {
		state := strings.ToUpper(c.State)
		bucket := strings.ToLower(c.Bucket)
		switch {
		case state == "FAILURE" || bucket == "fail":
			hasFail = true
		case state == "PENDING" || bucket == "pending":
			hasPending = true
		}
	}
	switch {
	case hasFail:
		return "failure"
	case hasPending:
		return "pending"
	default:
		return "success"
	}
}

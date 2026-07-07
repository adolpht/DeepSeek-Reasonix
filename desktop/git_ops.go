package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rexion/internal/proc"
)

var versionPathRegexp = regexp.MustCompile(`/(v\d+[a-z]*|v\d+\.\d+)(/)?$`)

// normalizeBaseURL ensures baseURL ends with an API version segment (e.g. /v1)
// so that /chat/completions resolves correctly. Mirrors the openai provider's
// normalizeBaseURL.
func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if versionPathRegexp.MatchString(baseURL) {
		return baseURL
	}
	return baseURL + "/v1"
}

// git_ops.go provides the desktop's source-control command surface: a thin
// wrapper around the git CLI that the Wails-bound App methods call into. Every
// command uses the active workspace's base directory via -C, and
// proc.HideWindowDetached prevents console flashes on Windows.

// --- view types (serialised to JSON for the frontend) ---

// GitStatusView is the enriched result of "git status" plus branch/remote info.
type GitStatusView struct {
	Branch       string          `json:"branch"`
	Upstream     string          `json:"upstream,omitempty"`
	Ahead        int             `json:"ahead"`
	Behind       int             `json:"behind"`
	Staged       []GitFileStatus `json:"staged"`
	Unstaged     []GitFileStatus `json:"unstaged"`
	Untracked    []GitFileStatus `json:"untracked"`
	Conflicted   []GitFileStatus `json:"conflicted"`
	StashCount   int             `json:"stashCount"`
	GitAvailable bool            `json:"gitAvailable"`
	GitErr       string          `json:"gitErr,omitempty"`
}

// GitFileStatus describes a single file's git status.
type GitFileStatus struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	X       string `json:"x"` // index status letter
	Y       string `json:"y"` // worktree status letter
}

// BranchView represents a git branch.
type BranchView struct {
	Name      string `json:"name"`
	IsCurrent bool   `json:"isCurrent"`
	IsRemote  bool   `json:"isRemote"`
	Upstream  string `json:"upstream,omitempty"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
}

// CommitView represents a single commit.
type CommitView struct {
	Hash      string   `json:"hash"`
	ShortHash string   `json:"shortHash"`
	Author    string   `json:"author"`
	Date      string   `json:"date"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body,omitempty"`
	Refs      []string `json:"refs,omitempty"`
}

// TagView represents a git tag.
type TagView struct {
	Name    string `json:"name"`
	Hash    string `json:"hash"`
	Subject string `json:"subject,omitempty"`
}

// GitDiffView is the result of a git diff query.
type GitDiffView struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Err     string `json:"err,omitempty"`
}

// StashEntryView represents a stash entry.
type StashEntryView struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

// GitOperationResult is the generic result for write operations.
type GitOperationResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// --- internal helpers ---

// gitRun executes a git command in the given directory and returns its output.
func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	proc.HideWindowDetached(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// gitRunRaw is like gitRun but returns raw bytes (for diff output).
func gitRunRaw(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	proc.HideWindowDetached(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// isGitRepo checks if the directory is inside a git repository.
func isGitRepo(dir string) bool {
	_, err := gitRun(dir, "rev-parse", "--git-dir")
	return err == nil
}

// --- Phase 1: read-only status queries ---

// gitStatusView builds a full GitStatusView for the workspace.
func gitStatusView(dir string) GitStatusView {
	out := GitStatusView{
		GitAvailable: true,
		Staged:       []GitFileStatus{},
		Unstaged:     []GitFileStatus{},
		Untracked:    []GitFileStatus{},
		Conflicted:   []GitFileStatus{},
	}
	if !isGitRepo(dir) {
		out.GitAvailable = false
		out.GitErr = "not a git repository"
		return out
	}

	// Branch and upstream info.
	if branch, err := gitRun(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		out.Branch = branch
	}
	if upstream, err := gitRun(dir, "rev-parse", "--abbrev-ref", "@{upstream}"); err == nil {
		out.Upstream = upstream
	}

	// Ahead/behind counts.
	if out.Upstream != "" {
		if counts, err := gitRun(dir, "rev-list", "--left-right", "--count", out.Upstream+"...HEAD"); err == nil {
			parts := strings.Fields(counts)
			if len(parts) >= 2 {
				out.Behind, _ = strconv.Atoi(parts[0])
				out.Ahead, _ = strconv.Atoi(parts[1])
			}
		}
	}

	// Stash count.
	if count, err := gitRun(dir, "stash", "list"); err == nil {
		for _, line := range strings.Split(count, "\n") {
			if strings.HasPrefix(line, "stash@{") {
				out.StashCount++
			}
		}
	}

	// Parse porcelain status.
	raw, err := gitRunRaw(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		out.GitErr = err.Error()
		return out
	}

	// Resolve repo root for relative paths.
	repoRoot := ""
	if root, err := gitRun(dir, "rev-parse", "--show-toplevel"); err == nil {
		repoRoot = root
	}

	entries := parseGitStatusPorcelainZ(raw)
	for _, entry := range entries {
		path := entry.Path
		if repoRoot != "" {
			path = workspaceRelPathFromGitStatus(repoRoot, dir, entry.Path)
		}
		if path == "" {
			continue
		}
		oldPath := entry.OldPath
		if repoRoot != "" && oldPath != "" {
			oldPath = workspaceRelPathFromGitStatus(repoRoot, dir, entry.OldPath)
		}

		// Status is exactly 2 chars: X (index) and Y (worktree). Do NOT trim.
		x := ""
		y := ""
		if len(entry.Status) >= 2 {
			x = string(entry.Status[0])
			y = string(entry.Status[1])
		} else if len(entry.Status) == 1 {
			x = string(entry.Status[0])
		}

		fs := GitFileStatus{
			Path:    path,
			OldPath: oldPath,
			X:       x,
			Y:       y,
		}

		// Classify: conflicted, staged, unstaged, or untracked.
		switch {
		case x == "U" || y == "U" || x == "A" && y == "A" || x == "D" && y == "D":
			out.Conflicted = append(out.Conflicted, fs)
		case x != "" && x != "?" && x != " ":
			// File has index changes — staged.
			out.Staged = append(out.Staged, fs)
			// If also modified in worktree, add to unstaged with the Y status.
			if y != "" && y != " " && y != "?" {
				unstagedCopy := fs
				unstagedCopy.X = " "
				out.Unstaged = append(out.Unstaged, unstagedCopy)
			}
		case y == "?":
			out.Untracked = append(out.Untracked, fs)
		case y != "" && y != " ":
			out.Unstaged = append(out.Unstaged, fs)
		}
	}

	return out
}

// gitBranches lists all branches (local and remote).
func gitBranches(dir string) []BranchView {
	out, err := gitRun(dir, "branch", "-vv", "--all")
	if err != nil {
		return nil
	}
	var branches []BranchView
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		isCurrent := strings.HasPrefix(line, "* ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "  ")

		// Parse: <name> [<upstream>] <ahead-behind> <commit subject>
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		isRemote := strings.HasPrefix(name, "remotes/")

		bv := BranchView{
			Name:      name,
			IsCurrent: isCurrent,
			IsRemote:  isRemote,
		}

		// Look for upstream in square brackets: [origin/main]
		for i, p := range parts {
			if strings.HasPrefix(p, "[") {
				upstream := strings.Trim(p, "[]")
				// Upstream may be "origin/main: ahead 2, behind 1"
				if colonIdx := strings.Index(upstream, ":"); colonIdx >= 0 {
					bv.Upstream = strings.TrimSpace(upstream[:colonIdx])
					abStr := strings.TrimSpace(upstream[colonIdx+1:])
					parseAheadBehind(abStr, &bv)
				} else {
					bv.Upstream = upstream
				}
				_ = i
				break
			}
		}

		branches = append(branches, bv)
	}
	return branches
}

func parseAheadBehind(s string, bv *BranchView) {
	s = strings.TrimSpace(s)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "ahead ") {
			bv.Ahead, _ = strconv.Atoi(strings.TrimPrefix(part, "ahead "))
		} else if strings.HasPrefix(part, "behind ") {
			bv.Behind, _ = strconv.Atoi(strings.TrimPrefix(part, "behind "))
		}
	}
}

// gitLog returns the last n commits.
func gitLog(dir string, n int) []CommitView {
	if n <= 0 {
		n = 50
	}
	format := "--format=%H%x00%h%x00%an%x00%aI%x00%s%x00%b%x00%D"
	out, err := gitRun(dir, "log", "-n", strconv.Itoa(n), format)
	if err != nil {
		return nil
	}
	var commits []CommitView
	for _, block := range strings.Split(out, "\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		parts := strings.SplitN(block, "\x00", 7)
		if len(parts) < 5 {
			continue
		}
		cv := CommitView{
			Hash:      parts[0],
			ShortHash: parts[1],
			Author:    parts[2],
			Date:      parts[3],
			Subject:   parts[4],
		}
		if len(parts) >= 6 {
			cv.Body = strings.TrimSpace(parts[5])
		}
		if len(parts) >= 7 {
			refs := strings.TrimSpace(parts[6])
			if refs != "" {
				cv.Refs = strings.Split(refs, ", ")
			}
		}
		commits = append(commits, cv)
	}
	return commits
}

// gitDiff returns the diff for a file (or all files if path is empty).
// If staged is true, shows the diff of the index against HEAD.
func gitDiff(dir string, path string, staged bool) GitDiffView {
	args := []string{"diff"}
	if staged {
		args = append(args, "--cached")
	}
	if path != "" {
		args = append(args, "--", path)
	}
	raw, err := gitRunRaw(dir, args...)
	if err != nil {
		return GitDiffView{Path: path, Err: err.Error()}
	}
	return GitDiffView{Path: path, Content: string(raw)}
}

// gitDiffSummaryForCommit returns a concise summary of changes suitable for
// generating a commit message. If there are staged changes, it shows the staged
// diff. Otherwise, it shows all unstaged and untracked changes so the AI can
// still suggest a commit message.
func gitDiffSummaryForCommit(dir string) string {
	status := gitStatusView(dir)

	var filePaths []string
	var stagedOnly bool

	if len(status.Staged) > 0 {
		for _, f := range status.Staged {
			filePaths = append(filePaths, f.Path)
		}
		stagedOnly = true
	} else {
		// No staged files — include all changes so AI can still generate a message.
		for _, f := range status.Unstaged {
			filePaths = append(filePaths, f.Path)
		}
		for _, f := range status.Conflicted {
			filePaths = append(filePaths, f.Path)
		}
		for _, f := range status.Untracked {
			filePaths = append(filePaths, f.Path)
		}
	}

	if len(filePaths) == 0 {
		return ""
	}

	var sb strings.Builder
	if stagedOnly {
		sb.WriteString("Staged files:\n")
	} else {
		sb.WriteString("Changed files (not yet staged):\n")
	}
	for _, f := range filePaths {
		sb.WriteString("  " + f + "\n")
	}
	sb.WriteString("\nDiff:\n")

	// Get the diff, truncated to ~8000 chars.
	const maxDiffLen = 8000

	if stagedOnly {
		// Staged diff is straightforward.
		raw, err := gitRunRaw(dir, "diff", "--cached")
		if err == nil {
			diff := string(raw)
			if len(diff) > maxDiffLen {
				diff = diff[:maxDiffLen] + "\n... (truncated)"
			}
			sb.WriteString(diff)
		}
	} else {
		// No staged files — gather diffs for unstaged and untracked files.
		// Unstaged (tracked) modifications.
		if raw, err := gitRunRaw(dir, "diff"); err == nil {
			diff := string(raw)
			if len(diff) > maxDiffLen {
				diff = diff[:maxDiffLen] + "\n... (truncated)"
			}
			sb.WriteString(diff)
		}
		// Untracked files: show their content (first N lines) since git diff
		// does not include them by default.
		for _, f := range status.Untracked {
			if sb.Len() > maxDiffLen {
				break
			}
			sb.WriteString("\n--- new file: " + f.Path + "\n")
			// Use git diff --no-index /dev/null to show the full new file.
			if raw, err := gitRunRaw(dir, "diff", "--no-index", "--", "/dev/null", f.Path); err == nil {
				content := string(raw)
				if len(content) > 2000 {
					content = content[:2000] + "\n... (truncated)"
				}
				sb.WriteString(content)
			}
		}
	}

	return sb.String()
}

// generateCommitMessage calls the OpenAI-compatible chat completion API to
// generate a commit message from the given diff summary.
func generateCommitMessage(ctx context.Context, baseURL, apiKey, model, diffSummary string) (string, error) {
	prompt := `你是一个擅长撰写简洁、准确的 git 提交消息的专家。
分析下面的代码变更，生成一条提交消息，要求：
1. 使用 conventional commits 格式：type(scope): description
   - type: feat/fix/refactor/docs/style/test/chore/perf
   - scope: 可选，受影响的模块或区域
2. 主题行：不超过72个字符，简要描述改了什么
3. 正文：必须包含，列出具体改动的要点（如新增的函数、修改的逻辑、新增的UI组件等），每条要点一行
4. 关注实际代码变更——函数名、新增功能、修复的bug等
5. 不要用代码块或引号包裹
6. 主题行用中文，正文也用中文

重要：提交消息必须反映 diff 中展示的具体变更，不要使用"同步变更"或"更新文件"等泛泛的消息。

变更内容：
` + diffSummary

	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type reqBody struct {
		Model       string  `json:"model"`
		Messages    []msg   `json:"messages"`
		Temperature float64 `json:"temperature"`
		MaxTokens   int     `json:"max_tokens"`
	}

	body, _ := json.Marshal(reqBody{
		Model: model,
		Messages: []msg{
			{Role: "system", Content: "你是一个提交消息生成器。只输出提交消息文本，不要输出其他内容。"},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
		MaxTokens:   512,
	})

	url := normalizeBaseURL(baseURL) + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from model")
	}

	message := strings.TrimSpace(result.Choices[0].Message.Content)
	// Strip markdown code block wrappers if the model wraps them.
	message = strings.TrimPrefix(message, "```")
	message = strings.TrimPrefix(message, "git\n")
	message = strings.TrimSuffix(message, "```")
	return strings.TrimSpace(message), nil
}

// gitRemotes returns the list of remote names and URLs.
func gitRemotes(dir string) map[string]string {
	out, err := gitRun(dir, "remote", "-v")
	if err != nil {
		return nil
	}
	remotes := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && strings.HasSuffix(parts[1], "(fetch)") || (len(parts) >= 2 && strings.Contains(parts[1], "(push)")) {
			// just take the first URL per remote
			if _, exists := remotes[parts[0]]; !exists {
				// parts[1] is the URL (may be followed by (fetch)/(push))
				url := parts[1]
				if strings.HasSuffix(url, "(fetch)") {
					url = strings.TrimSpace(strings.TrimSuffix(url, "(fetch)"))
				}
				if strings.HasSuffix(url, "(push)") {
					url = strings.TrimSpace(strings.TrimSuffix(url, "(push)"))
				}
				remotes[parts[0]] = url
			}
		}
	}
	return remotes
}

// --- Phase 2: write operations ---

// gitAdd stages the given paths. If paths is empty, stages all changes.
func gitAdd(dir string, paths []string) GitOperationResult {
	args := []string{"add"}
	if len(paths) == 0 {
		args = append(args, "-A")
	} else {
		args = append(args, "--")
		args = append(args, paths...)
	}
	_, err := gitRun(dir, args...)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitReset unstages the given paths. If paths is empty, unstages everything.
func gitReset(dir string, paths []string) GitOperationResult {
	args := []string{"reset"}
	if len(paths) == 0 {
		args = append(args, "HEAD")
	} else {
		args = append(args, "HEAD", "--")
		args = append(args, paths...)
	}
	_, err := gitRun(dir, args...)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitCommit creates a commit with the given message.
func gitCommit(dir string, message string) GitOperationResult {
	if strings.TrimSpace(message) == "" {
		return GitOperationResult{Success: false, Message: "commit message is empty"}
	}
	// Auto-stage all changes if nothing is staged yet.
	status := gitStatusView(dir)
	if len(status.Staged) == 0 {
		if len(status.Unstaged)+len(status.Untracked)+len(status.Conflicted) == 0 {
			return GitOperationResult{Success: false, Message: "no changes to commit"}
		}
		if _, err := gitRun(dir, "add", "-A"); err != nil {
			return GitOperationResult{Success: false, Message: "auto-stage failed: " + err.Error()}
		}
	}
	_, err := gitRun(dir, "commit", "-m", message)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitPush pushes to the remote. If upstream is empty, pushes the current branch
// with --set-upstream origin <branch>.
func gitPush(dir string, upstream string) GitOperationResult {
	if upstream != "" {
		_, err := gitRun(dir, "push", upstream)
		if err != nil {
			return GitOperationResult{Success: false, Message: err.Error()}
		}
		return GitOperationResult{Success: true}
	}
	// Push current branch with --set-upstream.
	branch, err := gitRun(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	_, err = gitRun(dir, "push", "--set-upstream", "origin", branch)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitPull pulls from the remote for the current branch.
func gitPull(dir string) GitOperationResult {
	_, err := gitRun(dir, "pull")
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitFetch fetches from all remotes.
func gitFetch(dir string) GitOperationResult {
	_, err := gitRun(dir, "fetch", "--all")
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitCheckout switches to the given branch or creates a new one.
func gitCheckout(dir string, branch string, create bool) GitOperationResult {
	args := []string{"checkout"}
	if create {
		args = append(args, "-b", branch)
	} else {
		args = append(args, branch)
	}
	_, err := gitRun(dir, args...)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitRestore discards working-tree changes for the given paths.
func gitRestore(dir string, paths []string) GitOperationResult {
	if len(paths) == 0 {
		return GitOperationResult{Success: false, Message: "no paths specified"}
	}
	args := append([]string{"restore", "--"}, paths...)
	_, err := gitRun(dir, args...)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitRestoreStaged unstages (restores from HEAD to index) the given paths.
func gitRestoreStaged(dir string, paths []string) GitOperationResult {
	if len(paths) == 0 {
		return GitOperationResult{Success: false, Message: "no paths specified"}
	}
	args := append([]string{"restore", "--staged", "--"}, paths...)
	_, err := gitRun(dir, args...)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitStashPush creates a new stash with an optional message.
func gitStashPush(dir string, message string) GitOperationResult {
	args := []string{"stash", "push"}
	if message != "" {
		args = append(args, "-m", message)
	}
	_, err := gitRun(dir, args...)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitStashPop applies and removes the stash at the given index.
func gitStashPop(dir string, index int) GitOperationResult {
	ref := fmt.Sprintf("stash@{%d}", index)
	_, err := gitRun(dir, "stash", "pop", ref)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitStashApply applies the stash at the given index without removing it.
func gitStashApply(dir string, index int) GitOperationResult {
	ref := fmt.Sprintf("stash@{%d}", index)
	_, err := gitRun(dir, "stash", "apply", ref)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitStashList returns the list of stash entries.
func gitStashList(dir string) []StashEntryView {
	out, err := gitRun(dir, "stash", "list")
	if err != nil || out == "" {
		return nil
	}
	var entries []StashEntryView
	for _, line := range strings.Split(out, "\n") {
		// Format: stash@{0}: On branch: message
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "stash@{") {
			continue
		}
		// Extract index.
		closeIdx := strings.Index(line, "}")
		if closeIdx < 0 {
			continue
		}
		idx, err := strconv.Atoi(line[7:closeIdx])
		if err != nil {
			continue
		}
		// Extract message (after first colon-space).
		msg := line
		if colonIdx := strings.Index(line, ": "); colonIdx >= 0 {
			msg = line[colonIdx+2:]
		}
		entries = append(entries, StashEntryView{
			Index:   idx,
			Message: msg,
		})
	}
	return entries
}

// gitDeleteBranch deletes the given branch.
func gitDeleteBranch(dir string, branch string, force bool) GitOperationResult {
	args := []string{"branch"}
	if force {
		args = append(args, "-D")
	} else {
		args = append(args, "-d")
	}
	args = append(args, branch)
	_, err := gitRun(dir, args...)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitRenameBranch renames the current branch.
func gitRenameBranch(dir string, newName string) GitOperationResult {
	_, err := gitRun(dir, "branch", "-m", newName)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitInit initialises a new git repository.
func gitInit(dir string) GitOperationResult {
	_, err := gitRun(dir, "init")
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// --- Phase 3: advanced operations ---

// gitMerge merges the given branch into the current branch.
func gitMerge(dir string, branch string, noFF bool) GitOperationResult {
	args := []string{"merge"}
	if noFF {
		args = append(args, "--no-ff")
	}
	args = append(args, branch)
	_, err := gitRun(dir, args...)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitRebase rebases the current branch onto the given branch.
func gitRebase(dir string, branch string) GitOperationResult {
	_, err := gitRun(dir, "rebase", branch)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitRebaseAbort aborts an in-progress rebase.
func gitRebaseAbort(dir string) GitOperationResult {
	_, err := gitRun(dir, "rebase", "--abort")
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitRebaseContinue continues an in-progress rebase.
func gitRebaseContinue(dir string) GitOperationResult {
	_, err := gitRun(dir, "rebase", "--continue")
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitCherryPick cherry-picks the given commit hash.
func gitCherryPick(dir string, hash string) GitOperationResult {
	_, err := gitRun(dir, "cherry-pick", hash)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitMergeAbort aborts an in-progress merge.
func gitMergeAbort(dir string) GitOperationResult {
	_, err := gitRun(dir, "merge", "--abort")
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitConflictFiles returns the list of files with merge conflicts.
func gitConflictFiles(dir string) []string {
	out, err := gitRun(dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil
	}
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// gitResolveConflict marks a file as resolved by adding it to the index.
func gitResolveConflict(dir string, path string) GitOperationResult {
	_, err := gitRun(dir, "add", "--", path)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitCheckoutOurs resolves a conflict by checking out "ours" version.
func gitCheckoutOurs(dir string, path string) GitOperationResult {
	_, err := gitRun(dir, "checkout", "--ours", "--", path)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	// Auto-mark as resolved.
	_, _ = gitRun(dir, "add", "--", path)
	return GitOperationResult{Success: true}
}

// gitCheckoutTheirs resolves a conflict by checking out "theirs" version.
func gitCheckoutTheirs(dir string, path string) GitOperationResult {
	_, err := gitRun(dir, "checkout", "--theirs", "--", path)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	// Auto-mark as resolved.
	_, _ = gitRun(dir, "add", "--", path)
	return GitOperationResult{Success: true}
}

// gitTags lists all tags.
func gitTags(dir string) []TagView {
	out, err := gitRun(dir, "tag", "-l", "--format=%(refname:short)%00%(objectname:short)%00%(subject)")
	if err != nil || out == "" {
		return nil
	}
	var tags []TagView
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 3)
		tv := TagView{Name: parts[0]}
		if len(parts) >= 2 {
			tv.Hash = parts[1]
		}
		if len(parts) >= 3 {
			tv.Subject = parts[2]
		}
		tags = append(tags, tv)
	}
	return tags
}

// gitCreateTag creates an annotated tag.
func gitCreateTag(dir string, name string, message string) GitOperationResult {
	args := []string{"tag"}
	if message != "" {
		args = append(args, "-a", name, "-m", message)
	} else {
		args = append(args, name)
	}
	_, err := gitRun(dir, args...)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitDeleteTag deletes a tag.
func gitDeleteTag(dir string, name string) GitOperationResult {
	_, err := gitRun(dir, "tag", "-d", name)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitRevert creates a revert commit for the given hash.
func gitRevert(dir string, hash string, noCommit bool) GitOperationResult {
	args := []string{"revert"}
	if noCommit {
		args = append(args, "--no-commit")
	}
	args = append(args, hash)
	_, err := gitRun(dir, args...)
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return GitOperationResult{Success: true}
}

// gitShowCommit returns the full diff of a specific commit.
func gitShowCommit(dir string, hash string) GitDiffView {
	raw, err := gitRunRaw(dir, "show", "--format=", hash)
	if err != nil {
		return GitDiffView{Err: err.Error()}
	}
	return GitDiffView{Content: string(raw)}
}

// gitFileHistory returns the commit log for a specific file.
func gitFileHistory(dir string, path string, n int) []CommitView {
	if n <= 0 {
		n = 20
	}
	format := "--format=%H%x00%h%x00%an%x00%aI%x00%s"
	out, err := gitRun(dir, "log", "-n", strconv.Itoa(n), format, "--", path)
	if err != nil {
		return nil
	}
	var commits []CommitView
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 5)
		if len(parts) < 5 {
			continue
		}
		commits = append(commits, CommitView{
			Hash:      parts[0],
			ShortHash: parts[1],
			Author:    parts[2],
			Date:      parts[3],
			Subject:   parts[4],
		})
	}
	return commits
}

// gitBlame returns blame info for a file (simplified: line-by-line).
func gitBlame(dir string, path string) string {
	out, err := gitRun(dir, "blame", "--porcelain", "--", path)
	if err != nil {
		return ""
	}
	return out
}

// gitLastCommitTime returns the timestamp of the last commit, for refresh checks.
func gitLastCommitTime(dir string) time.Time {
	out, err := gitRun(dir, "log", "-1", "--format=%aI")
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, out)
	if err != nil {
		return time.Time{}
	}
	return t
}

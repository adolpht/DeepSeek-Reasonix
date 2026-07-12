package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// WorktreeManager 管理子Agent的Git Worktree生命周期，实现多Agent工作区隔离。
type WorktreeManager struct {
	// repoRoot 是主仓库的根目录路径
	repoRoot string
}

// NewWorktreeManager 创建一个WorktreeManager。如果repoRoot不是Git仓库，返回nil。
func NewWorktreeManager(repoRoot string) *WorktreeManager {
	if !isGitRepo(repoRoot) {
		slog.Debug("worktree: not a git repository, skipping worktree isolation", "path", repoRoot)
		return nil
	}
	return &WorktreeManager{repoRoot: repoRoot}
}

// isGitRepo 检查给定路径是否为Git仓库。
func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// agentIDToDirName 将agentID转换为安全的目录名，替换/为-避免嵌套问题。
func agentIDToDirName(agentID string) string {
	return strings.ReplaceAll(agentID, "/", "-")
}

// agentIDToBranchName 将agentID转换为Git分支名，取短ID部分。
// agentID格式可能是 "parent/agent-xxxx" 或 "agent-xxxx"，
// 分支名格式为 "rexion/agent-<短id>"，其中短ID把/替换为-。
func agentIDToBranchName(agentID string) string {
	safe := agentIDToDirName(agentID)
	return "rexion/" + safe
}

// CreateWorktree 为子Agent创建隔离的Git Worktree工作区。
// 返回worktree的绝对路径；如果创建失败则返回空字符串和error。
// 失败时静默跳过，不影响Agent正常运行。
func (m *WorktreeManager) CreateWorktree(repoRoot, agentID string) (worktreePath string, err error) {
	dirName := agentIDToDirName(agentID)
	branchName := agentIDToBranchName(agentID)

	// Worktree路径格式：.rexion/worktrees/<agent-id>/
	relPath := filepath.Join(".rexion", "worktrees", dirName)
	worktreePath = filepath.Join(repoRoot, relPath)

	// 确保父目录存在
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		slog.Warn("worktree: failed to create parent directory", "path", filepath.Dir(worktreePath), "error", err)
		return "", fmt.Errorf("worktree parent dir: %w", err)
	}

	// 创建新分支并检出到worktree：git worktree add <path> -b <branch>
	cmd := exec.Command("git", "worktree", "add", worktreePath, "-b", branchName)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		// 分支可能已存在，尝试用已有分支创建worktree
		slog.Debug("worktree: branch create failed, trying with existing branch", "branch", branchName, "error", err, "output", string(out))
		cmd = exec.Command("git", "worktree", "add", worktreePath, branchName)
		cmd.Dir = repoRoot
		out, err = cmd.CombinedOutput()
		if err != nil {
			slog.Warn("worktree: failed to create worktree", "path", worktreePath, "branch", branchName, "error", err, "output", string(out))
			return "", fmt.Errorf("git worktree add: %w (%s)", err, strings.TrimSpace(string(out)))
		}
	}

	slog.Info("worktree: created worktree for agent", "agent", agentID, "path", worktreePath, "branch", branchName)
	return worktreePath, nil
}

// RemoveWorktree 清理指定worktree及其关联分支。
func (m *WorktreeManager) RemoveWorktree(worktreePath string) error {
	// 从worktree路径推导repoRoot：取 .rexion/worktrees 之前的路径
	repoRoot := findRepoRootFromWorktree(worktreePath)
	if repoRoot == "" {
		return fmt.Errorf("worktree: cannot determine repo root from path %s", worktreePath)
	}

	// git worktree remove <path>
	cmd := exec.Command("git", "worktree", "remove", worktreePath, "--force")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("worktree: failed to remove worktree", "path", worktreePath, "error", err, "output", string(out))
		return fmt.Errorf("git worktree remove: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	// 清理关联的本地分支
	// 从worktree路径提取agentID来推导分支名
	dirName := filepath.Base(worktreePath)
	branchName := "rexion/" + dirName
	delCmd := exec.Command("git", "branch", "-D", branchName)
	delCmd.Dir = repoRoot
	delOut, delErr := delCmd.CombinedOutput()
	if delErr != nil {
		// 分支删除失败不阻塞，可能已被清理或正在使用
		slog.Debug("worktree: failed to delete branch (non-fatal)", "branch", branchName, "error", delErr, "output", string(delOut))
	}

	slog.Info("worktree: removed worktree", "path", worktreePath)
	return nil
}

// MergeWorktree 将worktree的变更合并回目标分支。如果有冲突，返回冲突文件列表。
func (m *WorktreeManager) MergeWorktree(worktreePath, targetBranch string) error {
	repoRoot := findRepoRootFromWorktree(worktreePath)
	if repoRoot == "" {
		return fmt.Errorf("worktree: cannot determine repo root from path %s", worktreePath)
	}

	// 获取worktree对应的分支名
	dirName := filepath.Base(worktreePath)
	sourceBranch := "rexion/" + dirName

	// 如果未指定目标分支，使用当前分支
	if targetBranch == "" {
		cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git rev-parse HEAD: %w", err)
		}
		targetBranch = strings.TrimSpace(string(out))
	}

	// 切换到目标分支
	checkoutCmd := exec.Command("git", "checkout", targetBranch)
	checkoutCmd.Dir = repoRoot
	if out, err := checkoutCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s: %w (%s)", targetBranch, err, strings.TrimSpace(string(out)))
	}

	// 执行合并
	mergeCmd := exec.Command("git", "merge", sourceBranch, "--no-edit")
	mergeCmd.Dir = repoRoot
	out, err := mergeCmd.CombinedOutput()
	if err != nil {
		// 检查是否有冲突
		if isMergeConflict(string(out)) {
			conflicts := listConflictedFiles(repoRoot)
			return fmt.Errorf("merge conflict in files: %s", strings.Join(conflicts, ", "))
		}
		return fmt.Errorf("git merge %s: %w (%s)", sourceBranch, err, strings.TrimSpace(string(out)))
	}

	slog.Info("worktree: merged branch into target", "source", sourceBranch, "target", targetBranch)
	return nil
}

// ListWorktrees 列出所有活跃的rexion worktree路径。
func (m *WorktreeManager) ListWorktrees(repoRoot string) ([]string, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}

	var worktrees []string
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		// porcelain格式：每行 "worktree <path>"
		if strings.HasPrefix(line, "worktree ") {
			path := strings.TrimPrefix(line, "worktree ")
			// 只返回.rexion/worktrees下的worktree
			if strings.Contains(path, ".rexion"+string(filepath.Separator)+"worktrees") {
				worktrees = append(worktrees, path)
			}
		}
	}
	return worktrees, nil
}

// findRepoRootFromWorktree 从worktree路径推导主仓库根目录。
// worktree路径格式为 <repoRoot>/.rexion/worktrees/<agent-id>，
// 找到 .rexion/worktrees 的位置即可得到repoRoot。
func findRepoRootFromWorktree(worktreePath string) string {
	// 标准化路径
	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		return ""
	}

	// 向上查找包含 .rexion/worktrees 的目录
	dir := absPath
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			break // 到达根目录
		}
		// 检查当前目录是否是worktrees目录
		if filepath.Base(parent) == "worktrees" &&
			filepath.Base(filepath.Dir(parent)) == ".rexion" {
			return filepath.Dir(filepath.Dir(parent))
		}
		dir = parent
	}

	// 回退：直接用 git rev-parse 查找
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = absPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// mergeConflictRe 匹配git合并冲突输出中的冲突标记
var mergeConflictRe = regexp.MustCompile(`(?i)CONFLICT|Merge conflict`)

// isMergeConflict 检查git merge输出是否包含冲突
func isMergeConflict(output string) bool {
	return mergeConflictRe.MatchString(output)
}

// listConflictedFiles 列出当前有冲突的文件
func listConflictedFiles(repoRoot string) []string {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// --- Context注入 ---

// CtxKeyWorktreeManager 是context中WorktreeManager的key，供跨包使用。
const CtxKeyWorktreeManager = "rexion.worktree.manager"

// CtxKeyWorktreePaths 是context中agent→worktree路径映射的key，供跨包使用。
const CtxKeyWorktreePaths = "rexion.worktree.paths"

// WithWorktreeManager 将WorktreeManager注入context，供merge_worktree工具使用。
func WithWorktreeManager(ctx context.Context, wm *WorktreeManager) context.Context {
	return context.WithValue(ctx, CtxKeyWorktreeManager, wm)
}

// WorktreeManagerFromContext 从context获取WorktreeManager。
func WorktreeManagerFromContext(ctx context.Context) (*WorktreeManager, bool) {
	wm, ok := ctx.Value(CtxKeyWorktreeManager).(*WorktreeManager)
	return wm, ok
}

// WithWorktreePath 将指定agent的worktree路径注入context。
func WithWorktreePath(ctx context.Context, agentID, worktreePath string) context.Context {
	paths, _ := ctx.Value(CtxKeyWorktreePaths).(map[string]string)
	if paths == nil {
		paths = make(map[string]string)
	}
	copied := make(map[string]string, len(paths)+1)
	for k, v := range paths {
		copied[k] = v
	}
	copied[agentID] = worktreePath
	return context.WithValue(ctx, CtxKeyWorktreePaths, copied)
}

// WorktreePathFromContext 从context获取指定agent的worktree路径。
func WorktreePathFromContext(ctx context.Context, agentID string) (string, bool) {
	paths, ok := ctx.Value(CtxKeyWorktreePaths).(map[string]string)
	if !ok {
		return "", false
	}
	path, found := paths[agentID]
	return path, found
}

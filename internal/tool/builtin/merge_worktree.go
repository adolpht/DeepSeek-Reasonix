package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"rexion/internal/tool"
)

func init() { tool.RegisterBuiltin(mergeWorktree{}) }

// mergeWorktree 是内置工具，用于将子Agent的worktree变更合并回主分支。
// 通过context获取WorktreeManager和worktree路径（由agent包注入），无需直接导入agent包。
type mergeWorktree struct{}

// worktreeMerger 定义合并worktree所需的接口，由agent.WorktreeManager实现。
type worktreeMerger interface {
	MergeWorktree(worktreePath, targetBranch string) error
}

func (mergeWorktree) Name() string { return "merge_worktree" }

func (mergeWorktree) Description() string {
	return "Merge a child agent's worktree changes back to the main branch. Reports conflicts if any."
}

func (mergeWorktree) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "agent_id":{"type":"string","description":"ID of the child agent whose worktree to merge"},
  "branch":{"type":"string","description":"Target branch to merge into (default: current branch)"}
},
"required":["agent_id"]
}`)
}

// ReadOnly 返回false，因为merge操作会改变仓库状态。
func (mergeWorktree) ReadOnly() bool { return false }

func (mergeWorktree) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		AgentID string `json:"agent_id"`
		Branch  string `json:"branch"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.AgentID == "" {
		return "", fmt.Errorf("agent_id is required")
	}

	// 从context获取WorktreeManager（通过agent包注入的字符串key）
	wm, ok := ctx.Value("rexion.worktree.manager").(worktreeMerger)
	if !ok || wm == nil {
		return "", fmt.Errorf("worktree manager not available — worktree isolation is not active for this session")
	}

	// 从context获取worktree路径映射
	paths, ok := ctx.Value("rexion.worktree.paths").(map[string]string)
	if !ok {
		return "", fmt.Errorf("no worktree paths found in context — worktree isolation is not active")
	}

	worktreePath, found := paths[p.AgentID]
	if !found || worktreePath == "" {
		return "", fmt.Errorf("no worktree found for agent %q", p.AgentID)
	}

	// 执行合并
	err := wm.MergeWorktree(worktreePath, p.Branch)
	if err != nil {
		// 区分合并冲突和其他错误
		if strings.Contains(err.Error(), "merge conflict") {
			return fmt.Sprintf("Merge conflict detected for agent %s: %v\nResolve the conflicts manually, then commit to complete the merge.", p.AgentID, err), nil
		}
		return "", fmt.Errorf("merge worktree for agent %s: %w", p.AgentID, err)
	}

	targetBranch := p.Branch
	if targetBranch == "" {
		targetBranch = "current branch"
	}
	return fmt.Sprintf("Successfully merged worktree changes from agent %s into %s.", p.AgentID, targetBranch), nil
}

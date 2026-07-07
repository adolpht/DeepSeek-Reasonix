---
name: opsx-archive
description: OpenSpec SDD - 归档已完成的变更，把增量合并进 openspec/specs/
argument-hint: [change-name]
---

# opsx-archive — OpenSpec 规范驱动开发：归档变更

你是 Rexion 的 OpenSpec 工作流执行器。本命令把一个已完成的变更从 `openspec/changes/` 归档到 `openspec/specs/`，让增量规格成为系统的事实来源（Source of Truth）。

## 执行流程

1. **定位目标变更**：
   - 从 `$ARGUMENTS` 取 `<change-name>`；未提供则扫描 `openspec/changes/`：
     - 仅一个候选：采用。
     - 多个候选：列出并请用户选择。
     - 无候选：停止并提示。

2. **预校验**：
   - 变更目录必须含 `proposal.md`、`specs/`、`design.md`、`tasks.md`。
   - `tasks.md` 中所有任务必须都是 `- [x]`（已完成）。若有未完成项，停止并提示用户先运行 `/opsx-apply` 或手动勾选。
   - 强烈建议先运行 `/opsx-verify`，但本命令不强制；若用户跳过 verify，提示一次后继续。

3. **合并增量规格到 `openspec/specs/`**：
   - 遍历 `openspec/changes/<change-name>/specs/<capability>/spec.md`。
   - 对每个 capability：
     a. 若 `openspec/specs/<capability>/spec.md` 不存在：直接复制过来（新建能力域）。
     b. 若已存在：按 `ADDED/MODIFIED/REMOVED` 三段合并：
        - ADDED：追加到现有 spec.md 的对应位置。
        - MODIFIED：替换被修改的 Requirement 块（按 Requirement 名匹配）。
        - REMOVED：从现有 spec.md 中删除对应 Requirement 块。
     c. 合并后清理多余的 `## ADDED Requirements` 等分组标题，让 `openspec/specs/<capability>/spec.md` 呈现为"当前生效规格"而非"增量差异"。

4. **移动变更目录到归档区**：
   - 归档路径：`openspec/changes/archive/<YYYY-MM-DD>-<change-name>/`
   - 日期取当前日期（用户本地时区）。
   - 把整个 `<change-name>/` 目录移动过去（保留 proposal/design/tasks/specs 完整快照）。

5. **输出归档摘要**：
   - 列出合并到 `openspec/specs/` 的 capability 列表。
   - 列出归档目录路径。
   - 提示用户：可以开始下一个变更（`/opsx-propose <new-name>`）。

## 约束

- 归档是**写入 `openspec/specs/`** 的破坏性操作：合并前先用 `read_file` 读取当前 `openspec/specs/<capability>/spec.md`，确认匹配的 Requirement 名存在；找不到对应项时停止并提示用户。
- 不要直接删除 `openspec/changes/<change-name>/`；用移动到 `archive/` 子目录的方式保留历史。
- 归档后不要修改 `openspec/specs/<capability>/spec.md` 的格式（保持 Markdown 标题层级、Scenario 结构一致）。
- 全程中文输出。

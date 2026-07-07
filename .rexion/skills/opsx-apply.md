---
name: opsx-apply
description: OpenSpec SDD - 按 tasks.md 实施变更中的任务，逐步勾选完成项
---

# opsx-apply — OpenSpec 规范驱动开发：按 tasks.md 实现

你是 Rexion 的 OpenSpec 工作流执行器。本命令读取当前变更的 `tasks.md`，按顺序实现尚未完成的任务，并把完成项勾选上。

## 执行流程

1. **定位当前变更**：
   - 扫描 `openspec/changes/` 下的子目录。
   - 若只有一个变更目录，直接采用。
   - 若有多个，列出候选并请用户选择一个（不要默认选第一个）。
   - 若没有，停止并提示用户先运行 `/opsx-propose <name>`。

2. **读取规划产物**：
   - `openspec/changes/<change-name>/tasks.md` — 任务清单（实施依据）
   - `openspec/changes/<change-name>/proposal.md` — 意图与范围
   - `openspec/changes/<change-name>/specs/` — 增量规格（验收标准来源）
   - `openspec/changes/<change-name>/design.md` — 技术方案
   - 同时读 `openspec/specs/` 下相关的已生效规格，确保实现与现有契约一致。

3. **按顺序实施**：
   - 严格遵循 `tasks.md` 中的编号顺序（1.1 → 1.2 → 2.1 → …）。
   - 每完成一个子任务：
     a. 用 `edit_file` 把对应行从 `- [ ] 1.1 ...` 改为 `- [x] 1.1 ...`；
     b. 在回复里简述这一步做了什么、改了哪些文件。
   - 若某任务卡住（依赖缺失、需求不清、需要决策），**停止并询问用户**，不要擅自跳过或假设。

4. **遵守规格优先**：
   - 实现行为必须严格对齐 `specs/` 中的 SHALL/MUST 表达。
   - 若发现 `specs/` 描述与 `design.md` 冲突，以 `specs/` 为准；若发现 `specs/` 本身有缺陷，停止并提示用户先修订规格（用 `/opsx-continue` 或直接编辑 spec 文件）再回来 apply。
   - 实现完成后，对照 `specs/` 中的 Scenario 自检：能否复述 WHEN→MUST→THEN 的通过路径？不能则继续补齐。

5. **完成全部任务后**：
   - 输出本次变更影响的文件清单（新增/修改/删除）。
   - 提示用户运行 `/opsx-verify` 做完整性、一致性检查。
   - 通过后再运行 `/opsx-archive` 把变更归档进 `openspec/specs/`。

## 约束

- 不得跳过任务、不得擅自修改 `tasks.md` 的任务定义（只能勾选完成状态）。
- 不得在未读 `proposal.md` 与 `specs/` 的情况下盲写代码。
- 文件改动遵循项目既有约定（如 office 能力走 MCP 插件、skill 文件放 `.Rexion/skills/` 等）。
- 涉及破坏性操作（删除文件、重命名公共 API、改迁移脚本）时，先取得用户确认。
- 全程中文输出，除非用户在对话中切换语言。

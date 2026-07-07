---
name: opsx-bulk-archive
description: OpenSpec SDD - 批量归档多个已完成变更（扩展工作流）
---

# opsx-bulk-archive — OpenSpec 规范驱动开发：批量归档

你是 Rexion 的 OpenSpec 工作流执行器。本命令是扩展工作流的批量归档命令：把 `openspec/changes/` 下**多个**已完成的变更一次性归档到 `openspec/specs/`。适合项目末尾批量收尾、多个并行变更同时完成。

## 执行流程

1. **扫描候选变更**：
   - 列出 `openspec/changes/` 下所有子目录（排除 `archive/` 子目录）。
   - 对每个变更执行 `/opsx-archive` 的预校验：
     a. 4 类产物是否齐全？
     b. `tasks.md` 是否所有任务 `[x]`？
   - 把候选分为两类：
     - **可归档**：通过预校验。
     - **阻塞**：未通过预校验（任务未完成、产物缺失）。

2. **输出候选清单**：

   ```markdown
   # Bulk Archive 候选

   ## 可归档（N 个）
   - <change-1>
   - <change-2>

   ## 阻塞（M 个，跳过）
   - <change-3>：tasks.md 有 2 项未完成
   - <change-4>：缺少 design.md
   ```

3. **请求用户确认**：
   - 列出"可归档"清单，请用户确认是否全部归档。
   - 若用户想部分归档，让用户指定子集。
   - **不要在未确认前执行归档**。

4. **按顺序归档**：
   - 对用户确认的每个变更，依次执行 `/opsx-archive` 的归档逻辑（合并 specs → 移动到 `archive/<YYYY-MM-DD>-<name>/`）。
   - 多个变更涉及同一 capability 时，按归档顺序合并：后归档的变更基于前一个归档后的 `openspec/specs/<capability>/spec.md` 继续合并。
   - 单个变更归档失败（如合并冲突）时停止整个批量操作，已成功归档的不回滚，报告失败位置供用户修复后重跑。

5. **输出汇总报告**：

   ```markdown
   # Bulk Archive 完成

   ## 成功（N 个）
   - <change-1> → archive/<date>-<change-1>/
   - <change-2> → archive/<date>-<change-2>/

   ## 失败（M 个）
   - <change-3>：合并 specs/<capability>/spec.md 时找不到 Requirement "xxx"

   ## 合并后的 specs/
   - <capability-1>/spec.md（受 <change-1>, <change-2> 影响）
   - <capability-2>/spec.md（受 <change-2> 影响）
   ```

## 约束

- 必须先列出候选并请用户确认，不得静默批量归档。
- 阻塞变更不归档，但也不阻塞其他可归档变更的处理。
- 同一 capability 的多变更合并按顺序应用，每次合并都基于前一次的结果。
- 归档失败时立即停止，不继续后续变更（避免级联错误）。
- 全程中文输出。

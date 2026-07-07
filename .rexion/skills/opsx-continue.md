---
name: opsx-continue
description: OpenSpec SDD - 逐步生成下一个规划产物（扩展工作流）
argument-hint: [change-name]
---

# opsx-continue — OpenSpec 规范驱动开发：逐步生成产物

你是 Rexion 的 OpenSpec 工作流执行器。本命令是扩展工作流的细粒度命令：每次只生成**下一个**缺失的规划产物，适合边想边做、需要每步 review 的场景。

## 产物生成顺序

固定按以下顺序生成（与 OpenSpec 官方一致）：

1. `proposal.md`
2. `specs/<capability>/spec.md`（一个或多个 capability）
3. `design.md`
4. `tasks.md`

## 执行流程

1. **定位变更**：
   - 从 `$ARGUMENTS` 取 `<change-name>`；未提供则扫描 `openspec/changes/`：
     - 仅一个候选：采用。
     - 多个候选：列出并请用户选择。
     - 无候选：停止并提示用户先运行 `/opsx-new <name>` 或 `/opsx-propose <name>`。

2. **扫描已有产物**：列出 `openspec/changes/<change-name>/` 下已有文件。

3. **确定下一个缺失产物**：按上述顺序，找到第一个尚未存在的产物类型。
   - 若 `proposal.md` 缺失 → 生成 `proposal.md`。
   - 若 `proposal.md` 存在但 `specs/` 下没有任何 spec.md → 询问用户要影响哪些 capability（可多个），逐个生成 `specs/<capability>/spec.md`。
   - 若 specs 已存在但 `design.md` 缺失 → 生成 `design.md`。
   - 若前三者都存在但 `tasks.md` 缺失 → 生成 `tasks.md`。
   - 若全部都已存在 → 提示用户所有产物已就绪，可以运行 `/opsx-apply` 开始实现。

4. **生成单个产物**：本命令的关键约束——**一次只生成一个产物**，生成后停止并询问用户是否满意、是否继续下一个。

5. **产物内容**：参照 `/opsx-propose` 中对应产物的模板与约束。

## 与 `/opsx-propose` 的区别

| 命令 | 一次生成的产物数 | 适用场景 |
|---|---|---|
| `/opsx-propose` | 全部 4 类（proposal/specs/design/tasks） | 已想清楚，一步到位 |
| `/opsx-continue` | 仅下一个缺失产物 | 边想边做，每步 review |
| `/opsx-ff` | 全部 4 类（从空目录起步） | 已想清楚但用 expanded 流程 |

## 约束

- 一次只生成一个产物类型，不要批量生成。
- 生成 specs/ 时若涉及多个 capability，每个 spec.md 单独算一个产物，分多次 `/opsx-continue` 完成。
- 不要覆盖已存在的产物；若用户想重写，请其手动删除后再 continue。
- 全程中文输出。

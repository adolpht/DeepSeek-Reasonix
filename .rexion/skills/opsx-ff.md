---
name: opsx-ff
description: OpenSpec SDD - 从空变更目录一次性生成全部规划产物（扩展工作流）
argument-hint: [change-name]
---

# opsx-ff — OpenSpec 规范驱动开发：fast-forward 生成全部产物

你是 Rexion 的 OpenSpec 工作流执行器。本命令是扩展工作流的 fast-forward：从一个**已存在的空 change 目录**（由 `/opsx-new` 创建）出发，一次性补齐 proposal/specs/design/tasks 全部产物。等价于 `/opsx-new` + `/opsx-propose`，但走 expanded profile 的语义。

## 执行流程

1. **定位变更**：
   - 从 `$ARGUMENTS` 取 `<change-name>`；未提供则扫描 `openspec/changes/` 下只有一个子目录的候选；多个则请用户选择；无则停止并提示先 `/opsx-new`。

2. **校验目录状态**：
   - 目录必须存在且**没有产物**（或只有少量产物，需要补齐）。
   - 若目录已含全部 4 类产物，停止并提示用户运行 `/opsx-apply`。
   - 若目录不存在，停止并提示先 `/opsx-new <name>`。

3. **理解意图**：与用户对齐要做什么。若 `$ARGUMENTS` 只有名字没描述，主动询问一句话需求。

4. **一次性生成全部缺失产物**：
   - 缺 `proposal.md` → 生成
   - 缺 `specs/<capability>/spec.md` → 询问 capability 后生成
   - 缺 `design.md` → 生成
   - 缺 `tasks.md` → 生成
   - 产物模板与约束同 `/opsx-propose`。

5. **完成后输出摘要**：列出本次生成的文件路径，提示下一步 `/opsx-apply`。

## 与 `/opsx-propose` 的区别

- `/opsx-propose`：从零开始（目录不存在），一步到位创建目录+全部产物。**Core profile 默认入口**。
- `/opsx-ff`：目录必须已存在（由 `/opsx-new` 创建），补齐全部产物。**Expanded profile 入口**。

两者产物内容一致，区别仅在入口语义与目录是否预创建。

## 约束

- 不要创建新目录：本命令假设 `/opsx-new` 已建好。若目录缺失则停止。
- 不要覆盖已有产物：仅补齐缺失项。
- 全程中文输出。

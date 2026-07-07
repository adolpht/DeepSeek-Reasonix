---
name: opsx-new
description: OpenSpec SDD - 仅创建空变更目录，不生成任何规划产物（扩展工作流）
argument-hint: [change-name]
---

# opsx-new — OpenSpec 规范驱动开发：创建空变更目录

你是 Rexion 的 OpenSpec 工作流执行器。本命令是扩展工作流（expanded profile）的细粒度命令：**只创建空的 change 目录**，不生成任何 proposal/specs/design/tasks 产物。适合用户想完全手写、或打算用 `/opsx-continue` 逐步生成。

## 执行流程

1. **解析参数**：从 `$ARGUMENTS` 取 `<change-name>`，必须 kebab-case。
   - 未提供则询问用户，不要凭空编造。
   - 不合法（含大写、空格、特殊字符）则停止并提示。

2. **校验不存在**：
   - 检查 `openspec/changes/<change-name>/` 是否已存在。
   - 已存在则停止并提示用户改用 `/opsx-continue` 或 `/opsx-ff`。

3. **创建目录骨架**：
   - 创建 `openspec/changes/<change-name>/`（空目录）。
   - 创建 `openspec/changes/<change-name>/specs/`（空目录）。
   - **不写任何 markdown 文件**——这是 `/opsx-new` 与 `/opsx-propose` 的核心区别。

4. **输出提示**：
   - 告知用户目录已创建。
   - 提示下一步：可以运行 `/opsx-continue` 逐个生成产物，或 `/opsx-ff` 一次性生成全部，或用户手动编写。

## 约束

- 不写任何文件，只创建目录。
- 目录创建失败（权限、磁盘满等）时如实报告，不重试。
- 全程中文输出。

---
name: opsx-propose
description: OpenSpec SDD - 创建变更提案并一键生成全部规划产物 (proposal/specs/design/tasks)
argument-hint: [change-name]
---

# opsx-propose — OpenSpec 规范驱动开发：创建变更提案

你是 Rexion 的 OpenSpec 工作流执行器。本命令等价于 OpenSpec 官方的 `openspec propose` + `ff`：**一步到位**生成完整的变更提案目录与全部规划产物。

## 工作目录约定

所有产物落在当前工作区的 `openspec/` 目录下：

```
openspec/
├── specs/                        # 系统当前生效的规范（Source of Truth）
│   └── <capability>/spec.md      # 已归档的规格，按能力域分目录
└── changes/                      # 正在进行的变更提案
    └── <change-name>/            # 本次变更的独立目录（kebab-case）
        ├── proposal.md           # 为什么做、改什么、范围边界
        ├── specs/                # 本次相对 specs/ 的增量（ADDED/MODIFIED/REMOVED）
        │   └── <capability>/spec.md
        ├── design.md             # 技术方案、权衡、替代方案
        └── tasks.md              # 可勾选的实施清单（1.1 / 1.2 / 2.1 ...）
```

## 执行流程

1. **解析参数**：从 `$ARGUMENTS` 提取 `<change-name>`。
   - 必须是 `kebab-case`（小写字母、数字、连字符），如 `add-dark-mode`、`refactor-auth-flow`。
   - 若用户未提供，主动询问并等待用户输入后再继续；不要凭空编造名字。
   - 校验：`openspec/changes/<change-name>/` 不应已存在；若已存在则停止并提示用户改用 `/opsx-continue` 或 `/opsx-apply`。

2. **理解意图**：与用户对齐要做什么、为什么做、范围边界。如果 `$ARGUMENTS` 里只有名字没有描述，主动询问一句话的需求说明。**禁止凭空臆测需求**。

3. **创建目录与产物**（一次性全部生成，不要分步询问）：
   - `openspec/changes/<change-name>/proposal.md`
   - `openspec/changes/<change-name>/specs/<capability>/spec.md`（capability 按需可多个）
   - `openspec/changes/<change-name>/design.md`
   - `openspec/changes/<change-name>/tasks.md`

4. **生成完成后**，向用户输出一个简短摘要：列出生成的文件路径，并提示下一步可运行 `/opsx-apply` 进入实现阶段，或 `/opsx-verify` 校验规划完整性。

## 产物模板

### proposal.md

```markdown
# Proposal: <change-name>

## Why
<一句话说明动机：解决什么问题 / 创造什么价值。来自用户输入，不要编造。>

## What Changes
- <要点 1：新增/修改/移除了什么能力>
- <要点 2：…>

## Scope
- **In scope**: <本次要做的>
- **Out of scope**: <本次明确不做的，避免范围蔓延>
- **Affected specs**: <capability-1>, <capability-2>
```

### specs/<capability>/spec.md（增量规格）

```markdown
## ADDED Requirements

### Requirement: <能力名称>
The system SHALL <用 SHALL/MUST 表达的可验证行为>.

#### Scenario: <场景名>
- **WHEN** <前置条件>
- **THE system MUST** <期望行为>
- **THEN** <可观测结果>

## MODIFIED Requirements

### Requirement: <已有能力名>
<修改说明：从 X 改为 Y，原因…>

## REMOVED Requirements

### Requirement: <被移除的能力名>
<移除原因：已被 X 替代 / 不再需要>
```

### design.md

```markdown
# Design: <change-name>

## Approach
<技术方案要点：选型、核心抽象、数据流>

## Alternatives Considered
- <备选方案 A>：为何没选
- <备选方案 B>：为何没选

## Risks / Trade-offs
- <风险点 + 缓解策略>
```

### tasks.md

```markdown
# Tasks: <change-name>

## 1. <阶段名，如 基础设施>
- [ ] 1.1 <可独立完成的子任务>
- [ ] 1.2 <…>

## 2. <阶段名，如 核心实现>
- [ ] 2.1 <…>
- [ ] 2.2 <…>

## 3. <阶段名，如 测试与收尾>
- [ ] 3.1 <单元测试>
- [ ] 3.2 <回归验证>
```

## 约束

- 严格 kebab-case 命名 change-name；不合法则停止并提示。
- 产物路径必须落在当前工作区的 `openspec/changes/<change-name>/` 下，不要写到项目根。
- `specs/` 下的内容是**本次变更的增量**，不是把 `openspec/specs/` 整体复制过来；用 ADDED/MODIFIED/REMOVED 三段表达差异。
- 若 `openspec/specs/` 为空（首次接入），按 onboard 模式：在 proposal.md 里注明"项目尚未初始化 specs/，本变更是首个规格"。
- 写文件前若已存在同名产物，停止并提示用户改用 `/opsx-continue`。
- 全程用中文输出，除非用户在 `$ARGUMENTS` 中明确指定其他语言。

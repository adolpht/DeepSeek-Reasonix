---
name: opsx-onboard
description: OpenSpec SDD - 给存量项目做 SDD 冷启动，扫描代码生成首批 specs/
---

# opsx-onboard — OpenSpec 规范驱动开发：存量项目冷启动

你是 Rexion 的 OpenSpec 工作流执行器。本命令是扩展工作流的冷启动命令：给一个**还没接入 OpenSpec** 的存量项目生成首批 `openspec/specs/` 规格，让项目可以从已有代码反推契约，而不是从空 specs/ 起步。

## 适用场景

- 项目已有大量代码，但没有任何规格文档。
- 想引入 SDD 但不知从何下手。
- 希望把现有行为固化成 specs/，后续变更都基于契约做增量。

## 执行流程

1. **确认工作区**：
   - 检查当前工作区根目录。
   - 若 `openspec/` 已存在且 `openspec/specs/` 非空，停止并提示用户"项目已接入 OpenSpec，无需 onboard"。
   - 若 `openspec/` 不存在或 `specs/` 为空，继续。

2. **扫描代码库**：
   - 用 `grep`/`list_dir`/`read_file` 探查项目结构：
     - 顶层目录、模块划分、主要入口。
     - 配置文件（`go.mod`/`package.json`/`Rexion.toml` 等）。
     - 测试目录、文档目录。
   - 不需要读完所有代码——只识别"能力域"（capability）的边界。

3. **识别能力域**：
   - 把项目按业务/功能模块划分为若干 capability（如 `auth`、`billing`、`agent-loop`、`mcp-server`）。
   - 每个 capability 对应 `openspec/specs/<capability>/spec.md`。
   - 数量控制在 3-8 个：太少粒度过粗，太多粒度过细。

4. **请求用户确认能力域划分**：
   - 列出建议的 capability 清单与各自的边界描述。
   - 请用户确认、增删、改名。
   - **不要在未确认前生成 specs/**。

5. **为每个 capability 生成 spec.md**：
   - 基于代码扫描结果，反推该能力域的：
     - 核心需求（Requirement，用 SHALL/MUST 表达）
     - 关键场景（Scenario，WHEN→MUST→THEN）
   - 每个 capability 至少 3 个 Requirement、每个 Requirement 至少 1 个 Scenario。
   - 不要凭空创造代码中不存在的需求；只描述已观测到的行为。

6. **生成 onboard 报告**：

   ```markdown
   # Onboard 完成

   ## 已生成 specs/
   - openspec/specs/<capability-1>/spec.md（N 个 Requirement）
   - openspec/specs/<capability-2>/spec.md（M 个 Requirement）

   ## 识别的边界假设
   - <在生成 specs 时做出的假设，需用户复核>
   - <代码中模糊不清处，已按最合理推断处理>

   ## 下一步
   - 请复核 specs/ 内容，确认与实际行为一致。
   - 复核后，可以开始用 `/opsx-propose <name>` 提出第一个变更。
   - 后续变更都会基于这些 specs 做增量。
   ```

## 约束

- 只生成 `openspec/specs/`，不创建 `openspec/changes/`（那是后续变更用的）。
- specs 内容必须基于代码事实，不得编造未实现的需求。
- 能力域划分必须用户确认后才生成 specs。
- 扫描代码时优先读公开入口（main、API handler、exported types），避免读入实现细节。
- 全程中文输出。

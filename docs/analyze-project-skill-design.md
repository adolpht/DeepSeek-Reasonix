# 项目文档自动生成 Skill 实现方案

## 一、目标分析

现有 `content/` 目录是对一个老旧项目（HxCapital 支付系统）的深度解读文档，包含 **10 大模块、60+ 篇 Markdown 文档**，每篇都有统一结构：

- `<cite>` 引用文件清单
- 目录、简介、项目结构、核心组件、架构总览（Mermaid 图）、详细组件分析、依赖分析、性能考量、故障排查、结论
- 每个章节标注了源码行号来源

目标：在 Reasonix Agent 中实现一个 **自动分析老旧项目并生成类似文档体系** 的能力，便于接手和分析老旧项目。

---

## 〇、测试结果差距分析（contenttestresult vs content）

基于对 `contenttestresult/`（Skill 产出）与 `content/`（目标标准）的逐篇对比，识别出以下 **7 个核心差距**：

### 差距 1：文档目录结构不完整

| 维度 | content（目标） | contenttestresult（产出） |
|------|:---:|:---:|
| 模块子文档 | 每个模块拆分为 3-7 个子文档 | 每个模块仅 1 个主文档 |
| 嵌套层级 | 架构设计有 `分层架构设计/服务层设计/` 三级嵌套 | 无嵌套目录 |
| 总文档数 | 60+ 篇 | 16 篇 |

**根因**：Skill prompt 中 Phase 4 虽然提到了 "For sub-modules, generate both a summary doc AND per-topic docs"，但未强制要求，LLM 倾向于生成单个大文档。

### 差距 2：章节结构缺少固定章节

| 差距 | 目标 | 产出 |
|------|:---:|:---:|
| 章节数 | 固定 10 章（含附录） | 6-8 章（缺少附录） |
| 附录 | 每个文档都有 "附录：API 接口文档" 等 | 无 |
| 性能考量 | 有源码行号引用 + 具体分析 | 泛泛而谈，无行号 |
| 故障排查 | 源码行号引用 + 排查步骤 | 较详细但缺少代码引用 |

**根因**：模板中章节数定义为 9 章，缺少第 10 章附录；且未强调每个章节都要有 "章节来源" 引用。

### 差距 3：代码引用深度不足

| 维度 | 目标 | 产出 |
|------|:---:|:---:|
| 伪代码块 | 有完整的伪代码逻辑块（含关键步骤注释） | 有但不够深入 |
| 行号精度 | 每条引用精确到行范围 `#L100-L137` | 部分精确，部分只到文件级 |
| 代码量 | 每个关键方法都有代码片段 | 仅核心方法有 |

**根因**：Skill prompt 中 "NEVER fabricate function signatures or logic" 过于严格，导致 LLM 不敢生成伪代码。应改为 "use read_file to get actual code, then create annotated pseudocode blocks with line references"。

### 差距 4：Mermaid 图丰富度不够

| 图类型 | 目标 | 产出 |
|------|:---:|:---:|
| `graph TB` 分层图 | 每篇都有 | 每篇都有 |
| `sequenceDiagram` 时序图 | 每个业务模块都有 | 仅消息通知、外部集成有 |
| `stateDiagram` 状态图 | 有状态机的模块都有 | 订单、积分有 |
| `erDiagram` ER 图 | 数据模型有 | 数据模型有 |
| `flowchart` 流程图 | 复杂流程有 | 积分有 |

**根因**：模板中只定义了 `graph TB`，未要求根据模块类型生成不同类型的图。

### 差距 5：负面声明规则缺失

目标文档中，每个事实性声明（如 "系统使用 HXException 统一异常处理"）都有源码行号引用。产出文档中部分做到了，部分没有。

**根因**：Skill prompt 中有 "Every factual claim MUST cite source file + line range" 规则，但未强调 **负面声明**（如 "系统不支持 PUT/DELETE"）也需要引用。

### 差距 6：章节来源规范不统一

- 目标：每章结尾有独立的 "章节来源" 节，列出 `[file:line](path#Lline)` 格式，且按章节编号
- 产出：部分有但不够规范，有的是在文档末尾统一列出

**根因**：模板中只有 "图表来源"，缺少 "章节来源" 的强制要求。

### 差距 7：文档间交叉引用缺失

- 目标：文档间有相互链接（如 订单管理系统.md 引用 积分管理系统.md）
- 产出：基本无交叉引用

**根因**：Phase 5 提到了交叉引用，但未在文档模板中要求添加 "相关文档" 节。

---

## 二、实现路径：Skill + Subagent

Reasonix 已有成熟的 Skill 扩展机制（`internal/skill/skill.go`），最佳路径是 **新增一个 `analyze-project` 内置 Skill**，以 subagent 模式运行，利用已有的 `explore`/`research` 子能力 + 工具链完成全流程。

### 架构总览

```
用户: /analyze-project 或 run_skill({name: "analyze-project", arguments: "/path/to/legacy-project"})

┌─────────────────────────────────────────────────────┐
│  analyze-project (subagent skill)                    │
│                                                      │
│  Phase 1: 项目扫描 ──────────────────────────────── │
│    ls / glob / read_file → 目录结构、技术栈识别      │
│    codegraph_search / codegraph_context → 符号索引   │
│                                                      │
│  Phase 2: 架构分析 ──────────────────────────────── │
│    codegraph_callers / callees / impact → 依赖图     │
│    read_file → 入口文件、配置文件、核心模块          │
│                                                      │
│  Phase 3: 分层归类 ──────────────────────────────── │
│    LLM 推理 → 识别分层模式、业务域划分               │
│    生成文档目录树 (content/ 结构)                     │
│                                                      │
│  Phase 4: 文档生成 ──────────────────────────────── │
│    对每个模块调用 explore subagent                    │
│    按模板生成 Markdown + Mermaid + cite               │
│    write_file → 输出到 <project>/content/             │
│                                                      │
│  Phase 5: 交叉引用 & 索引 ───────────────────────── │
│    生成 快速开始.md / 项目概述.md                    │
│    补全各文档间的链接关系                             │
└─────────────────────────────────────────────────────┘
```

---

## 三、详细实现方案

### 3.1 新增 Skill 定义

在 `internal/skill/builtins.go` 中新增：

```go
{
    Name:         "analyze-project",
    Description:  "Analyze a legacy project and generate a comprehensive documentation suite — architecture, data models, API references, module guides, deployment docs. Outputs a structured content/ directory with Mermaid diagrams and source citations.",
    Body:         builtinAnalyzeProjectBody,
    Scope:        ScopeBuiltin,
    Path:        "(builtin)",
    RunAs:        RunSubagent,
    AllowedTools: []string{"read_file", "ls", "glob", "grep", "bash", "write_file", "explore", "research"},
}
```

同时在 `BuiltinSubagentTools` 中注册专用顶层工具 `analyze_project`，与 `explore`/`review` 同级。

### 3.2 Skill Body（核心 Prompt）

这是最关键的部分——定义 subagent 的工作流程。以下是 prompt 的结构化设计：

```markdown
You are running as a project-analysis subagent. Given a target project path,
produce a comprehensive documentation suite that helps a new developer understand
and take over the codebase. Output goes to <project-root>/content/.

## Phase 1: Project Scanning (breadth-first)

1. `ls` the root → identify directory structure
2. Read manifest files: package.json / go.mod / pom.xml / Cargo.toml / requirements.txt
3. Read config files: tsconfig.json / .env.example / docker-compose.yml / Makefile
4. `glob **/*.{ts,js,go,py,java,rs}` → file inventory
5. `codegraph_search` for key symbols: main, App, Server, Router, Config, DB, Model

Goal: determine language, framework, architecture pattern, entry points.

## Phase 2: Architecture Analysis (depth-first on key files)

1. Read entry point (index.ts / main.go / app.py / Main.java)
2. `codegraph_context` on the entry point → call graph
3. `codegraph_callers` / `codegraph_callees` on top-level services
4. Identify layers: entry → service → data access → infrastructure
5. Identify cross-cutting: auth, error handling, logging, config

Goal: produce a layered architecture diagram and module responsibility map.

## Phase 3: Module Classification

Based on Phase 1+2, classify files into documentation modules:
- 项目概述/ (project overview, tech stack, deployment architecture)
- 架构设计/ (layered architecture, component interaction, tech choices)
- 数据模型设计/ (ORM models, entity relationships, DB schema)
- API接口参考/ (endpoints, request/response, auth)
- 核心工具类/ (utilities, helpers, shared infrastructure)
- 业务模块1/ (per business domain)
- 业务模块2/ ...
- 开发指南/ (setup, build, test, debug)
- 部署运维/ (deployment, monitoring, troubleshooting)
- 故障排除/ (common issues, performance tuning)

Generate the directory tree first, then proceed to Phase 4.

**STOP and report**: After generating the directory tree, STOP and output the full tree. 
Wait for the user to confirm (or auto-continue) before writing any documents.

## Phase 4: Document Generation (per module) — EXECUTION DISCIPLINE

**CRITICAL: DO NOT write all documents in one burst. Process modules one at a time.**

For EACH module, you MUST generate MULTIPLE documents, not just one. 
Follow these rules strictly:

### Per-Module Execution Rule

```
After completing each module's documents, STOP and output:
  "✅ 模块 {name} 完成: {n} 篇文档已生成。
  下一模块: {next_module_name}
  进度: {completed_count}/{total_count}"

Only proceed to the next module after the STOP marker.
This prevents quality degradation from long uninterrupted generation.
```

### Sub-document Splitting Rules

For each major business module, generate:
- **1 summary doc** (`{模块名}.md`) — overall architecture, core components, module relationships
- **N topic docs** — one per sub-topic within the module

Examples:
- `支付交易系统.md` + `充值管理.md` + `消费管理.md` + `冲销管理.md` + `余额管理.md` + `交易查询.md` + `交易安全机制.md`
- `架构设计.md` + `整体架构模式.md` + `技术栈选型.md` + `组件交互模式.md` + `分层架构设计/分层架构设计.md` + `分层架构设计/入口层设计.md` + `分层架构设计/服务层设计/服务层设计.md` + `分层架构设计/服务层设计/支付交易服务.md` + ...
- `API接口参考.md` + `支付交易API.md` + `用户管理API.md` + `系统管理API.md` + `认证授权API.md`

**Nested directories**: Use nested directories when a sub-module itself has sub-topics (e.g., `分层架构设计/服务层设计/`).

### Document Template (MANDATORY 10-Chapter Structure)

Every document MUST have exactly these 10 chapters:

```markdown
# {模块名称}

<cite>
**本文引用的文件**
- [filename](relative_path)
- ...
</cite>

## 目录
1. [简介](#简介)
2. [项目结构](#项目结构)
3. [核心组件](#核心组件)
4. [架构总览](#架构总览)
5. [详细组件分析](#详细组件分析)
6. [依赖关系分析](#依赖关系分析)
7. [性能考量](#性能考量)
8. [故障排查指南](#故障排查指南)
9. [结论](#结论)
10. [附录](#附录)

## 简介
{模块职责、核心能力、设计原则。2-3段，具体而非泛泛而谈}

## 项目结构
{文件列表 + Mermaid 组件关系图}

## 核心组件
{表格列出核心类/函数 + 职责描述}

## 架构总览
{至少2张Mermaid图，根据模块类型选择：}
- 必须: `graph TB` 分层/组件关系图
- 如果有流程: `sequenceDiagram` 时序图
- 如果有状态机: `stateDiagram-v2` 状态图
- 如果涉及实体: `erDiagram` ER图
- 如果有复杂分支: `flowchart TD` 流程图

图表来源
- [file:line](path#Lline)

## 详细组件分析
{逐个分析核心类/函数：签名、伪代码（从源码读取后简化）、关键决策、调用链}
- 每个核心方法都要有伪代码块（从源码读取后提取关键逻辑，用注释标注步骤）
- 伪代码块后标注 "> 来源：[file:line](path#Lline)"
- 禁止凭空编造代码，必须先 read_file 确认代码存在

## 依赖关系分析
{上游依赖、下游被依赖、外部依赖}
- 列出依赖的方向和原因
- 标注每个依赖关系的源码出处

## 性能考量
{分析具体的性能瓶颈、缓存策略、并发模型}
- 必须引用源码中的具体实现（行号）
- 提供可操作的优化建议

## 故障排查指南
{常见错误、排查路径}
- 列出具体的错误场景
- 给出排查步骤（带具体命令/操作）
- 标注错误处理代码的源码位置

## 结论
{1-2段总结}

## 附录
{至少一项附录内容：}
- API 接口文档（如有）
- 配置参考（如有）
- 术语表（如有）
- 变更历史（如有）

## 相关文档
{列出相关文档的链接，Phase 5 时补充完整}
- [相关模块1](path)
- [相关模块2](path)
```

### Mandatory Citation Rules

**CRITICAL — 违反以下规则视为不合格输出：**

1. **行号引用格式**：所有源码引用必须为 `[filename](relative_path#Lstart-Lend)` 格式
2. **每条事实性声明都要有引用**：包括正面声明（"系统使用 X"）和负面声明（"系统不支持 Y"）
3. **图表来源**：每个 Mermaid 图后必须紧跟 "图表来源" 节，列出图中每个节点对应的源码位置
4. **章节来源**：每个正文章节（2-9章）结束时必须添加 "章节来源" 节，列出本章所有引用的源码位置
5. **伪代码规则**：先用 `read_file` 读取源码确认存在，然后生成带注释的伪代码块，最后标注行号来源
6. **cite 块完整性**：文档顶部的 `<cite>` 块必须列出本文档引用的所有文件（去重）

### Mermaid Diagram Type Selection Guide

| 文档类型 | 必选图 | 可选图 |
|---------|--------|--------|
| 项目概述/架构设计 | `graph TB` 分层图 | `flowchart` 部署架构 |
| 业务模块 | `graph TB` 分层图 + `sequenceDiagram` 时序图 | `stateDiagram-v2` 状态机 |
| 数据模型设计 | `graph TB` 分层图 + `erDiagram` ER 图 | - |
| API 接口参考 | `sequenceDiagram` 时序图 | `graph TB` 分层图 |
| 开发指南/快速开始 | `graph TB` 分层图 | `flowchart` 流程 |
| 部署运维 | `graph TB` 架构图 | `flowchart` 应急流程 |
| 故障排除 | `flowchart` 排查流程 | `graph TB` 分层图 |
| 核心工具类 | `graph TB` 分层图 | `sequenceDiagram` |
| 缓存与任务调度 | `graph TB` 分层图 + `stateDiagram-v2` | - |

## Phase 5: Index, Cross-references & Quality Validation

1. Generate `快速开始.md` — quickstart guide with setup, build, run commands (10-chapter template)
2. Generate `项目概述/项目概述.md` — overall architecture with top-level Mermaid (10-chapter template)
3. **Cross-reference injection**: After ALL documents are generated, go back and:
   - Add "## 相关文档" section to every document with links to related docs
   - Verify all internal markdown links are valid (check file existence)
   - Ensure every document's `<cite>` block is complete and deduplicated
   - Add cross-references between business modules (e.g., 订单管理 → 积分管理, 订单管理 → 商品管理)
4. **Quality validation** — run the following checks using bash:
   ```bash
   # Check directory structure completeness
   echo "=== 文档统计 ==="
   echo "总 Markdown 文件数: $(find content/ -name '*.md' | wc -l)"
   echo ""

   # Check each document for required sections
   echo "=== 每篇文档结构检查 ==="
   for f in $(find content/ -name '*.md'); do
     name=$(basename "$f")
     chapters=$(grep -c '^## ' "$f")
     has_cite=$(grep -c '<cite>' "$f")
     mermaids=$(grep -c '```mermaid' "$f")
     sources=$(grep -c '图表来源\|章节来源' "$f")
     line_refs=$(grep -oP '\[.*?\]\(.*?#L\d+.*?\)' "$f" 2>/dev/null | wc -l)
     echo "[$chapters章/${has_cite}cite/${mermaids}图/${sources}来源/${line_refs}引用] $name"
     # Flag issues
     if [ "$chapters" -lt 8 ]; then echo "  ⚠️  章节数不足 (<8): $name"; fi
     if [ "$has_cite" -eq 0 ]; then echo "  ❌ 缺少 <cite>: $name"; fi
     if [ "$mermaids" -eq 0 ]; then echo "  ⚠️  无 Mermaid 图: $name"; fi
     if [ "$line_refs" -lt 3 ]; then echo "  ⚠️  行号引用不足 (<3): $name"; fi
   done
   ```

5. If validation finds issues, fix them before reporting completion.
6. **Invoke doc-reviewer subagent** for deep quality assurance:
   ```
   Now invoke the doc-reviewer subagent to verify and fix every document:
   
   run_skill({name: "doc-reviewer", arguments: "Review and fix all documents 
   in content/ for the project at <project-root>. Verify every claim against 
   source code, fix wrong citations, add missing chapters/sources/diagrams, 
   and ensure cross-references are complete."})
   ```
   
   The doc-reviewer will:
   - Read each .md file and cross-reference claims against actual source code
   - Fix inaccurate pseudocode, wrong line numbers, missing citations
   - Add missing Mermaid diagrams and verify existing ones
   - Complete cross-references between documents
   - Output a review report with fix summary
   
   After doc-reviewer completes, review its report. If major issues found, 
   fix them before finalizing.
7. Generate `content/README.md` listing all documents with one-line descriptions and a directory tree

## Output Rules

- Write all files to `<project-root>/content/` using `write_file`
- File naming: use Chinese for domain names (matching the user's convention), or English if the project is English-centric
- Each file should be 200-500 lines — detailed but not bloated
- If the project is too large (>50 source files), focus on the most important modules first and note what was skipped
- **Every document MUST have the 10-chapter structure** — do not skip chapters even if content seems sparse (write "暂无" if truly nothing to say)
- **Every chapter MUST end with "章节来源"** listing all source code references used in that chapter
- **Every Mermaid diagram MUST be followed by "图表来源"** with file:line references for each node
- Return a summary: how many docs generated, total files analyzed, any gaps
- **Quality self-check before writing**: verify each document has:
  - [x] 10 chapters (including 附录)
  - [x] `<cite>` block with all referenced files
  - [x] At least 2 Mermaid diagrams (for business modules)
  - [x] 图表来源 after each diagram
  - [x] 章节来源 after each chapter (2-9)
  - [x] 相关文档 section
  - [x] All citations use `#Lstart-Lend` format
  - [x] No fabricated code — all pseudocode has line references
```

### 3.3 代码改动清单

| 文件 | 改动 |
|------|------|
| `internal/skill/builtins.go` | 新增 `builtinAnalyzeProjectBody` 常量 + 在 `builtinSkills()` 中注册 `analyze-project` |
| `internal/skill/builtins.go` | 新增 `builtinDocReviewerBody` 常量 + 在 `builtinSkills()` 中注册 `doc-reviewer` |
| `internal/skill/tools.go` | 在 `BuiltinSubagentTools` 的 `specs` 切片中新增 `analyze_project` 条目 |
| `internal/skill/tools.go` | 在 `BuiltinSubagentTools` 的 `specs` 切片中新增 `doc_reviewer` 条目 |
| `internal/control/controller.go` | 无需改动（subagent 工具自动注册到 Registry） |

### 3.4 Skill 协作关系图

```
analyze-project (主 subagent)
  │
  ├── Phase 1-2: read_file / ls / glob / grep / codegraph_*
  ├── Phase 3: LLM 推理（无需工具）
  ├── Phase 4: 逐模块生成文档（write_file）
  │              └── 每个模块独立执行，完成后 STOP 报告进度
  │
  └── Phase 5: 质量保障
       ├── bash 脚本：结构化检查（章节数/Mermaid数/引用格式）
       ├── doc-reviewer subagent：深度校验 + 自动修正
       │    ├── read_file: 读取文档 + 源码对照
       │    ├── codegraph_*: 验证架构关系
       │    └── write_file: 覆盖修正后的文档
       └── 生成 README.md 索引
```

### 3.5 可选增强：分阶段执行

对于大型项目，单次 subagent 可能 token 超限。可设计为 **两阶段**：

**阶段 A — `analyze-project-scan`**（subagent，只读）：
- 扫描项目，输出一个 JSON 格式的分析计划（模块划分、文件分配、依赖图）
- 写入 `.reasonix/analysis-plan.json`

**阶段 B — `analyze-project-generate`**（inline，多轮）：
- 读取分析计划
- 逐模块调用 `explore` subagent 生成文档
- 每个模块一个独立 turn，避免单次 token 过载

### 3.6 与现有 Skill 的协作关系

```
analyze-project (主 subagent)
  │
  ├── Phase 1-2: 直接使用 read_file / ls / glob / grep / codegraph_*
  ├── Phase 3: LLM 推理（无需工具）
  ├── Phase 4: 逐模块生成文档（write_file）
  │              └── 每个模块独立执行，完成后 STOP 报告进度
  │
  └── Phase 5: 质量保障
       ├── bash 脚本：结构化检查
       ├── doc-reviewer subagent（新增）：深度校验 + 自动修正
       │    ├── read_file: 读取文档 + 源码对照
       │    ├── codegraph_*: 验证架构关系
       │    └── write_file: 覆盖修正后的文档
       └── 生成 README.md 索引
```

### 3.7 用户交互设计

用户可通过以下方式触发：

```bash
# CLI 方式
reasonix run "/analyze-project /path/to/legacy-project"

# Chat 方式
/analyze-project /path/to/legacy-project

# 或通过 run_skill
run_skill({name: "analyze-project", arguments: "/path/to/legacy-project"})
```

可选参数：
- `--language zh|en` — 文档语言（默认跟随项目语言）
- `--depth quick|standard|deep` — 分析深度（影响文档数量和详细程度）
- `--output content/` — 输出目录（默认 `<project>/content/`）

---

## 四、执行保障：prompt 之外的约束机制

纯 prompt 约束存在 "LLM 偷懒/遗忘" 的风险，需要从以下三个层面补充约束机制：

### 4.1 结构层面：分阶段 + 分模块执行（防治"大文档综合症"）

**问题**：当 LLM 面对 "生成全部文档" 的单次任务时，倾向于牺牲质量追求速度，产出单一大文档而非拆分的子文档。

**方案**：将 Phase 4 从 "一次 subagent 全做" 改为 **多轮分批执行**：

```
Phase 3 产出: analysis-plan
├── 模块清单 (JSON)
├── 每个模块的子文档计划
└── 预估行数

Phase 4 执行方式（方案 A — 推荐）:
  对每个模块，发起独立的文档生成指令:
  - "为模块 '支付交易系统' 生成以下子文档: 支付交易系统.md, 充值管理.md, ..."
  - 每个模块独立一次调用，LLM 上下文干净，专注度高

Phase 4 执行方式（方案 B — 兜底）:
  在 prompt 中强制要求: 每完成一个模块的文档生成，必须停止并报告进度，
  等待 continue 指令再处理下一个模块
```

**代码改动**：不需要，这是 Skill prompt 层面的执行策略。

### 4.2 校验层面：后置质量校验脚本

**问题**：LLM 生成完文档后，没有机制检测产出是否达标。

**方案**：通过 Skill 的 `AllowedTools` 添加 `bash`，在 Phase 5 阶段执行校验脚本：

```bash
# 校验脚本：content/ 目录结构 vs 期望结构
# 1. 统计文档数量
find content/ -name "*.md" | wc -l

# 2. 检查每个 .md 是否包含必需的章节
for f in $(find content/ -name "*.md"); do
  echo "=== $f ==="
  grep -c "## 简介" "$f"
  grep -c "## 详细组件分析" "$f"
  grep -c "## 附录" "$f"
  grep -c "章节来源" "$f"
  grep -c '```mermaid' "$f"
done

# 3. 检查 Mermaid 图后面是否有 "图表来源"
# 4. 检查所有引用是否使用 #L 格式
grep -oP '\[.*?\]\(.*?#L\d+.*?\)' "$f" | wc -l
```

**代码改动**：在 `builtins.go` 中注册 `analyze-project` 时，`AllowedTools` 需包含 `"bash"`：

```go
{
    Name:         "analyze-project",
    Description:  "Analyze a legacy project...",
    Body:         builtinAnalyzeProjectBody,
    Scope:        ScopeBuiltin,
    Path:         "(builtin)",
    RunAs:        RunSubagent,
    AllowedTools: append([]string{"read_file", "ls", "glob", "grep", "write_file", "bash"},
        codegraphTools...),
}
```

### 4.3 子Agent层面：校验与修正子Agent（推荐方案）

**问题**：bash 脚本只能做结构化检查（章节数、引用格式），无法判断内容质量（伪代码是否准确、Mermaid 图是否反映真实架构、分析深度是否足够）。Go 校验器开发成本高且不够灵活。

**方案**：利用 Reasonix 已有的 subagent 机制，在 Phase 5 阶段启动一个独立的 **校验修正子Agent**，对所有产出物逐篇审查并自动修正。

#### 架构设计

```
analyze-project (主 subagent)
  │
  ├── Phase 1-4: 生成所有文档到 content/
  │
  └── Phase 5: 启动 "doc-reviewer" 子Agent
                │
                ├── 逐篇读取 content/ 下的文档
                ├── 对照源码验证每个事实性声明
                ├── 检查 Mermaid 图是否反映真实架构
                ├── 补全缺失的 "章节来源" / "图表来源"
                ├── 修正格式不规范的引用
                ├── 补充交叉引用链接
                └── 输出修正后的文档（覆盖原文件）
```

#### 校验子Agent 的 Skill 定义

在 `builtins.go` 中新增一个内置 subagent skill：

```go
{
    Name:         "doc-reviewer",
    Description:  "Review and fix generated documentation — verifies source citations, Mermaid accuracy, chapter completeness, cross-references. Reads docs + source code, then overwrites corrected versions.",
    Body:         builtinDocReviewerBody,
    Scope:        ScopeBuiltin,
    Path:         "(builtin)",
    RunAs:        RunSubagent,
    AllowedTools: append([]string{"read_file", "write_file", "ls", "glob", "grep"},
        codegraphTools...),
}
```

#### 校验子Agent 的 Prompt（核心）

```markdown
You are running as a documentation-review subagent. Your job is to review 
and FIX a set of generated project documentation files in the `content/` 
directory. The parent agent generated these docs by analyzing source code — 
your job is to verify every claim against the actual source and fix anything 
that's wrong, missing, or incomplete.

## Input

The `content/` directory contains Markdown files organized by module. 
The source code is at the project root (the parent will tell you the path).

## Review Checklist (check EVERY document)

For each .md file in content/, verify and fix:

### 1. Structural Completeness
- [ ] Has exactly 10 chapters (简介 → 附录)
- [ ] Has `<cite>` block listing ALL referenced source files
- [ ] Has "## 相关文档" section with cross-references
- [ ] Each chapter (2-9) ends with "章节来源" listing source references

### 2. Citation Accuracy
- [ ] Every factual claim has a source citation in `[file](path#Lstart-Lend)` format
- [ ] Negative claims ("does NOT support X") also cite the search that proved it
- [ ] Open each cited file at the claimed line range — verify the code actually 
      says what the doc claims. If the line numbers are wrong, fix them.
- [ ] If a claim has NO source, either find the source or remove the claim

### 3. Code Accuracy (CRITICAL)
- [ ] Every pseudocode block must match the actual source code
- [ ] Read the source file at the cited lines, compare with the pseudocode
- [ ] If the pseudocode is fabricated or wrong, rewrite it based on actual code
- [ ] If a method/function is described but doesn't exist in source, remove it

### 4. Mermaid Diagram Accuracy
- [ ] Every diagram node/edge must correspond to actual code relationships
- [ ] Verify at least 3 nodes per diagram by reading the actual source files
- [ ] Each diagram MUST be followed by "图表来源" with file:line references
- [ ] If a diagram shows a relationship that doesn't exist in code, fix it
- [ ] Business modules MUST have at least 2 diagrams (graph TB + sequenceDiagram)

### 5. Cross-Reference Completeness
- [ ] Documents that mention another module must link to that module's doc
- [ ] "## 相关文档" section must list all related docs with valid links
- [ ] Verify each link points to an existing file

### 6. Content Depth
- [ ] "详细组件分析" must have pseudocode for each core method, not just descriptions
- [ ] "性能考量" must cite specific code (not generic advice)
- [ ] "故障排查指南" must list concrete error scenarios with resolution steps
- [ ] "附录" must have at least one subsection (API docs, config reference, etc.)

## How to Fix

When you find an issue, FIX IT by overwriting the file with write_file:
- Missing chapter → add it with appropriate content (read source to fill it)
- Wrong citation → correct the line numbers after verifying
- Missing citation → find the source and add it
- Fabricated pseudocode → replace with real code-derived pseudocode
- Missing diagram → create one based on actual code structure
- Missing cross-reference → add links to related docs

## Output

After reviewing ALL documents, output a summary:

```
📊 文档审查报告
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
总文档数: {n}
审查通过（无需修改）: {n}
已修正: {n}

修正明细:
- {file}: 补充了缺失的 "章节来源" (3处)
- {file}: 修正了错误的行号引用 (5处)
- {file}: 重写了不准确的伪代码 (2处)
- {file}: 补充了缺失的 sequenceDiagram
- {file}: 添加了交叉引用链接 (4处)
...
```

## Rules

- You MUST read the actual source code to verify claims — never trust the doc blindly
- You MUST fix every issue you find — don't just report them
- Cap yourself at ~30 tool calls. Prioritize: code accuracy > citations > structure > cross-refs
- If a document is fundamentally wrong (claims about code that doesn't exist), rewrite it
```

#### 调用方式

在 analyze-project 的 Phase 5 prompt 中，通过 `run_skill` 调用校验子Agent：

```markdown
## Phase 5: Quality Assurance via doc-reviewer subagent

After all documents are generated, invoke the doc-reviewer subagent to 
verify and fix every document:

```
run_skill({name: "doc-reviewer", arguments: "Review and fix all documents 
in content/ for the project at <project-root>. Verify every claim against 
source code, fix wrong citations, add missing chapters/sources/diagrams, 
and ensure cross-references are complete."})
```

The doc-reviewer will:
1. Read each .md file in content/
2. Cross-reference claims against actual source code
3. Fix any inaccuracies, missing sections, or broken citations
4. Output a review report with fix summary

After doc-reviewer completes, review its report. If it found major issues,
you may need to re-generate specific documents.
```

#### 为什么用子Agent 而不是 Go 代码

| 维度 | 子Agent 方案 | Go 校验器方案 |
|------|:---:|:---:|
| **内容准确性校验** | ✅ 能读源码对照验证 | ❌ 只能做格式检查 |
| **自动修正能力** | ✅ 发现错误直接修复 | ❌ 只能报告不能修 |
| **Mermaid 语义校验** | ✅ 能判断图是否反映真实架构 | ❌ 只能检查语法 |
| **伪代码准确性** | ✅ 能对比源码验证 | ❌ 无法判断 |
| **开发成本** | 低（写 prompt 即可） | 中（需开发新包） |
| **执行成本** | 中（消耗 LLM token） | 低（纯计算） |
| **覆盖的差距** | 全部 7 个差距 | 差距 2、5、6（格式层面） |

### 4.4 对比：四种约束机制

| 机制 | 约束强度 | 实施成本 | 覆盖的差距 |
|------|:---:|:---:|------|
| **Prompt 约束**（已有） | 中（LLM 可能遗忘） | 低（改 prompt 即可） | 全部 |
| **分阶段执行**（新增） | 高（任务分解降低偷懒空间） | 低（prompt 层面） | 差距1、2、4 |
| **bash 校验**（新增） | 高（自动检测不合格） | 低（加 bash tool） | 差距2、5、6 |
| **子Agent校验修正**（新增） | 最高（AI 审查 + 自动修复） | 低（新 Skill prompt） | 全部7个差距 |

### 4.5 推荐实施路径

```
短期（当前迭代）:
  1. Prompt 约束 ✅ 已完成
  2. 分阶段执行 ✅ 已写入 Phase 4 prompt
  3. bash 校验 ✅ 已写入 Phase 5 prompt
  4. 子Agent校验修正 → 新增 doc-reviewer Skill + 在 Phase 5 中调用

中期（下个迭代）:
  5. 在 builtins.go 注册 doc-reviewer Skill
  6. 端到端测试：analyze-project → doc-reviewer 全流程

长期:
  7. 与 review skill 协作 → analyze-project 产出的文档自动触发 review
  8. 增量更新 → 仅校验变更的文档章节
```

---

## 五、实现步骤（更新）

| 步骤 | 内容 | 说明 | 状态 |
|------|------|------|:--:|
| 1 | 编写 `builtinAnalyzeProjectBody` prompt | 最核心的工作，需要精心调优 | ✅ 已完成 |
| 2 | 编写 `builtinDocReviewerBody` prompt | 校验修正子Agent的 prompt | ✅ 本次迭代 |
| 3 | 在 `builtins.go` 注册两个 Skill | `analyze-project` + `doc-reviewer` | ⬜ 待实现 |
| 4 | 在 `tools.go` 注册两个顶层工具 | `analyze_project` + `doc_reviewer` | ⬜ 待实现 |
| 5 | 用 HxCapital 项目做端到端测试 | 对比生成文档与现有 `content/` 的质量 | ✅ 已完成 |
| 6 | 迭代调优 prompt | 根据测试结果调整分析策略和文档模板 | ✅ 本次迭代 |
| 7 | 二次测试验证（含 doc-reviewer） | 用优化后的 prompt + doc-reviewer 重新生成 | ⬜ 待执行 |
| 8 | 处理边界情况 | 超大项目、多语言项目、无 codegraph 的项目 | ⬜ 待处理 |

### 本次迭代调整要点汇总

基于 `contenttestresult/` vs `content/` 的差距分析，在三层做了关键调整：

| 层面 | 调整内容 | 影响 |
|------|------|------|
| **Prompt 优化** | 7项模板和规则改进（子文档拆分、10章结构、伪代码规则、Mermaid选择指南、引用规则、章节来源、交叉引用） | 差距1-7 |
| **执行保障** | 分阶段逐模块执行 + STOP 进度报告 + bash 结构化校验 | 防偷懒、防结构缺失 |
| **质量闭环** | 新增 `doc-reviewer` 子Agent：读源码逐条验证 → 发现错误自动修正 → 输出审查报告 | 覆盖全部7个差距的最深防线 |

---

## 六、风险与对策

| 风险 | 对策 |
|------|------|
| 大项目 token 超限 | 分阶段执行，每模块独立 subagent |
| 生成内容不准确 | 强制 cite 源码行号，negative claim rule |
| Mermaid 图语法错误 | 模板化生成，限制图表复杂度 |
| 无 codegraph 支持 | 降级为 grep + read_file 纯文本分析 |
| 文档间引用断裂 | Phase 5 专门做交叉引用校验 |
| **LLM 偷懒/遗忘约束** | 四层保障：分阶段执行 + bash 校验 + doc-reviewer 子Agent + prompt 约束 |
| **单次生成质量下降** | Phase 4 强制逐模块执行 + STOP 报告机制 |
| **产出结构不完整** | Phase 5 bash 脚本自动检测 + doc-reviewer 补全缺失章节 |
| **内容准确性不足** | doc-reviewer 子Agent 逐条对照源码验证并修正 |

---

## 七、现有文档体系参考

当前 `content/` 目录的完整结构，作为生成目标的参考范本：

```
content/
├── 快速开始.md
├── 项目概述/
│   ├── 项目概述.md
│   ├── 项目介绍.md
│   ├── 技术架构.md
│   ├── 核心功能特性.md
│   └── 部署架构.md
├── 架构设计/
│   ├── 架构设计.md
│   ├── 整体架构模式.md
│   ├── 技术栈选型.md
│   ├── 组件交互模式.md
│   └── 分层架构设计/
│       ├── 分层架构设计.md
│       ├── 入口层设计.md
│       ├── 服务层设计/
│       │   ├── 服务层设计.md
│       │   ├── 支付交易服务.md
│       │   ├── 系统管理服务.md
│       │   ├── 事务管理服务.md
│       │   └── 认证服务.md
│       ├── 数据访问层设计.md
│       └── 核心工具层设计.md
├── 数据模型设计/
│   ├── 数据模型设计.md
│   ├── 核心数据模型.md
│   ├── 权限数据模型.md
│   └── 系统配置数据模型.md
├── API接口参考/
│   ├── API接口参考.md
│   ├── 支付交易API.md
│   ├── 用户管理API.md
│   ├── 系统管理API.md
│   └── 认证授权API.md
├── 核心工具类/
│   ├── 核心工具类.md
│   ├── 加密解密工具.md
│   ├── 数据库辅助工具.md
│   ├── 通用工具函数.md
│   ├── 配置管理工具.md
│   └── 错误处理机制.md
├── 支付交易系统/
│   ├── 支付交易系统.md
│   ├── 充值管理.md
│   ├── 消费管理.md
│   ├── 冲销管理.md
│   ├── 余额管理.md
│   ├── 交易查询.md
│   └── 交易安全机制.md
├── 用户管理系统/
│   ├── 用户管理系统.md
│   ├── 用户信息管理.md
│   ├── 用户余额管理.md
│   ├── 用户认证机制.md
│   └── 登录会话管理.md
├── 权限控制系统/
│   ├── 权限控制系统.md
│   ├── 用户权限系统.md
│   ├── API权限管理.md
│   ├── 应用授权管理.md
│   └── 认证机制.md
├── 系统配置管理/
│   ├── 系统配置管理.md
│   ├── 组织机构管理.md
│   └── 菜单管理.md
├── 开发指南/
│   ├── 开发指南.md
│   ├── 开发环境配置.md
│   ├── 代码规范与最佳实践.md
│   ├── 构建与部署流程.md
│   ├── 测试策略与实践.md
│   └── 调试技巧与故障排除.md
├── 部署运维/
│   ├── 部署运维.md
│   ├── AWS Lambda部署.md
│   ├── 环境配置管理.md
│   ├── 监控与日志.md
│   ├── 数据库运维.md
│   ├── 安全运维.md
│   └── 性能调优.md
└── 故障排除/
    ├── 故障排除.md
    ├── 常见问题解决.md
    ├── 日志分析.md
    ├── 性能调优.md
    └── 安全问题处理.md
```

### 单篇文档标准结构

每篇文档遵循以下统一格式：

```markdown
# {模块名称}

<cite>
**本文引用的文件**
- [{filename}]({relative_path})
- ...
</cite>

## 目录
1. [简介](#简介)
2. [项目结构](#项目结构)
3. [核心组件](#核心组件)
4. [架构总览](#架构总览)
5. [详细组件分析](#详细组件分析)
6. [依赖关系分析](#依赖关系分析)
7. [性能考量](#性能考量)
8. [故障排查指南](#故障排查指南)
9. [结论](#结论)

## 简介
{模块职责、核心能力、设计原则概述}

## 项目结构
{文件列表 + Mermaid 组件关系图}

## 核心组件
{每个核心类/函数的职责描述}

## 架构总览
​```mermaid
graph TB
{组件关系图}
​```

图表来源
- [file:line](path#Lline)

章节来源
- [file:line](path#Lline)

## 详细组件分析
{逐个分析核心类/函数}

## 依赖关系分析
{上游/下游/外部依赖}

## 性能考量
{瓶颈、缓存、并发}

## 故障排查指南
{常见错误、排查路径}

## 结论
{总结与建议}
```

---

## 八、扩展方向

1. **增量更新**：检测项目变更，仅更新受影响的文档章节
2. **多语言输出**：支持中英文双语文档生成
3. **交互式分析**：用户可指定重点模块，跳过不关心的部分
4. **文档质量评分**：对生成的文档进行自检（引用完整性、Mermaid 语法正确性）
5. **与 `/init` 协作**：`/init` 生成的 AGENTS.md 可引用 `content/` 中的详细文档

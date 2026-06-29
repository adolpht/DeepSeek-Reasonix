# Reasonix 个人 Agent 改造计划

> 将 Reasonix 从「DeepSeek 原生编码 Agent」扩展为「既支持编码、又支持日常工作（文档生成、表格分析、桌面办公）」的个人 Agent。
>
> 本文档是改造契约：先改契约，再改代码。所有新增能力**默认走插件与配置**，主仓保持纯净。

---

## 0. 文档定位

- **读者**：Reasonix 维护者、贡献者、个人 Agent 使用者
- **状态**：规划草案（Draft）
- **关联文档**：
  - [SPEC.md](./SPEC.md) — 工程契约，本文档不违背其设计原则
  - [analyze-project-skill-design.md](./analyze-project-skill-design.md) — 已有的项目分析 Skill 实现参考
- **变更原则**：本计划演进时，先更新本文档，再改代码

---

## 1. 目标与边界

### 1.1 目标

将 Reasonix 扩展为个人 Agent，支持两类工作场景：

| 场景 | 现状 | 目标 |
|------|------|------|
| 编码（读/写/改代码、调试、重构） | ✅ 已支持 | 维持并增强（保持缓存友好） |
| 文档生成（周报、会议纪要、合同、文档体系） | ❌ 无 | 通过插件 + Skill 支持 |
| 表格整理与分析（Excel/CSV 读写、查询、图表） | ❌ 无 | 通过插件 + Skill 支持 |
| 桌面办公交互（预览、模板、落盘、定时任务） | ❌ 无 | 桌面端增强 |

### 1.2 非目标（Out of Scope）

- **不**把办公能力写进 `internal/tool/builtin`（破坏单二进制哲学）
- **不**重新实现 Provider/Tool 接口（已有抽象足够）
- **不**做图形化拖拽编排（保持 Skill 声明式工作流）
- **不**做云同步、多用户、SaaS 化（这是个人 Agent）

### 1.3 成功标准

1. **零侵入**：主仓 `internal/` 不为办公能力新增任何 `*.go` 文件
2. **可裁剪**：禁用所有办公插件后，Reasonix 行为与改造前完全一致
3. **闭环验证**：以下三个端到端任务可由 Skill 完成
   - 周报生成（git log + 待办 → docx）
   - 销售表格分析（xlsx 读取 → 筛选聚合 → 图表 → 结论）
   - 项目文档体系生成（已有 `analyze-project` Skill 的延伸）

---

## 2. 现状评估

### 2.1 已就位的扩展点

| 扩展点 | 位置 | 关键事实 |
|--------|------|----------|
| Tool 接口与注册表 | `internal/tool` | `type Tool interface` + `RegisterBuiltin(t)`；内置工具 `init()` 自注册 |
| MCP 插件客户端 | `internal/plugin` | stdio JSON-RPC，`tools/list` + `tools/call`，命名空间 `mcp__<server>__<tool>` |
| Skill 声明式工作流 | `.reasonix/commands/*.md` + `internal/skill` | 支持 `runAs=subagent`，单独会话执行 |
| Provider 注册表 | `internal/provider` | OpenAI 兼容；支持 `executor + planner` 双模型 |
| 桌面 Wails 应用 | [`desktop/app.go`](../desktop/app.go) | 多 Tab、`AssetServer` 中间件、`mediaTokenStore` |
| 桌面前端组件 | `desktop/frontend/src/components/` | 已有 `Markdown.tsx`、`HljsCode.tsx`、`PromptShelf.tsx`、`CommandPalette.tsx`、`ApprovalModal.tsx`、`MemoryPanel.tsx` 等 |
| 权限层 | `internal/permission` | `allow/ask/deny` 决策，副作用动作可强制审批 |
| 长期记忆 | `AGENTS.md` + `MemoryPanel.tsx` | 项目级记忆，跨会话引用 |

### 2.2 关键约束（不可违背）

1. **`CGO_ENABLED=0` 单二进制** — 所有新增依赖必须纯 Go
2. **配置驱动** — 不硬编码任何办公行为
3. **DeepSeek 前缀缓存友好** — 避免长会话中途切换 system prompt；大表格分片返回
4. **副作用需审批** — 写文件、发邮件等必须走 `ApprovalModal.tsx`

---

## 3. 设计原则（继承并扩展 SPEC §1）

1. **办公能力 = 插件 + Skill**。文档/表格工具以独立 MCP 进程接入，主仓不感知。
2. **数据往返闭环**。每个工具的返回必须是「模型可消费」的结构（Markdown 表格 / JSON / 图片 token），避免全量文本灌入上下文。
3. **缓存感知**。大文件分片、长上下文交给 subagent，主会话保持 system prompt 稳定。
4. **本地优先**。敏感文档默认本地模型处理（如配置 `planner_model = "ollama/..."`），不强制上云。
5. **渐进式**。每个 Phase 可独立交付、独立验收，不依赖后续 Phase。

---

## 4. 总体架构

```
┌─────────────────────────────────────────────────────────────────┐
│                        Reasonix Core                            │
│  (internal/ — 不改动)                                            │
│   provider · tool · plugin · agent · skill · permission        │
└───────────────┬─────────────────────────────────┬──────────────┘
                │ MCP (stdio JSON-RPC)            │ Bound Methods (Wails)
                │                                 │
   ┌────────────┴──────────────┐    ┌────────────┴───────────────┐
   │   办公能力插件层（新增）    │    │   桌面交互层（增强）         │
   │                             │    │                            │
   │  reasonix-plugin-sheet      │    │  app.go:                    │
   │   - read_sheet              │    │   - ExportToWorkspace()      │
   │   - write_sheet             │    │   - OpenInOSDefault()        │
   │   - query_sheet             │    │   - RenderDocPreview()       │
   │   - chart_sheet             │    │                              │
   │                             │    │  frontend/components/:       │
   │  reasonix-plugin-office     │    │   - DocPreviewer.tsx         │
   │   - read_docx               │    │   - SheetViewer.tsx          │
   │   - write_docx              │    │   - TemplateLibrary.tsx      │
   │   - md_to_pdf               │    │   - SchedulerPanel.tsx       │
   │   - render_template         │    │                              │
   └─────────────┬───────────────┘    └────────────┬───────────────┘
                 │                                  │
                 └──────────────┬───────────────────┘
                                │
                ┌───────────────┴────────────────┐
                │     Skill 库（.reasonix/commands/）│
                │  weekly-report.md                  │
                │  meeting-minutes.md                │
                │  sheet-analysis.md                 │
                │  contract-draft.md                 │
                │  inbox-triage.md                   │
                └────────────────────────────────────┘
```

---

## 5. 分阶段路线图

### 总览

| Phase | 主题 | 工作量 | 侵入度 | 交付物 |
|-------|------|--------|--------|--------|
| P1 | 表格插件 | 1-2 周 | 零（独立仓） | `reasonix-plugin-sheet` 二进制 |
| P2 | 表格 Skill 闭环 | 1 周 | 配置 | `sheet-analysis.md` 等 Skill |
| P3 | 文档插件 | 2 周 | 零（独立仓） | `reasonix-plugin-office` 二进制 |
| P4 | 文档 Skill 闭环 | 1 周 | 配置 | `weekly-report.md` 等 Skill |
| P5 | 桌面预览/模板 UI | 3-4 周 | 改 app.go + 前端 | DocPreviewer / SheetViewer / TemplateLibrary |
| P6 | 个人化与调度 | 4 周+ | 改 tray + cron | SchedulerPanel / 全局热键 / 长期记忆增强 |

> 先做表格再作文档的原因：表格数据往返更闭环（读→算→图→结论），能更快验证「Agent 在干活」而非「生成了一坨文字」。

---

### Phase 1 · 表格插件 `reasonix-plugin-sheet`（1-2 周）

#### 1.1 目标

提供 Excel/CSV 的读写、查询、图表能力，作为独立 MCP 服务器进程接入。

#### 1.2 仓库结构

新建独立仓 `reasonix-plugin-sheet`，参考 [`cmd/reasonix-plugin-example/main.go`](../cmd/reasonix-plugin-example/main.go)：

```
reasonix-plugin-sheet/
├── go.mod                      # module reasonix-plugin-sheet
├── main.go                     # MCP server 入口
├── internal/
│   ├── sheet/
│   │   ├── reader.go           # read_sheet 实现
│   │   ├── writer.go           # write_sheet 实现
│   │   ├── query.go            # query_sheet 实现（类 SQL 过滤）
│   │   └── chart.go            # chart_sheet 实现
│   └── mcp/
│       └── server.go           # JSON-RPC 调度
└── README.md
```

#### 1.3 工具契约

##### `mcp__sheet__read_sheet`

```jsonc
// 入参
{
  "path": "/abs/path/data.xlsx",   // 必填，绝对路径
  "sheet": "Sheet1",               // 可选，默认第一个 sheet
  "range": "A1:Z100",              // 可选，Excel 区域；缺省=自动探测有效区域
  "header_row": 1,                 // 可选，默认 1；0 表示无表头
  "max_rows": 1000,                // 可选，截断保护，默认 1000
  "format": "markdown"             // 可选：markdown | json | csv
}
// 返回（format=markdown）
{
  "content": [{ "type": "text", "text": "| A | B |\n|---|---|\n| 1 | 2 |" }],
  "meta": { "rows": 100, "cols": 26, "truncated": false }
}
```

##### `mcp__sheet__write_sheet`

```jsonc
// 入参
{
  "path": "/abs/path/out.xlsx",
  "sheet": "Sheet1",
  "data": [[...], [...]] | "<markdown table>",
  "mode": "overwrite" | "append",   // 默认 overwrite
  "start_cell": "A1"
}
// 返回
{ "content": [{ "type": "text", "text": "wrote 100 rows to Sheet1" }] }
```

##### `mcp__sheet__query_sheet`

类 SQL 过滤聚合，避免全量灌入模型上下文：

```jsonc
// 入参
{
  "path": "/abs/path/sales.xlsx",
  "sheet": "Q1",
  "select": ["region", "SUM(amount) as total"],
  "where": "region IN ('华东','华北') AND amount > 1000",
  "group_by": ["region"],
  "order_by": "total DESC",
  "limit": 50
}
// 返回：Markdown 表格 + 行数
```

> 实现可先用 `xuri/excelize` 读全量到内存，再用 `go-sqlparser` 解析 WHERE/SELECT 在内存执行；后续再优化为流式。

##### `mcp__sheet__chart_sheet`

```jsonc
// 入参
{
  "path": "/abs/path/out.xlsx",     // 也可以是 read_sheet 的内存数据
  "data_ref": { "range": "A1:B10" },
  "type": "bar" | "line" | "pie" | "scatter",
  "title": "Q1 销售对比",
  "x": "region",
  "y": ["total"],
  "output": "png"                  // 仅支持 png（走桌面 AssetServer）
}
// 返回：图片附件 token
{
  "content": [
    { "type": "image", "data": "<base64>", "mime": "image/png" }
  ],
  "meta": { "image_token": "chart_abc123" }
}
```

> 桌面端 `mediaTokenStore` 已支持图片附件路由，复用即可。

#### 1.4 配置接入

在 `reasonix.toml`：

```toml
[[plugins]]
name = "sheet"
command = ["reasonix-plugin-sheet"]
# 可选：限制可见工具
# tools = ["read_sheet", "query_sheet"]   # 只读模式
```

#### 1.5 验收

- [ ] 插件可作为独立进程启动，`tools/list` 返回 4 个工具
- [ ] 在 `reasonix chat` 中可调用 `read_sheet` 读取本地 xlsx
- [ ] `query_sheet` 对 1 万行 xlsx 的查询响应 < 2s
- [ ] `chart_sheet` 返回的图片在桌面端能正常显示
- [ ] 禁用插件后 Reasonix 行为与改造前一致

#### 1.6 技术选型

| 依赖 | 用途 | 纯 Go | 备注 |
|------|------|:---:|------|
| `xuri/excelize/v2` | xlsx 读写 | ✅ | 事实标准 |
| `jszwec/csvutil` | CSV 结构化 | ✅ | |
| `wcharczuk/go-chart` | 图表 | ✅ | 输出 PNG |
| `xwb1989/sqlparser` | query_sheet 解析 | ✅ | 可选；初版可简化 |

---

### Phase 2 · 表格 Skill 闭环（1 周）

#### 2.1 目标

用 Skill 把表格工具串成端到端工作流，验证「声明式工作流 + subagent」在办公场景的可行性。

#### 2.2 Skill 清单

##### `.reasonix/commands/sheet-analysis.md`

```markdown
---
description: 分析 Excel/CSV 表格：读取→清洗→聚合→图表→结论
runAs: subagent
tools:
  - mcp__sheet__read_sheet
  - mcp__sheet__query_sheet
  - mcp__sheet__chart_sheet
  - write_file
---

# 表格分析 Skill

## 输入
- path: 表格文件路径
- question: 自然语言分析问题（如「华东区 Q1 销售额排名前三的城市」）

## 执行步骤
1. 调用 read_sheet 探测表格结构与有效区域，**只返回前 5 行**作为 schema 样本
2. 根据 question 规划 query_sheet 的 SQL（select/where/group_by）
3. 执行 query_sheet，获取聚合结果
4. 根据结果选择图表类型，调用 chart_sheet 生成 PNG
5. 用自然语言总结结论，引用具体数值
6. 调用 write_file 把结论 + 图表路径写入 `分析报告.md`

## 约束
- NEVER 对超过 5 万行的表调用 read_sheet 全量读取
- 图表最多生成 3 张，避免 token 爆炸
- 结论必须引用具体数值，禁止「整体良好」这类空话
```

##### `.reasonix/commands/sheet-clean.md`（可选）

数据清洗：去重、空值处理、类型转换、写入新 sheet。

#### 2.3 验收

- [ ] 输入一个 5000 行销售 xlsx，能完成「按区域聚合 + 生成柱状图 + Markdown 结论」
- [ ] 全程不把原始 5000 行数据灌入主会话（走 subagent）
- [ ] 产物 `分析报告.md` 落盘到工作区

---

### Phase 3 · 文档插件 `reasonix-plugin-office`（2 周）

#### 3.1 目标

提供 docx/markdown/pdf 的读写与互转能力。

#### 3.2 仓库结构

```
reasonix-plugin-office/
├── go.mod
├── main.go
└── internal/
    ├── docx/
    │   ├── reader.go            # read_docx
    │   └── writer.go            # write_docx
    ├── convert/
    │   ├── md_pdf.go            # md_to_pdf（chromedp 渲染）
    │   └── docx_pdf.go          # docx_to_pdf
    └── template/
        └── render.go            # render_template（Go template）
```

#### 3.3 工具契约

##### `mcp__office__read_docx`

```jsonc
{
  "path": "/abs/path/doc.docx",
  "mode": "full" | "outline",     // outline 只返回标题层级，省 token
  "max_chars": 20000              // 截断保护
}
// 返回 Markdown 文本
```

##### `mcp__office__write_docx`

```jsonc
{
  "path": "/abs/path/out.docx",
  "content": "# 标题\n\n正文...",   // Markdown 输入
  "template": "report.docx",       // 可选，套用样式模板
  "metadata": { "title": "...", "author": "..." }
}
```

##### `mcp__office__md_to_pdf`

```jsonc
{
  "input": "/abs/path/in.md",
  "output": "/abs/path/out.pdf",
  "style": "default" | "report" | "contract",
  "engine": "chromedp" | "pandoc"   // 探测可用引擎
}
```

##### `mcp__office__render_template`

```jsonc
{
  "template": "/abs/path/tmpl.docx",   // 含 {{.Field}} 占位
  "data": { "name": "张三", "date": "2026-06-28" },
  "output": "/abs/path/out.docx"
}
```

#### 3.4 依赖探测策略

插件启动时探测：
- `chromedp`（纯 Go，内置）→ 优先用于 PDF
- `pandoc`（外部二进制）→ 可选，支持更复杂转换
- `wkhtmltopdf`（外部）→ 备选

任一可用即注册对应工具；全缺失则只暴露 `read_docx / write_docx`，并在 `tools/list` 的 description 中标注「PDF 不可用，需安装 pandoc」。

#### 3.5 配置接入

```toml
[[plugins]]
name = "office"
command = ["reasonix-plugin-office"]
env = { PANDOC_PATH = "/usr/local/bin/pandoc" }   # 可选
```

#### 3.6 验收

- [ ] `read_docx` 能正确读取含表格、图片的 docx（图片以 token 形式返回）
- [ ] `write_docx` 输出的 docx 在 Word/WPS 中样式正常
- [ ] `md_to_pdf` 在安装 pandoc 的环境下可生成 PDF
- [ ] `render_template` 能正确替换 `{{.Field}}` 占位

#### 3.7 技术选型

| 依赖 | 用途 | 纯 Go | 备注 |
|------|------|:---:|------|
| `unidoc/unioffice` | docx 读写 | ✅ | 注意 license，备选 `carmel/docx2txt`（读）|
| `chromedp/chromedp` | HTML→PDF | ✅ | 需本机有 Chrome |
| `gomarkdown/markdown` | md→HTML | ✅ | |
| `Masterminds/sprig` | 模板函数 | ✅ | 增强render_template |

---

### Phase 4 · 文档 Skill 闭环（1 周）

#### 4.1 Skill 清单

##### `.reasonix/commands/weekly-report.md`

```markdown
---
description: 生成本周工作周报（docx）
runAs: subagent
tools:
  - bash                          # git log
  - read_file
  - mcp__office__write_docx
  - mcp__office__render_template
---

# 周报生成 Skill

## 输入
- workspace: 工作区路径（取 git log）
- week: 第几周，默认本周
- template: 周报模板路径（可选）

## 执行步骤
1. bash: `git log --since='1 week ago' --pretty=format:'%h %s'`
2. 从 AGENTS.md / TodoPanel 读取本周完成的 TODO
3. 归类为「需求 / 修复 / 重构 / 其他」
4. 如果有 template，render_template 套用；否则 write_docx 生成
5. 输出 `周报_YYYYWW.docx`

## 约束
- 每条 git commit 必须保留 hash，便于回溯
- 禁止臆造未发生的工作
```

##### `.reasonix/commands/meeting-minutes.md`

会议纪要：录音转写（外部）→ 结构化（要点/决议/待办）→ docx。

##### `.reasonix/commands/contract-draft.md`

合同起草：条款库 + 模板渲染。

#### 4.2 验收

- [ ] 周报 Skill 能从 git log 生成结构化 docx
- [ ] 会议纪要能处理一份转写文本输出标准格式 docx

---

### Phase 5 · 桌面预览与模板 UI（3-4 周）

> 本阶段开始改动 [`desktop/app.go`](../desktop/app.go) 与前端，是改造中**唯一侵入主仓**的部分。

#### 5.1 新增 App 方法（Wails bound）

```go
// 在 desktop/app.go 中新增

// ExportToWorkspace 将 Agent 产物写入工作区指定路径，返回相对路径。
// 触发 ApprovalModal 审批。
func (a *App) ExportToWorkspace(tabID, relPath, content string) (string, error)

// OpenInOSDefault 用系统默认程序打开文件（docx/pdf/xlsx）。
func (a *App) OpenInOSDefault(absPath string) error

// RenderDocPreview 将 docx/pdf 渲染为图片列表，返回 mediaToken。
// 用于前端 DocPreviewer 组件。
func (a *App) RenderDocPreview(absPath string, page int) ([]string, error)

// ListTemplates 列出用户模板库（.reasonix/templates/）。
func (a *App) ListTemplates(kind string) ([]TemplateMeta, error)
```

#### 5.2 新增前端组件

| 组件 | 路径 | 职责 |
|------|------|------|
| `DocPreviewer.tsx` | `desktop/frontend/src/components/` | docx/pdf 分页预览，复用 `mediaTokenStore` |
| `SheetViewer.tsx` | 同上 | xlsx 分页表格视图，支持筛选条 |
| `TemplateLibrary.tsx` | 同上 | 模板库浏览 + 一键应用，扩展自 `PromptShelf.tsx` |
| `SchedulerPanel.tsx` | 同上 | 定时任务配置（见 P6） |

#### 5.3 改动点清单

- [`desktop/app.go`](../desktop/app.go)：新增 5 个 bound method
- [`desktop/frontend/src/components/`](../desktop/frontend/src/components/)：新增 4 个组件
- [`desktop/frontend/src/lib/types.ts`](../desktop/frontend/src/lib/types.ts)：新增 `DocMeta`、`TemplateMeta` 类型
- [`desktop/frontend/src/components/Composer.tsx`](../desktop/frontend/src/components/Composer.tsx)：在消息中支持 `doc_export` / `sheet_chart` 类型的内联渲染
- `reasonix.example.toml`：补充 `[[plugins]]` 示例

#### 5.4 验收

- [ ] Agent 生成 docx 后，前端可内嵌预览前 5 页
- [ ] 「导出到工作区」按钮触发审批弹窗（`ApprovalModal.tsx`）
- [ ] 模板库可浏览 `.reasonix/templates/*.docx` 并一键套用

---

### Phase 6 · 个人化与调度（4 周+，长期）

#### 6.1 长期记忆增强

扩展 `AGENTS.md` 机制为「个人知识库」：
- `~/.reasonix/memory/people.md` — 常联系人/同事
- `~/.reasonix/memory/projects.md` — 在跟进的项目
- `~/.reasonix/memory/preferences.md` — 个人偏好（写作风格、术语）

`MemoryPanel.tsx` 增加多文件切换。

#### 6.2 全局热键与后台唤起

基于 tray（已有 [`desktop/tray.go`](../desktop/tray.go)）：
- 全局热键（如 `Ctrl+Shift+R`）唤起主窗口
- 后台 Skill 执行时，tray 图标显示进度

#### 6.3 定时任务

新增 `SchedulerPanel.tsx`，配置 cron 表达式触发 Skill：
- 每周五 17:00 跑 `weekly-report`
- 每天 09:00 跑 `inbox-triage`

实现：Go 侧用 `robfig/cron`（纯 Go），tray 启动时加载，触发时新建一个 WorkspaceTab 执行 Skill。

#### 6.4 本地模型优先

在 `reasonix.toml` 支持标记敏感任务：

```toml
[agent]
planner_model = "ollama/qwen2.5:14b"   # 本地模型做规划
sensitive_patterns = ["合同", "薪资", "身份证"]   # 命中则强制走本地
```

---

## 6. 落地顺序（最小阻力路径）

```
Week 1-2:  P1  reasonix-plugin-sheet          ← 立刻能用的表格能力
Week 3:    P2  sheet-analysis skill           ← 验证 Skill 闭环
Week 4-5:  P3  reasonix-plugin-office         ← 文档能力
Week 6:    P4  weekly-report skill            ← 端到端文档闭环
Week 7-10: P5  桌面预览/模板 UI                 ← 主仓改动
Week 11+:  P6  调度/记忆/热键                   ← 长期演进
```

**里程碑**：
- M1（Week 3 末）：能用一句话分析一个 Excel
- M2（Week 6 末）：能用一句话生成一份周报 docx
- M3（Week 10 末）：桌面端可预览/导出/模板化

---

## 7. 风险与约束

| 风险 | 影响 | 缓解 |
|------|------|------|
| 大表格 token 爆炸 | 成本 / 上下文丢失 | `query_sheet` 强制分片，`read_sheet` 默认 max_rows=1000 |
| docx 样式丢失 | 产物不可用 | 提供 `template` 参数套用样式；不追求 100% 还原 |
| 外部依赖（pandoc/Chrome）缺失 | 功能降级 | 启动时探测，缺失则工具不注册 + UI 提示 |
| 敏感文档外泄 | 隐私 | 支持本地模型 + sensitive_patterns 强制路由 |
| 缓存失效 | 成本上升 | 长任务走 subagent，主会话保持 system prompt 稳定 |
| 插件进程崩溃 | 工具不可用 | MCP 客户端已有重连；Skill 层做降级提示 |

---

## 8. 不做的事情（明确排除）

1. **不**做图形化 workflow 编排器 — 保持 Skill 声明式
2. **不**实现完整的 Excel 公式引擎 — `query_sheet` 只做 SQL 级聚合
3. **不**支持在线协作 / 多人编辑 — 个人 Agent
4. **不**把 pandoc/chromedp 打进主二进制 — 外部依赖探测
5. **不**重写 Provider/Tool/Plugin 接口 — 现有抽象足够

---

## 9. 验收总表

| 编号 | 验收项 | Phase |
|------|--------|-------|
| A1 | 独立仓 `reasonix-plugin-sheet` 可编译为单二进制 | P1 |
| A2 | `read_sheet` 对 1 万行 xlsx 响应 < 2s | P1 |
| A3 | `chart_sheet` 图片在桌面端正常显示 | P1 |
| A4 | `sheet-analysis` Skill 完成 5000 行表格端到端分析 | P2 |
| A5 | 全程不把原始数据灌入主会话 | P2 |
| A6 | `read_docx` 正确读取含表格/图片的 docx | P3 |
| A7 | `md_to_pdf` 在 pandoc 环境下生成 PDF | P3 |
| A8 | `weekly-report` Skill 从 git log 生成 docx | P4 |
| A9 | 桌面端可内嵌预览 docx 前 5 页 | P5 |
| A10 | 「导出到工作区」触发 `ApprovalModal` | P5 |
| A11 | 禁用所有办公插件后，Reasonix 行为与改造前一致 | 全局 |
| A12 | 主仓 `internal/` 不新增任何办公相关 `*.go` | 全局 |

---

## 10. 后续演进（不在本计划内）

- **多 Agent 协作**：subagent 机制已有，可演化出「研究 Agent + 写作 Agent + 审核 Agent」分工
- **跨设备同步**：个人知识库的云同步（端到端加密）
- **语音输入**：tray 全局热键 + 本地 whisper
- **插件市场**：办公插件模板化，社区共享

---

## 附录 A：参考文件索引

| 文件 | 用途 |
|------|------|
| [`cmd/reasonix-plugin-example/main.go`](../cmd/reasonix-plugin-example/main.go) | MCP 插件参考实现 |
| [`docs/SPEC.md`](./SPEC.md) | 工程契约 |
| [`desktop/app.go`](../desktop/app.go) | 桌面 App bound method |
| [`desktop/frontend/src/components/Markdown.tsx`](../desktop/frontend/src/components/Markdown.tsx) | 已有 Markdown 渲染器 |
| [`desktop/frontend/src/components/PromptShelf.tsx`](../desktop/frontend/src/components/PromptShelf.tsx) | 提示词货架（模板库基础）|
| [`desktop/frontend/src/components/ApprovalModal.tsx`](../desktop/frontend/src/components/ApprovalModal.tsx) | 审批弹窗 |
| [`desktop/frontend/src/components/CommandPalette.tsx`](../desktop/frontend/src/components/CommandPalette.tsx) | 命令面板 |
| [`desktop/frontend/src/components/MemoryPanel.tsx`](../desktop/frontend/src/components/MemoryPanel.tsx) | 记忆面板 |
| [`internal/plugin/`](../internal/plugin/) | MCP 客户端 |
| [`internal/skill/`](../internal/skill/) | Skill 加载器 |
| [`.reasonix/commands/review.md`](../.reasonix/commands/review.md) | 现有 Skill 示例 |

---

**文档版本**：v0.1（Draft）
**最后更新**：2026-06-28
**维护者**：Reasonix 个人 Agent 改造负责人

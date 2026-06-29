# Reasonix 办公能力使用手册

## 概述

Reasonix 通过两个 MCP 插件（`reasonix-plugin-office` 和 `reasonix-plugin-sheet`）扩展了办公能力，覆盖文档处理、表格分析、模板渲染、图表生成等场景。配合 5 个内置 Skill 工作流，可实现周报生成、会议纪要、合同起草、表格清洗、数据分析等自动化任务。

---

## 一、MCP 工具清单

### 1.1 Office 插件（文档处理）

| 工具名 | 功能 | 只读 | 依赖 |
|--------|------|------|------|
| `mcp__office__read_docx` | 读取 .docx 文件，返回 Markdown | ✅ | 无 |
| `mcp__office__write_docx` | 将 Markdown 写入 .docx 文件 | ❌ | 无 |
| `mcp__office__render_template` | 渲染 Go text/template 模板 | ✅ | 无 |
| `mcp__office__md_to_pdf` | Markdown 转 PDF | ❌ | pandoc（可选） |

#### read_docx 参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `path` | string | ✅ | .docx 文件绝对路径 |
| `mode` | string | ❌ | `full`（默认）或 `outline`（仅标题） |
| `max_chars` | integer | ❌ | 输出字符上限（默认 20000） |

**示例**：
```
mcp__office__read_docx(path="C:/docs/报告.docx", mode="outline")
```

#### write_docx 参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `path` | string | ✅ | 输出 .docx 路径 |
| `content` | string | ✅ | Markdown 内容 |
| `title` | string | ❌ | 文档标题元数据 |

**示例**：
```
mcp__office__write_docx(
  path="C:/docs/周报.docx",
  content="# 本周工作总结\n\n## 完成事项\n- 完成需求评审...",
  title="2024年第42周周报"
)
```

#### render_template 参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `template` | string | 二选一 | 内联模板内容 |
| `template_path` | string | 二选一 | 模板文件绝对路径 |
| `variables` | object | ❌ | 变量映射 `{"key": "value"}` |

**模板语法**：Go text/template，支持 `{{.变量名}}`、`{{if}}`、`{{range}}` 等。

**示例**：
```
mcp__office__render_template(
  template="尊敬的 {{.name}}：\n\n感谢您参与 {{.project}} 项目。",
  variables={"name": "张三", "project": "Reasonix"}
)
```

#### md_to_pdf 参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `output` | string | ✅ | 输出 .pdf 路径 |
| `input` | string | 二选一 | 输入文件路径（.md/.html/.docx） |
| `markdown` | string | 二选一 | 内联 Markdown 内容 |
| `pdf_engine` | string | ❌ | PDF 引擎（pdflatex/wkhtmltopdf/weasyprint） |
| `extra_args` | array | ❌ | 额外 pandoc 参数 |

**注意**：需安装 pandoc 并加入 PATH。

---

### 1.2 Sheet 插件（表格处理）

| 工具名 | 功能 | 只读 | 依赖 |
|--------|------|------|------|
| `mcp__sheet__read_sheet` | 读取 xlsx/csv 行数据 | ✅ | excelize |
| `mcp__sheet__write_sheet` | 写入 xlsx/csv 文件 | ❌ | excelize |
| `mcp__sheet__query_sheet` | SQL 风格聚合查询 | ✅ | 无 |
| `mcp__sheet__chart_sheet` | 生成图表 PNG | ✅ | go-chart |

#### read_sheet 参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `path` | string | ✅ | xlsx/csv 文件路径 |
| `sheet` | string | ❌ | 工作表名（xlsx） |
| `range` | string | ❌ | Excel 范围（如 A1:Z100） |
| `header_row` | integer | ❌ | 标题行号（默认 1） |
| `max_rows` | integer | ❌ | 行数上限（默认 1000） |
| `format` | string | ❌ | `markdown`/`json`/`csv`（默认 markdown） |

**示例**：
```
mcp__sheet__read_sheet(
  path="C:/data/销售数据.xlsx",
  sheet="2024Q3",
  range="A1:F100",
  format="markdown"
)
```

#### write_sheet 参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `path` | string | ✅ | 输出文件路径 |
| `data` | array/string | ✅ | 二维数组或 Markdown 表格 |
| `sheet` | string | ❌ | 工作表名（默认 Sheet1） |
| `mode` | string | ❌ | `overwrite`（默认）或 `append` |
| `start_cell` | string | ❌ | 起始单元格（默认 A1） |

**示例**：
```
mcp__sheet__write_sheet(
  path="C:/data/汇总.xlsx",
  data=[["姓名","部门","业绩"],["张三","销售",150000]],
  mode="overwrite"
)
```

#### query_sheet 参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `path` | string | ✅ | xlsx/csv 文件路径 |
| `select` | array | ✅ | 字段或聚合函数 |
| `where` | string | ❌ | 过滤条件 |
| `group_by` | array | ❌ | 分组字段 |
| `order_by` | string | ❌ | 排序（如 `total DESC`） |
| `limit` | integer | ❌ | 结果行数上限（默认 100） |

**支持的聚合函数**：`SUM`、`COUNT`、`AVG`、`MIN`、`MAX`

**示例**：
```
mcp__sheet__query_sheet(
  path="C:/data/销售数据.xlsx",
  select=["region", "SUM(amount) as total"],
  where="amount > 10000",
  group_by=["region"],
  order_by="total DESC",
  limit=10
)
```

#### chart_sheet 参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `path` | string | ✅ | xlsx/csv 文件路径 |
| `x` | string | ✅ | X 轴列名 |
| `y` | array | ✅ | Y 轴列名数组 |
| `type` | string | ❌ | `bar`/`line`/`pie`/`scatter`（默认 bar） |
| `title` | string | ❌ | 图表标题 |
| `sheet` | string | ❌ | 工作表名 |
| `range` | string | ❌ | 数据范围 |

**示例**：
```
mcp__sheet__chart_sheet(
  path="C:/data/销售数据.xlsx",
  x="月份",
  y=["销售额", "利润"],
  type="line",
  title="2024年销售趋势"
)
```

---

## 二、Skill 工作流

### 2.1 weekly-report（周报生成）

**触发方式**：
- 对话输入 `/skill weekly-report`
- 自然语言：「帮我写周报」「生成本周工作总结」

**工作流程**：
1. 执行 `git log --since="7 days ago"` 获取提交记录
2. 读取 `AGENTS.md` 了解团队成员
3. 按类别归类：需求开发 / Bug 修复 / 重构优化 / 其他
4. 调用 `mcp__office__write_docx` 生成 `周报_YYYYWW.docx`

**输出**：`周报_202442.docx`（ISO 周数命名）

**约束**：严格基于 git log，不编造内容。

---

### 2.2 meeting-minutes（会议纪要）

**触发方式**：
- 对话输入 `/skill meeting-minutes`
- 自然语言：「整理会议纪要」「总结会议内容」

**输入**：会议转写文本（粘贴或文件路径）

**输出结构**：
```
# 会议纪要

## 议题
- 议题 1
- 议题 2

## 讨论要点
### 议题 1
- 要点 1
- 要点 2

## 决议
- [决议 1]

## 待办事项
- [ ] @张三 — 完成需求文档 — 截止 2024-10-25
- [ ] @李四 — 修复 Bug #123 — 截止 2024-10-26

## 遗留问题
- 待讨论事项
```

**约束**：不编造人名、决议或数据。

---

### 2.3 contract-draft（合同起草）

**触发方式**：
- 对话输入 `/skill contract-draft`
- 自然语言：「起草一份服务合同」「生成 NDA 模板」

**支持类型**：
- `service` — 服务合同
- `purchase` — 采购合同
- `nda` — 保密协议
- `employment` — 劳动合同
- `custom` — 自定义

**输出**：
- `合同_<类型>_<日期>.docx` — 合同正文
- `合同_<类型>_<日期>_TODO.md` — 待填项清单

**约束**：不替用户填写金额、期限、付款条件，留 `【待填：xxx】` 占位符。

---

### 2.4 sheet-clean（表格清洗）

**触发方式**：
- 对话输入 `/skill sheet-clean`
- 自然语言：「清洗这个表格」「去除重复行」

**功能**：
- 去除空行/重复行
- 标准化日期/数字格式
- 填充缺失值（均值/中位数/指定值）
- 列类型推断与转换

**输出**：`<原文件名>_cleaned.xlsx`

---

### 2.5 sheet-analysis（表格分析）

**触发方式**：
- 对话输入 `/skill sheet-analysis`
- 自然语言：「分析这个表格」「生成数据图表」

**功能**：
- 基础统计（均值、中位数、标准差、分位数）
- 分组聚合
- 相关性分析
- 生成可视化图表（柱状图/折线图/饼图/散点图）

**输出**：
- 统计摘要 Markdown
- 图表 PNG（base64 内联或文件）

---

## 三、桌面端 UI 能力

### 3.1 模板库

**位置**：侧边栏「模板库」图标

**功能**：
- 浏览 `.reasonix/templates/` 下的模板文件
- 按类型筛选（Word / 表格 / Markdown / 模板 / 文本 / CSV）
- 点击「套用」将模板路径插入 composer
- 点击「打开」用默认应用打开模板

**支持格式**：`.docx`、`.xlsx`、`.md`、`.tmpl`、`.txt`、`.csv`

---

### 3.2 文档预览

**触发**：Agent 生成 docx/图片后，ToolCard 自动显示预览

**功能**：
- 图片：内联渲染（`<img>`）
- docx/pdf：下载 chip + 「用默认应用打开」
- 「导出到工作区」：将产物保存到指定路径

**注意**：完整页级渲染需 LibreOffice/pandoc（未来增强）。

---

## 四、配置与部署

### 4.1 启用插件

在 `reasonix.toml` 中添加：

```toml
[[plugins]]
name    = "office"
command = "reasonix-plugin-office"

[[plugins]]
name    = "sheet"
command = "reasonix-plugin-sheet"
```

### 4.2 可选依赖

| 依赖 | 用途 | 安装方式 |
|------|------|----------|
| pandoc | `md_to_pdf` 工具 | `choco install pandoc` 或官网下载 |
| LaTeX 引擎 | PDF 中文字体支持 | `choco install miktex` |

### 4.3 构建产物

```
desktop/build/bin/
├── reasonix-desktop.exe              # 主程序
├── reasonix-desktop-amd64-installer.exe  # 安装程序
├── reasonix-plugin-office.exe        # Office 插件
└── reasonix-plugin-sheet.exe         # Sheet 插件
```

---

## 五、使用示例

### 示例 1：生成周报

```
用户：/skill weekly-report

Agent：
1. 执行 git log --since="7 days ago"
2. 读取 AGENTS.md 获取团队成员
3. 归类提交：
   - 需求开发：feat: 新增用户管理模块
   - Bug 修复：fix: 修复登录超时问题
   - 重构优化：refactor: 优化数据库查询
4. 调用 mcp__office__write_docx 生成 周报_202442.docx
```

### 示例 2：分析销售数据

```
用户：分析 sales.xlsx 中各区域的销售情况

Agent：
1. 调用 mcp__sheet__read_sheet 读取数据
2. 调用 mcp__sheet__query_sheet 聚合：
   select=["region", "SUM(amount) as total"]
   group_by=["region"]
   order_by="total DESC"
3. 调用 mcp__sheet__chart_sheet 生成柱状图
4. 输出统计摘要 + 图表
```

### 示例 3：起草服务合同

```
用户：/skill contract-draft

Agent：
1. 询问合同类型（默认 service）
2. 询问关键信息（甲方、乙方、服务范围）
3. 调用 mcp__office__render_template 渲染条款库
4. 调用 mcp__office__write_docx 生成合同
5. 输出 合同_service_20241025.docx + TODO.md
```

---

## 六、故障排查

| 问题 | 原因 | 解决 |
|------|------|------|
| `md_to_pdf` 不可用 | pandoc 未安装或未加入 PATH | 安装 pandoc 并重启 |
| 模板库为空 | `.reasonix/templates/` 不存在或无文件 | 创建目录并放入模板 |
| 文档预览显示「无法预览」 | docx 页级渲染未实现 | 点击「用默认应用打开」 |
| `read_sheet` 返回空 | 文件路径错误或格式不支持 | 检查路径，确认是 xlsx/csv |
| `query_sheet` 报错 | SQL 语法错误或字段不存在 | 检查字段名和语法 |

---

## 七、技术架构

```
┌─────────────────────────────────────────────────────────┐
│                    桌面端 (Wails)                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  模板库 UI   │  │  文档预览 UI  │  │  ToolCard    │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│                   Agent 核心                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  Skill 工作流 │  │  工具调度     │  │  权限控制     │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│                   MCP 插件层                             │
│  ┌──────────────────────────────────────────────────┐  │
│  │  reasonix-plugin-office                          │  │
│  │  - read_docx / write_docx / render_template      │  │
│  │  - md_to_pdf (需 pandoc)                         │  │
│  └──────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────┐  │
│  │  reasonix-plugin-sheet                           │  │
│  │  - read_sheet / write_sheet                      │  │
│  │  - query_sheet / chart_sheet                     │  │
│  └──────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

---

## 八、附录

### 8.1 文件约定

| 路径 | 用途 |
|------|------|
| `.reasonix/skills/` | Skill 工作流定义 |
| `.reasonix/templates/` | 用户模板库 |
| `cmd/reasonix-plugin-office/` | Office 插件源码 |
| `cmd/reasonix-plugin-sheet/` | Sheet 插件源码 |
| `desktop/preview_app.go` | 桌面端预览/模板绑定方法 |

### 8.2 相关文档

- [个人 Agent 路线图](./personal-agent-roadmap.md)
- [MCP 协议规范](https://modelcontextprotocol.io/)
- [Go text/template 语法](https://pkg.go.dev/text/template)

---

**版本**：Phase 5 完成  
**更新日期**：2026-06-28

# Rexion ToC 个人 Agent 改造 Spec

## Why

Rexion 当前定位为「开发者专用编码 Agent」，仅服务技术用户。为扩大用户群体至产品/运营、行政/HR、自由职业者等普通办公人群，需将产品从开发者工具升级为面向普通用户的桌面 AI 工作台——既能完成编码任务，又能处理文档、表格、PPT、信息调研、日程管理等日常办公与个人需求。

## What Changes

### P5：ToC 界面改造 + PPT + 调研

- 新增三模式切换器（编码/办公/助手），替代当前 coding/office 双模式
- 侧边栏重构为动态分区布局，内容随模式变化
- 新增首页面板（HomePanel），提供问候、快捷操作、最近任务、常用 Skill
- 新增右侧结果面板的预览 Tab，支持 docx/xlsx/pptx/pdf 预览
- 新增任务进度条组件（ProgressStepper），展示 Agent 多步执行进度
- 新增富媒体工具卡片（RichToolCard），替代纯文本工具输出
- 新增亮色主题 + 办公风格预设，办公/助手模式默认亮色
- 新增新手引导流程（OnboardingFlow），3 分钟完成首次任务
- 新增意图分类器（IntentClassifier），自动识别用户意图路由到对应模式
- 新增 PPT 生成 MCP 插件 `Rexion-plugin-slides`（5 个工具）
- 新增搜索调研 MCP 插件 `Rexion-plugin-search`（3 个工具）
- 新增 PPT 生成 Skill `generate-ppt`
- 新增竞品调研 Skill `research-report`
- 增强 Composer 输入框：附件上传、拖拽文件引用
- 数据模型扩展：新增 scheduled_tasks、todos、notifications 表

### P6：个人化 + 自动化 + 远程

- 新增日程管理 MCP 插件 `Rexion-plugin-calendar`
- 新增邮件处理 MCP 插件 `Rexion-plugin-mail`（IMAP/SMTP）
- 新增 IM 远程控制 MCP 插件 `Rexion-plugin-im`（企业微信/飞书/钉钉）
- 新增剪贴板助手：全局热键 Ctrl+Shift+R + 悬浮窗
- 新增定时任务调度器 + SchedulerPanel UI
- 新增长期记忆增强：个人知识库（people/projects/preferences/writing_style）
- 新增通知系统：定时任务完成/后台任务进度通知

## Impact

- **Affected specs**: SPEC.md（新增 Tool/Plugin 注册）、personal-agent-roadmap.md（P5/P6 继承扩展）、office-capabilities-guide.md（新增插件使用手册）
- **Affected code**:
  - `desktop/frontend/src/App.tsx`：布局全面重构（当前 1845 行单文件）
  - `desktop/frontend/src/lib/types.ts`：新增 WorkspaceType "assistant"、扩展 WireEvent 类型
  - `desktop/frontend/src/lib/bridge.ts`：新增 Wails 绑定方法
  - `desktop/frontend/src/lib/theme.ts`：新增亮色默认逻辑 + 办公风格
  - `desktop/frontend/src/components/`：新增 10+ 组件
  - `desktop/app.go`：新增 Wails 导出方法
  - `internal/control/`：意图分类器、模式路由
  - `internal/skill/`：新增 Skill 定义
  - `cmd/`：新增 3 个 MCP 插件二进制
  - `internal/config/`：新增插件配置项、数据模型

---

## ADDED Requirements

### Requirement: 三模式切换器（ModeSwitcher）

系统 SHALL 提供编码（coding）、办公（office）、助手（assistant）三种工作模式切换器，位于顶栏中央区域。

#### Scenario: 模式切换
- **WHEN** 用户点击模式切换器选择「办公」模式
- **THEN** 侧边栏内容切换为办公模式专属内容（模板库、最近文档、文件整理），右侧面板切换为文档预览视图，主题自动切换为亮色（用户未手动覆盖时）

#### Scenario: 首次启动默认模式
- **WHEN** 新用户首次启动 Rexion
- **THEN** 根据新手引导中选择的身份决定默认模式（开发者→编码，办公人员→办公，自由职业者→助手）

#### Scenario: 编码模式零回归
- **WHEN** 用户在编码模式下操作
- **THEN** 所有现有编码功能（项目树、代码 Diff、Git 操作、终端、Skill 等）行为与改造前完全一致

---

### Requirement: 动态侧边栏

系统 SHALL 提供随模式动态变化的侧边栏，分为四个区域：首页、工作台、资源、管理。

#### Scenario: 编码模式侧边栏
- **WHEN** 当前模式为「编码」
- **THEN** 侧边栏显示：首页入口、编码工作台、项目树、Skill 模板、开发工具插件市场、历史记录、回收站、设置

#### Scenario: 办公模式侧边栏
- **WHEN** 当前模式为「办公」
- **THEN** 侧边栏显示：首页入口、办公工作台、文档/PPT 模板库、最近文档、办公工具插件市场、历史记录、回收站、设置

#### Scenario: 助手模式侧边栏
- **WHEN** 当前模式为「助手」
- **THEN** 侧边栏显示：首页入口、助手工作台、日程、待办、信息流、生活工具插件市场、历史记录、回收站、设置

---

### Requirement: 首页面板（HomePanel）

系统 SHALL 提供首页面板，作为用户启动后的默认视图，包含问候语、快捷操作、最近任务、常用 Skill。

#### Scenario: 首页展示
- **WHEN** 用户点击侧边栏「首页」或启动 Rexion
- **THEN** 显示时间感知问候语、3-4 个基于身份推荐的快捷操作卡片、最近任务列表（含状态和时间）、常用 Skill 快捷入口、每日提示

#### Scenario: 快捷操作触发
- **WHEN** 用户点击快捷操作卡片「分析一个表格」
- **THEN** 自动切换到办公模式，创建新对话，并在输入框预填对应 Skill 命令

---

### Requirement: 右侧预览面板

系统 SHALL 在右侧面板新增「预览」Tab，支持 docx、xlsx、pptx、pdf 的内联预览。

#### Scenario: 文档预览
- **WHEN** Agent 生成了 .docx 文件
- **THEN** 右侧面板自动切换到预览 Tab，渲染 docx 的 HTML 分页预览（前 5 页），并提供「打开」「导出到工作区」按钮

#### Scenario: 表格预览
- **WHEN** Agent 生成了 .xlsx 文件
- **THEN** 右侧面板显示简易表格视图，支持排序和筛选

#### Scenario: PPT 预览
- **WHEN** Agent 生成了 .pptx 文件
- **THEN** 右侧面板显示幻灯片缩略图网格

#### Scenario: PDF 预览
- **WHEN** Agent 生成了 .pdf 文件或用户打开 PDF 文件
- **THEN** 右侧面板显示分页图片预览

---

### Requirement: 任务进度条（ProgressStepper）

系统 SHALL 在 Agent 执行多步任务时，在主对话区顶部显示任务进度。

#### Scenario: 多步任务执行
- **WHEN** Agent 执行包含多个步骤的任务
- **THEN** 顶部显示进度条，包括：总进度百分比、当前步骤编号/总步骤数、每个步骤的完成状态（✅完成 / ⏳进行中 / ⬚待执行）、当前步骤描述

#### Scenario: 步骤点击跳转
- **WHEN** 用户点击进度条中某个已完成步骤
- **THEN** 主对话区滚动到该步骤对应的对话消息位置

---

### Requirement: 富媒体工具卡片（RichToolCard）

系统 SHALL 将 Agent 工具调用结果渲染为结构化富媒体卡片，替代纯文本输出。

#### Scenario: 文档卡片
- **WHEN** 工具返回 .docx 文件
- **THEN** 渲染文档卡片：缩略图预览 + 文件名 + 「打开」「导出」「编辑」按钮

#### Scenario: 表格卡片
- **WHEN** 工具返回 .xlsx 文件
- **THEN** 渲染表格卡片：内嵌小型表格视图（前 10 行）+ 排序/筛选 + 「打开」「导出」按钮

#### Scenario: 图表卡片
- **WHEN** 工具返回图表图片
- **THEN** 渲染图表卡片：内嵌图片 + 「保存为 PNG」按钮

#### Scenario: 代码卡片
- **WHEN** 工具返回代码变更
- **THEN** 渲染代码卡片：语法高亮 + Diff 视图 + 「应用更改」按钮

---

### Requirement: 新手引导流程（OnboardingFlow）

系统 SHALL 提供新手引导，使新用户 3 分钟内完成首次任务。

#### Scenario: 首次启动引导
- **WHEN** 用户首次启动 Rexion（无历史配置）
- **THEN** 显示欢迎页 → 身份选择（开发者/办公人员/自由职业者/其他）→ API Key 配置（支持扫码/粘贴，可选本地模型跳过）→ 引导式首次任务 → 进入首页

#### Scenario: 身份关联引导任务
- **WHEN** 用户选择「办公人员」身份
- **THEN** 引导任务为「让 Rexion 帮你分析一个表格」，预置示例文件

#### Scenario: 跳过引导
- **WHEN** 用户点击「跳过」
- **THEN** 直接进入首页，身份默认为「其他」，模式默认为「编码」

---

### Requirement: 意图分类器（IntentClassifier）

系统 SHALL 自动识别用户输入的意图，将请求路由到对应工作模式。

#### Scenario: 编码意图识别
- **WHEN** 用户输入包含编码相关关键词（代码、函数、Bug、编译、测试、重构等）
- **THEN** 意图分类器将其路由到编码模式，如当前不在编码模式则建议切换

#### Scenario: 办公意图识别
- **WHEN** 用户输入包含办公相关关键词（周报、表格、PPT、合同、纪要、分析等）
- **THEN** 意图分类器将其路由到办公模式

#### Scenario: 助手意图识别
- **WHEN** 用户输入包含助手相关关键词（帮我查、提醒我、整理、发邮件、安排等）
- **THEN** 意图分类器将其路由到助手模式

#### Scenario: 关闭自动路由
- **WHEN** 用户在设置中关闭「自动模式切换」
- **THEN** 意图分类器仅展示分类结果提示，不自动切换模式

---

### Requirement: PPT 生成插件（Rexion-plugin-slides）

系统 SHALL 提供 PPT 生成 MCP 插件，支持从 Markdown 大纲生成专业 PPT。

#### Scenario: 创建 PPT
- **WHEN** Agent 调用 `mcp__slides__create_ppt` 工具，传入 Markdown 大纲和主题参数
- **THEN** 生成 .pptx 文件并返回文件路径，每页文字不超过 100 字，支持 professional/creative/minimal 三种风格

#### Scenario: 添加幻灯片
- **WHEN** Agent 调用 `mcp__slides__add_slide` 工具
- **THEN** 向已有 PPT 追加一页幻灯片，支持标题+正文+备注格式

#### Scenario: 应用主题
- **WHEN** Agent 调用 `mcp__slides__apply_theme` 工具
- **THEN** 对已有 PPT 应用指定主题模板，统一字体/配色/布局

#### Scenario: 插入图表
- **WHEN** Agent 调用 `mcp__slides__add_chart` 工具
- **THEN** 在 PPT 中插入图表页，支持柱状图/折线图/饼图

#### Scenario: 导出 PDF
- **WHEN** Agent 调用 `mcp__slides__export_pdf` 工具
- **THEN** 将 .pptx 导出为 .pdf 文件

#### Scenario: Skill 驱动生成
- **WHEN** 用户说「帮我做一个关于AI行业趋势的PPT，20页左右，专业风格」
- **THEN** Agent 通过 `generate-ppt` Skill 自动规划大纲→调研内容→调用 PPT 插件→返回 .pptx + 预览

---

### Requirement: 搜索调研插件（Rexion-plugin-search）

系统 SHALL 提供搜索调研 MCP 插件，支持自动搜索、整理、分析信息。

#### Scenario: 搜索查询
- **WHEN** Agent 调用 `web_search` 工具，传入查询关键词
- **THEN** 返回搜索结果列表（标题、摘要、URL），至少 5 个来源

#### Scenario: 网页信息提取
- **WHEN** Agent 调用 `web_extract` 工具，传入 URL 和提取目标
- **THEN** 返回网页的结构化信息（基于 web_fetch + LLM 抽取）

#### Scenario: 对比表格生成
- **WHEN** Agent 调用 `compare_table` 工具，传入维度列表和数据
- **THEN** 生成 Markdown 对比表格并可选导出为 .xlsx

#### Scenario: Skill 驱动调研
- **WHEN** 用户说「帮我调研一下2026年主流AI Agent产品」
- **THEN** Agent 通过 `research-report` Skill 自动拆解维度→并行搜索→提取信息→生成 docx 报告 + xlsx 对比表

---

### Requirement: 亮色主题与办公风格

系统 SHALL 新增亮色主题作为办公/助手模式的默认主题，并新增办公风格预设。

#### Scenario: 模式关联主题
- **WHEN** 用户切换到办公或助手模式
- **THEN** 主题自动切换为亮色（米白/浅灰 + 蓝色强调），除非用户在设置中手动覆盖

#### Scenario: 编码模式保持暗色
- **WHEN** 用户切换到编码模式
- **THEN** 主题自动切换为暗色（保持现有默认），除非用户手动覆盖

#### Scenario: 全局主题覆盖
- **WHEN** 用户在设置中启用「统一主题」
- **THEN** 所有模式使用同一主题，不再随模式切换

---

### Requirement: Composer 输入框增强

系统 SHALL 增强 Composer 输入框，支持附件上传和文件拖拽引用。

#### Scenario: 附件上传
- **WHEN** 用户点击输入框附件按钮 📎
- **THEN** 弹出文件选择对话框，支持选择图片/PDF/Office 文件作为附件

#### Scenario: 文件拖拽引用
- **WHEN** 用户将文件从系统文件管理器拖拽到输入框
- **THEN** 自动创建 @文件引用，支持图片/PDF/Office 文件

---

### Requirement: 数据模型扩展

系统 SHALL 新增定时任务、待办事项、通知记录的数据存储。

#### Scenario: 定时任务存储
- **WHEN** 用户创建定时任务
- **THEN** 存储到 scheduled_tasks 表（id, name, cron, skill, parameters, enabled, last_run, next_run, created_at）

#### Scenario: 待办事项存储
- **WHEN** 用户通过对话创建待办
- **THEN** 存储到 todos 表（id, title, description, due_date, priority, status, source, created_at, updated_at）

#### Scenario: 通知记录存储
- **WHEN** 系统产生通知（定时任务完成、后台任务进度等）
- **THEN** 存储到 notifications 表（id, kind, title, body, read, created_at）

---

### Requirement: 日程管理插件（Rexion-plugin-calendar）【P6】

系统 SHALL 提供日程管理 MCP 插件，支持通过对话管理日程和待办。

#### Scenario: 创建提醒
- **WHEN** 用户说「提醒我明天下午3点开会」
- **THEN** 创建待办事项并设置提醒时间

#### Scenario: 查看待办
- **WHEN** 用户说「下周有哪些待办？」
- **THEN** 返回下周待办列表，按时间排序

#### Scenario: 系统日历集成
- **WHEN** Agent 调用 `read_event` 工具
- **THEN** 读取系统日历事件（macOS Calendar / Windows Outlook）

---

### Requirement: 邮件处理插件（Rexion-plugin-mail）【P6】

系统 SHALL 提供邮件处理 MCP 插件，支持 IMAP/SMTP 协议。

#### Scenario: 邮件摘要
- **WHEN** 用户说「帮我看看今天的邮件」
- **THEN** 读取收件箱，LLM 生成摘要

#### Scenario: 起草回复
- **WHEN** 用户说「回复这封邮件，确认会议时间」
- **THEN** 起草回复邮件，展示给用户确认后发送

#### Scenario: 批量处理
- **WHEN** 用户说「把所有发票邮件整理成一个表格」
- **THEN** 批量扫描邮件，提取发票信息，生成 xlsx

---

### Requirement: IM 远程控制插件（Rexion-plugin-im）【P6】

系统 SHALL 提供 IM 远程控制 MCP 插件，支持通过企业微信/飞书/钉钉远程操控 Rexion。

#### Scenario: 微信远程执行
- **WHEN** 用户通过企业微信机器人发送「帮我生成今天的周报」
- **THEN** 本地 IM Bot 服务解析指令→创建会话任务→Agent 执行→推送结果通知到微信

#### Scenario: 飞书远程执行
- **WHEN** 用户通过飞书机器人发送指令
- **THEN** 同上流程，通过飞书 API 推送结果

#### Scenario: 本地回调安全
- **WHEN** IM 指令到达
- **THEN** 所有连接走本地 HTTP 回调，不经过第三方服务器

---

### Requirement: 剪贴板助手【P6】

系统 SHALL 提供全局快捷键呼出的剪贴板助手悬浮窗。

#### Scenario: 全局热键呼出
- **WHEN** 用户按下 Ctrl+Shift+R
- **THEN** 弹出悬浮窗，读取剪贴板内容，展示处理选项（翻译/总结/润色/续写/解释/优化等）

#### Scenario: 处理剪贴板内容
- **WHEN** 用户在悬浮窗选择处理方式
- **THEN** 将剪贴板内容发送给 Agent 处理，结果自动写入剪贴板

#### Scenario: 响应时间
- **WHEN** 悬浮窗弹出
- **THEN** 2 秒内展示处理选项和剪贴板内容预览

---

### Requirement: 定时任务调度器【P6】

系统 SHALL 提供定时任务调度器和 SchedulerPanel UI。

#### Scenario: 创建定时任务
- **WHEN** 用户在 SchedulerPanel 点击「新增」
- **THEN** 弹出表单：任务名称、Cron 表达式、Skill 选择、参数配置

#### Scenario: 定时执行
- **WHEN** Cron 调度器触发
- **THEN** 自动执行对应 Skill，记录执行日志，发送完成通知

#### Scenario: 任务管理
- **WHEN** 用户查看 SchedulerPanel
- **THEN** 显示所有定时任务列表（名称、Cron、Skill、上次执行时间、状态），支持编辑/暂停/删除

---

### Requirement: 长期记忆增强【P6】

系统 SHALL 提供个人知识库，存储在 `~/.Rexion/memory/` 目录下。

#### Scenario: 记忆文件
- **WHEN** 系统初始化
- **THEN** 创建记忆文件：people.md（常联系人）、projects.md（在跟项目）、preferences.md（个人偏好）、writing_style.md（写作风格）

#### Scenario: Agent 引用记忆
- **WHEN** 用户说「按我喜欢的风格写」
- **THEN** Agent 自动读取 preferences.md 和 writing_style.md 作为上下文

#### Scenario: 记忆面板
- **WHEN** 用户打开记忆面板
- **THEN** 展示所有记忆文件内容，支持多文件切换和编辑

---

## MODIFIED Requirements

### Requirement: WorkspaceType 扩展

当前 WorkspaceType 仅有 "coding" | "office" 两种值。扩展为 "coding" | "office" | "assistant" 三种值，与 ModeSwitcher 三模式对齐。

#### Scenario: 三种工作区类型
- **WHEN** 用户切换模式
- **THEN** WorkspaceType 同步更新为 coding/office/assistant，后端 Controller 接收新类型并调整可用工具集

### Requirement: 侧边栏动态渲染

当前侧边栏硬编码在 App.tsx 中。重构为动态渲染模式，根据 WorkspaceType 决定侧边栏内容。

#### Scenario: 侧边栏组件化
- **WHEN** WorkspaceType 变化
- **THEN** 侧边栏渲染对应模式的组件集合，隐藏不相关项

### Requirement: 右侧面板扩展

当前右侧面板有 Files/Changed/Context 三个 Tab（Context 被隐藏）。扩展为预览/文件/变更/上下文四个 Tab。

#### Scenario: 新增预览 Tab
- **WHEN** 右侧面板激活
- **THEN** 第一个 Tab 为「预览」，根据当前模式显示不同内容（编码→Diff，办公→文档/PPT/表格预览，助手→网页摘要）

### Requirement: 主题自动切换

当前主题切换为手动操作。新增模式关联主题自动切换逻辑。

#### Scenario: 模式切换联动
- **WHEN** 用户切换模式且未启用「统一主题」
- **THEN** 编码模式→暗色，办公/助手模式→亮色

## REMOVED Requirements

（无移除项，所有改造为增量式，保持向后兼容）

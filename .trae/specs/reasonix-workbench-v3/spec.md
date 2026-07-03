# Reasonix Workbench v3.0 设计规范

> 版本：v3.0 | 日期：2026-07-02 | 状态：规划草案
>
> 定位：在 v0.1（办公插件）、v1.0（安全/引擎）、v2.0（ToC 三模式 + PPT/调研/IM）之上，
> 聚焦「激活已搭好的骨架、闭环跨模块体验、深化个人助手能力、打磨日常使用细节」，
> 把 Reasonix 从「功能堆砌完成度 80%」推进到「日常可用、离不开、能成长」的专属工作台。

---

## 0. 文档定位

- **读者**：Reasonix 维护者、贡献者、个人 Agent 使用者
- **状态**：规划草案（Draft）
- **关联文档**：
  - [personal-agent-roadmap.md](../../../docs/personal-agent-roadmap.md) — v0.1，P1-P6 办公能力扩展（已完成）
  - [plan-p0-p1.md](../../../docs/plan-p0-p1.md) — v1.0，安全沙盒与引擎增强
  - [toc-agent-roadmap.md](../../../docs/toc-agent-roadmap.md) — v2.0，ToC 三模式 + PPT/调研/IM（已完成）
  - [SPEC.md](../../../docs/SPEC.md) — 工程契约
- **变更原则**：本计划演进时，先更新本文档，再改代码
- **继承关系**：本文不重写 v2.0 的需求；v2.0 标记完成的项视为「骨架就位」，本文聚焦「激活、闭环、深化」

---

## 1. 现状评估（v3.0 起点）

基于 2026-07-02 的代码审查（非 tasks.md 标记，而是实际代码核对），得出以下真实完成度：

### 1.1 已真实可用（无需重做）

| 能力 | 关键证据 | 备注 |
|------|----------|------|
| PPT 插件（5 工具） | `cmd/reasonix-plugin-slides/slides.go:136-568` 真实 unioffice 实现 | 需 UNIDOC_LICENSE_API_KEY，缺失则加水印 |
| 日历插件（5 工具） | `calendar.go:88-265` Windows Outlook COM + macOS AppleScript；`todo.go` SQLite | 跨平台真实可用 |
| IM 插件（5 工具） | `bot.go:118-178` HTTP 服务器；WeCom/Feishu/DingTalk 真实 webhook | 钉钉含 HMAC-SHA256 签名 |
| 定时任务调度 | `internal/scheduler/scheduler.go:14` robfig/cron 真实引擎；`desktop/app.go:4277-4347` 完整 CRUD | 前后端联动可用 |
| 意图分类器 | `internal/control/intent_classifier.go:76-168` 规则引擎真实接入 `Controller.Send` | 发 `IntentClassified` 事件 |
| step_progress emitter | `controller.go:2013-2031` `emitStepProgress` 真实发射 | 计划批准/完成时触发（v2.0 已激活，非 scaffold） |
| 三模式 UI / HomePanel / ProgressStepper / RichToolCard / SchedulerPanel / FloatingWindow / AgentCanvas | 组件文件均存在且有真实实现 | 部分联动待加强 |

### 1.2 骨架就位但未激活（v3.0 重点）

| 短板 | 证据 | 影响 |
|------|------|------|
| **个人知识库未激活** | `internal/memory/memory.go:175-245` 定义了 `people.md`/`projects.md`/`preferences.md`/`writing_style.md` 四个文件和 `EnsureMemoryDir()`，但**全仓库无调用者**；`memory.Load()` 也不读这些文件 | Agent 无法感知用户身份、写作风格、常联系人 → 「专属助手」无从谈起 |
| **邮件 OAuth2 未闭环** | `cmd/reasonix-plugin-mail/oauth2.go:134-141` 的 `OAuth2IMAPAuthString`/`OAuth2SMTPAuthString`/`GetOAuth2AccessToken` 是死代码，`read_mail`/`send_mail` 仍走 `MAIL_IMAP_PASS` 环境变量密码 | Gmail/Outlook 用户必须用应用专用密码，无法用 OAuth2 |
| **ProgressStepper 联动不足** | `ProgressStepper.tsx` 的 `onStepClick` 是 planned future enhancement；AgentCanvas 仅靠 ToolDispatch/Result `parentId` 嵌套，不渲染 step 节点 | 用户看不到任务全貌，无法跳转回看某步骤 |
| **信息流被删后无替代** | 上次会话从 Sidebar 移除了 `feed` 导航项（无后端实现） | 助手模式缺少「日常信息聚合」入口 |
| **跨会话任务状态丢失** | 历史会话恢复后，ProgressStepper 不会重新加载该会话的步骤状态 | 长任务断点续看体验差 |

### 1.3 体验缺口（v3.0 补齐）

| 缺口 | 现状 |
|------|------|
| 模式切换仅切 UI | WorkspaceType 切换后 Composer 提示词、Skill 推荐、最近任务过滤均未联动 |
| SchedulerPanel 触发的 Skill 在新 Tab 自动打开 | 调度器执行任务时无 UI 反馈，仅发通知 |
| 富媒体卡片与右面板预览未协同 | 点击文档/表格卡片不会在右面板打开预览 |
| 新手引导无反馈回路 | OnboardingFlow 完成身份选择后无「完成首任务」的进度反馈 |
| 个人偏好无自动学习 | 对话中「我是…/我喜欢…/我负责…」等声明不会写入 `preferences.md` |

---

## 2. 设计原则（继承 v2.0 §3）

1. **激活优先于新增** — 已搭好的骨架（个人知识库、邮件 OAuth2、ProgressStepper）优先激活，不新造轮子
2. **闭环优先于扩展** — 跨模块联动（模式↔Composer、卡片↔预览、Scheduler↔Tab）优先于新功能
3. **本地优先** — 个人知识库、剪贴板历史、Recipe 全部本地存储，不上云
4. **零回归** — 编码模式所有现有功能不可降级；每个 Phase 含回归测试
5. **缓存友好** — 个人知识库注入系统提示时，按 `Docs` 同样的 prefix 注入，保持 byte-stable
6. **渐进式** — 每个 Phase 独立交付、独立验收

---

## 3. 总体架构（v3.0 增量）

```
┌──────────────────────────────────────────────────────────────────────┐
│                      桌面客户端 (Wails) — v3.0 增强                    │
│                                                                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐    │
│  │ 编码视图  │  │ 办公视图  │  │ 助手视图  │  │  悬浮窗(剪贴板)   │    │
│  │          │  │          │  │ +日报面板 │  │  +剪贴板历史      │    │
│  └──────────┘  └──────────┘  └──────────┘  └──────────────────┘    │
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │           共享 UI 组件层 — v3.0 增强联动                          │  │
│  │  Transcript · Composer(模式联动) · DocPreviewer(卡片协同)        │  │
│  │  ProgressStepper(onStepClick) · AgentCanvas(step 节点)          │  │
│  │  SchedulerPanel(新 Tab 自动打开) · DailyBriefPanel(替代信息流)   │  │
│  └────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────┬───────────────────────────────────┘
                                   │ Wails Bound Methods
┌──────────────────────────────────▼───────────────────────────────────┐
│                     Reasonix Core (Go) — v3.0 增强                    │
│                                                                      │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │
│  │  Agent   │ │ Provider │ │   Tool   │ │  Skill   │ │Permission│   │
│  │  引擎    │ │  注册表   │ │  注册表   │ │  加载器   │ │  引擎    │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘ └──────────┘   │
│                                                                      │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │
│  │  沙箱    │ │ Memory✨ │ │  Hook    │ │  调度器   │ │CodeGraph │   │
│  │          │ │ +PKM激活 │ │  +Recipe │ │  +Tab联动 │ │          │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘ └──────────┘   │
└──────────────────────────────────┬───────────────────────────────────┘
                                   │ MCP (stdio / HTTP)
┌──────────────────────────────────▼───────────────────────────────────┐
│                  MCP 插件生态 — v3.0 修补                              │
│                                                                      │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌──────────────┐   │
│  │   Office    │ │   Sheet     │ │   Slides    │ │    Mail ✨    │   │
│  │             │ │             │ │             │ │ OAuth2 闭环  │   │
│  └─────────────┘ └─────────────┘ └─────────────┘ └──────────────┘   │
│  └─────────────┐ ┌─────────────┐ ┌─────────────┐                    │
│  │   Search    │ │  Calendar   │ │   IM Bot    │                    │
│  └─────────────┘ └─────────────┘ └─────────────┘                    │
└──────────────────────────────────────────────────────────────────────┘

✨ = v3.0 增强点
```

---

## 4. 分阶段路线图

### 总览

| Phase | 主题 | 核心交付 | 侵入度 | 依赖 |
|-------|------|---------|--------|------|
| **P8** | 激活与修补 | 个人知识库激活、邮件 OAuth2 闭环、ProgressStepper+AgentCanvas 联动、DailyBrief 替代信息流 | 低（多为已有代码接线） | 无 |
| **P9** | 体验闭环 | 模式深度联动、卡片↔预览协同、Scheduler→新 Tab、跨会话任务恢复、新手反馈回路 | 中（前端为主） | P8 |
| **P10** | 个人助手深化 | PKM 自动学习、自动化 Recipe、剪贴板历史、语音输入 | 中（新模块） | P8 |
| **P11** | 编码能力再增强 | 多 Agent 并行（落地 P1-2）、测试生成 Skill、PR 审查 Skill | 中（internal/agent） | 无（独立） |
| **P12** | 生态与社区（长期展望） | Skill 市场、插件市场、模板库扩展、主题市场 | — | P8-P11 |

> P8 投入最小、收益最大：把已有骨架接上线即可；P9 把体验打磨到「离不开」；P10 让助手真的「懂你」；P11 守住编码核心能力不停滞。

---

## 5. ADDED Requirements

> 采用 EARS（Easy Approach to Requirements Syntax）格式。每条需求带可验收 Scenario。

---

### Requirement: 个人知识库激活（PKM Activation）

系统 SHALL 在 Reasonix 启动时调用 `memory.EnsureMemoryDir()` 创建 `~/.reasonix/memory/` 目录及四个默认文件（`people.md`、`projects.md`、`preferences.md`、`writing_style.md`），并在 `memory.Set.Load()` 时把这四个文件的内容作为 `Docs` 的一部分注入到系统提示前缀。

#### Scenario: 首次启动创建默认 PKM
- **WHEN** 用户首次启动 Reasonix 且 `~/.reasonix/memory/` 不存在
- **THEN** 系统创建目录并写入四个默认模板文件（含中文示例提示）
- **AND** Agent 在首轮对话的系统提示中包含这四个文件内容
- **AND** 前端 MemoryPanel 显示「个人知识库」分区，可切换编辑这四个文件

#### Scenario: 用户编辑 PKM 后立即生效
- **WHEN** 用户在 MemoryPanel 编辑 `preferences.md` 并保存
- **THEN** 下一轮对话的系统提示包含更新后的内容
- **AND** 不破坏 DeepSeek 前缀缓存（通过把 PKM 放在 prefix 的固定位置实现）

#### Scenario: Agent 引用个人偏好
- **WHEN** 用户说「按我喜欢的风格写周报」
- **AND** `writing_style.md` 中记录了「偏好简洁、要点式、避免客套话」
- **THEN** Agent 生成的周报遵循该风格

---

### Requirement: 邮件 OAuth2 闭环

系统 SHALL 在 `reasonix-plugin-mail` 的 `read_mail` 和 `send_mail` 工具中支持 XOAUTH2 认证，复用 `oauth2_authorize`/`oauth2_callback` 已持久化的 token，使 Gmail/Outlook 用户无需应用专用密码。

#### Scenario: Gmail OAuth2 收邮件
- **WHEN** 用户已通过 `oauth2_authorize` 完成 Gmail 授权且 token 已持久化
- **AND** 调用 `read_mail` 时未设置 `MAIL_IMAP_PASS` 环境变量
- **THEN** 插件使用 XOAUTH2 协议用持久化的 access_token（必要时刷新）登录 IMAP
- **AND** 返回邮件列表

#### Scenario: token 过期自动刷新
- **WHEN** IMAP 登录时 access_token 已过期
- **THEN** 插件用 refresh_token 自动换取新 access_token
- **AND** 持久化新 token
- **AND** 重试登录

#### Scenario: 无 OAuth2 配置时回退
- **WHEN** 用户既未配置 OAuth2 也未设置 `MAIL_IMAP_PASS`
- **THEN** 工具返回明确错误：「请先配置 OAuth2（调用 oauth2_authorize）或设置 MAIL_IMAP_PASS 环境变量」

---

### Requirement: ProgressStepper 与 AgentCanvas 联动

系统 SHALL 在 `ProgressStepper` 的 `onStepClick` 回调中实现跳转到对应 turn 的消息，并 SHALL 在 `AgentCanvas` 中把 `StepProgress` 事件渲染为节点（与 ToolDispatch/Result 节点并列）。

#### Scenario: 点击已完成步骤跳转
- **WHEN** 用户点击 ProgressStepper 中某个 `status="completed"` 且 `turnIndex` 存在的步骤
- **THEN** Transcript 滚动到该 turnIndex 对应的消息
- **AND** 该消息高亮闪烁 1 次

#### Scenario: AgentCanvas 渲染 step 节点
- **WHEN** Controller emit `StepProgress` 事件
- **THEN** AgentCanvas 在当前任务节点下渲染一个 step 节点（label=步骤名，status=图标）
- **AND** step 节点与后续的 ToolDispatch 节点用边连接
- **AND** step 状态变更（pending→in_progress→completed）时节点图标实时更新

#### Scenario: 跨会话恢复步骤状态
- **WHEN** 用户打开一个历史会话
- **THEN** ProgressStepper 从该会话的 `StepProgress` 事件流重建步骤列表
- **AND** 所有步骤显示为 `completed`（历史会话已完成）

---

### Requirement: DailyBrief 面板（替代信息流）

系统 SHALL 在助手模式侧边栏新增「每日简报」入口（`DailyBriefPanel`），聚合当日待办、最近邮件摘要、定时任务执行结果、最近会话主题，作为被删除的「信息流」的替代方案。

#### Scenario: 打开每日简报
- **WHEN** 用户在助手模式点击侧边栏「每日简报」
- **THEN** 右面板显示今日待办（来自 calendar 插件的 `list_todo`）
- **AND** 显示今日未读邮件 Top 5 摘要（来自 mail 插件的 `search_mail`，按时间倒序）
- **AND** 显示昨日定时任务执行结果（来自 scheduler 的执行日志）
- **AND** 显示最近 3 个会话主题（来自 sessions 列表）

#### Scenario: 一键处理待办
- **WHEN** 用户点击简报中某条待办的「完成」按钮
- **THEN** 调用 calendar 插件的 `update_todo` 把状态改为 completed
- **AND** 简报列表实时更新

---

### Requirement: 模式深度联动

系统 SHALL 在 WorkspaceType 切换时联动 Composer 占位符、Skill 推荐列表、HomePanel 快捷操作、右面板默认 Tab。

#### Scenario: 切换到办公模式
- **WHEN** 用户从编码模式切换到办公模式
- **THEN** Composer 占位符变为「描述你要生成的文档或表格…」
- **AND** SlashMenu 顶部推荐 `weekly-report`、`sheet-analysis`、`meeting-minutes`、`contract-draft` 四个 Skill
- **AND** 右面板默认切换到「预览」Tab
- **AND** HomePanel 快捷操作切换为办公类（分析表格、生成周报、做 PPT、整理文档）

#### Scenario: 切换到助手模式
- **WHEN** 用户切换到助手模式
- **THEN** Composer 占位符变为「让我帮你查、提醒、整理…」
- **AND** SlashMenu 顶部推荐 `daily-brief`、`research-report`、`generate-ppt`、`sheet-analysis`
- **AND** 右面板默认切换到「每日简报」Tab
- **AND** HomePanel 快捷操作切换为助手类（搜索调研、总结文章、翻译、日程管理）

---

### Requirement: 富媒体卡片与右面板预览协同

系统 SHALL 在 `RichToolCard` 中点击文档/表格/图表/PPT 卡片的「预览」按钮时，自动在右面板打开对应预览组件。

#### Scenario: 点击文档卡片预览
- **WHEN** Agent 生成一个 docx 文件并在对话中显示文档卡片
- **AND** 用户点击卡片上的「预览」按钮
- **THEN** 右面板切换到「预览」Tab
- **AND** DocPreviewer 加载该 docx 文件并显示前 5 页

#### Scenario: 点击表格卡片预览
- **WHEN** 用户点击表格卡片的「预览」按钮
- **THEN** 右面板 SheetViewer 加载该 xlsx 文件并显示首个 sheet

---

### Requirement: Scheduler 触发的 Skill 在新 Tab 自动打开

系统 SHALL 在定时任务触发执行 Skill 时，自动在桌面端新建一个 Tab 并切换到该会话，使用户能实时看到执行过程。

#### Scenario: 定时任务触发
- **WHEN** cron 调度器在计划时间触发 `weekly-report` Skill
- **THEN** 桌面端自动新建一个 Tab，标题为「定时任务：weekly-report YYYY-MM-DD」
- **AND** 该 Tab 切换到前台
- **AND** Tab 内显示 Agent 执行过程（ProgressStepper + Transcript）
- **AND** 如果 Reasonix 窗口最小化到托盘，则弹出系统通知「定时任务已启动」

#### Scenario: 任务完成通知
- **WHEN** 定时任务执行的 Skill 完成
- **THEN** 弹出系统通知「weekly-report 已完成，点击查看」
- **AND** 点击通知跳转到该 Tab

---

### Requirement: PKM 自动学习

系统 SHALL 在对话中识别用户关于自身偏好、身份、关系的声明，并 SHALL 通过 ApprovalModal 询问用户是否写入对应的 PKM 文件。

#### Scenario: 识别写作偏好
- **WHEN** 用户在对话中说「我写报告喜欢用要点式，不要客套话」
- **THEN** Agent 识别为写作偏好声明
- **AND** 弹出 ApprovalModal：「检测到写作偏好，是否写入 `preferences.md`？」
- **AND** 用户确认后追加到 `~/.reasonix/memory/preferences.md` 的「写作风格」段落

#### Scenario: 识别联系人
- **WHEN** 用户说「张三是我领导，李四负责财务」
- **THEN** 弹出 ApprovalModal：「是否记录到 `people.md`？」
- **AND** 确认后追加到 `people.md` 的「同事」段落

#### Scenario: 用户拒绝学习
- **WHEN** 用户在 ApprovalModal 中点击「拒绝」
- **THEN** 不写入文件
- **AND** 本轮对话内不再对类似声明弹出询问（避免打扰）

---

### Requirement: 自动化 Recipe（工作流配方）

系统 SHALL 支持用户把一个成功的会话保存为「Recipe」（含 Skill 名、参数模板、触发条件），并 SHALL 在 SchedulerPanel 中可选作定时任务的内容。

#### Scenario: 保存会话为 Recipe
- **WHEN** 用户在一个成功的会话结束后点击「保存为 Recipe」
- **THEN** 弹出表单：名称、描述、参数模板（从对话中提取）、可选触发条件（手动/定时/事件）
- **AND** 保存到 `~/.reasonix/recipes/<name>.json`

#### Scenario: 从 Recipe 创建定时任务
- **WHEN** 用户在 SchedulerPanel 点击「新建定时任务」
- **THEN** 表单中可选「从 Recipe 选择」
- **AND** 选择后自动填充 Skill 名和参数模板

#### Scenario: 事件触发 Recipe
- **WHEN** Recipe 配置了触发条件「收到含『发票』的邮件」
- **AND** mail 插件检测到匹配邮件
- **THEN** 自动创建一个会话执行该 Recipe
- **AND** 弹出通知「事件触发 Recipe：发票整理」

---

### Requirement: 剪贴板历史

系统 SHALL 在 FloatingWindow 中增加「剪贴板历史」分区，保留最近 N 天（默认 7 天，可配置）的剪贴板条目，支持搜索和重新复制。

#### Scenario: 记录剪贴板
- **WHEN** 用户复制任意内容到系统剪贴板
- **THEN** FloatingWindow 后台记录该条目（文本/图片/文件路径）
- **AND** 持久化到 `~/.reasonix/clipboard_history.db`（SQLite）

#### Scenario: 搜索历史
- **WHEN** 用户在 FloatingWindow 中输入关键词搜索剪贴板历史
- **THEN** 显示匹配的条目列表（按时间倒序）
- **AND** 点击条目可重新复制到剪贴板

#### Scenario: 自动清理
- **WHEN** 剪贴板历史条目超过 N 天
- **THEN** 自动清理（启动时检查）

---

### Requirement: 语音输入

系统 SHALL 支持通过全局热键（默认 `Ctrl+Shift+V`）启动语音输入，使用本地 whisper 模型转写，结果填入 Composer。

#### Scenario: 语音输入
- **WHEN** 用户按下 `Ctrl+Shift+V`
- **THEN** 显示录音浮层（红色圆点 + 波形）
- **AND** 开始录音
- **AND** 用户再次按键或停止说话 2 秒后结束录音
- **AND** 调用本地 whisper 转写
- **AND** 结果填入 Composer 输入框

#### Scenario: whisper 不可用时降级
- **WHEN** 系统未安装 whisper
- **THEN** 弹出提示：「语音输入需要本地 whisper 模型，点击查看安装指引」
- **AND** 不阻塞其他功能

---

### Requirement: 多 Agent 并行（落地 P1-2）

系统 SHALL 实现 `plan-p0-p1.md` 中 P1-2 描述的多 Agent 并行编排，支持 spawn_agent/wait_agent/send_input/close_agent 四原语，并 SHALL 在 AgentCanvas 中以子图形式渲染子 Agent。

#### Scenario: 并行 spawn 两个子 Agent
- **WHEN** 主 Agent 调用 `spawn_agent` 同时启动两个 worker
- **THEN** 两个子 Agent 独立 Session 并行执行
- **AND** AgentCanvas 在主 Agent 节点下渲染两个子图
- **AND** 子图内显示各自的 ToolDispatch/Result 节点

#### Scenario: wait_agent 等待
- **WHEN** 主 Agent 调用 `wait_agent` 等待某个子 Agent
- **THEN** 阻塞直到该子 Agent 完成
- **AND** 返回结构化结果（summary、tool_calls、files_write 等）

---

### Requirement: 编码 Skill 增强

系统 SHALL 新增两个编码类 Skill：`generate-tests`（为指定函数/包生成测试）和 `review-pr`（审查 Git diff 或 PR）。

#### Scenario: 生成测试
- **WHEN** 用户说「为 `internal/control/controller.go` 的 `Send` 方法生成测试」
- **THEN** 触发 `generate-tests` Skill
- **AND** Skill 读取目标函数、分析签名、生成 table-driven 测试
- **AND** 写入到对应的 `_test.go` 文件

#### Scenario: PR 审查
- **WHEN** 用户说「审查当前分支相对于 main 的 diff」
- **THEN** 触发 `review-pr` Skill
- **AND** Skill 调用 `git diff main...HEAD` 获取变更
- **AND** 逐文件分析变更，输出审查意见（问题/建议/风险）
- **AND** 审查意见以 Markdown 报告形式展示

---

## 6. Impact

### 6.1 Affected specs

- [SPEC.md](../../../docs/SPEC.md) — Memory 章节需补充 PKM 注入规则
- [personal-agent-roadmap.md](../../../docs/personal-agent-roadmap.md) — P6 长期记忆增强被本文 P8/P10 落地
- [plan-p0-p1.md](../../../docs/plan-p0-p1.md) — P1-2 多 Agent 并行被本文 P11 落地
- [toc-agent-roadmap.md](../../../docs/toc-agent-roadmap.md) — 信息流被删除，本文 DailyBrief 替代

### 6.2 Affected code

**P8 激活与修补**：
- `internal/memory/memory.go` — `Load()` 注入 PKM 四文件；新增 `EnsureMemoryDir()` 调用点
- `internal/boot/boot.go` — 启动时调用 `EnsureMemoryDir()`
- `cmd/reasonix-plugin-mail/oauth2.go` — 死代码激活，接入 `imap.go`/`smtp.go`
- `cmd/reasonix-plugin-mail/imap.go` + `smtp.go` — 增加 XOAUTH2 认证路径
- `desktop/frontend/src/components/ProgressStepper.tsx` — `onStepClick` 实现
- `desktop/frontend/src/components/AgentCanvas.tsx` + `lib/agentGraph.ts` — step 节点渲染
- `desktop/frontend/src/components/DailyBriefPanel.tsx` — 新增
- `desktop/frontend/src/components/Sidebar.tsx` — 助手模式新增「每日简报」入口
- `desktop/frontend/src/lib/types.ts` + `lib/i18n.tsx` — 新类型与翻译

**P9 体验闭环**：
- `desktop/frontend/src/components/Composer.tsx` — 占位符随模式联动
- `desktop/frontend/src/components/SlashMenu.tsx` — Skill 推荐随模式
- `desktop/frontend/src/components/RichToolCard.tsx` — 卡片「预览」按钮接右面板
- `desktop/frontend/src/App.tsx` — Scheduler 触发新 Tab、跨会话 ProgressStepper 恢复
- `desktop/app.go` — 新增 `OpenTabForScheduledTask` 等 bound method
- `desktop/frontend/src/components/OnboardingOverlay.tsx` — 完成首任务反馈回路

**P10 个人助手深化**：
- `internal/memory/autolearn.go` — 新增，PKM 自动学习
- `internal/control/controller.go` — 对话后触发 autolearn
- `internal/recipe/` — 新增包，Recipe 存储/加载/触发
- `desktop/frontend/src/components/SchedulerPanel.tsx` — Recipe 选择
- `desktop/clipboard_history.go` — 新增，剪贴板历史 SQLite
- `desktop/frontend/src/components/FloatingWindow.tsx` — 剪贴板历史分区
- `desktop/voice_input.go` — 新增，whisper 调用
- `desktop/frontend/src/components/Composer.tsx` — 语音输入按钮

**P11 编码能力再增强**：
- `internal/agent/pool.go` + `role.go` + `spawn.go` — 多 Agent 并行
- `internal/agent/task.go` — 升级为完整子 Agent
- `internal/event/event.go` — AgentSpawned/Progress/Completed 事件
- `desktop/frontend/src/components/AgentCanvas.tsx` — 子图渲染
- `.reasonix/skills/generate-tests.md` — 新增 Skill
- `.reasonix/skills/review-pr.md` — 新增 Skill

### 6.3 配置扩展

```toml
# ~/.reasonix/config.toml（新增节）

[pkm]
enabled = true                 # 默认 true
auto_learn = true              # 自动学习用户声明
auto_learn_confirm = true      # 学习前需 ApprovalModal 确认

[clipboard_history]
enabled = true
retention_days = 7
max_entries = 1000

[voice_input]
enabled = false                # 默认 false，需安装 whisper
whisper_path = ""              # whisper 可执行文件路径
hotkey = "Ctrl+Shift+V"

[recipe]
enabled = true
storage_dir = "~/.reasonix/recipes"

[agents]
max_threads = 6
max_depth = 1
job_timeout = "5m"
```

---

## 7. 风险与约束

### 7.1 技术风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| PKM 注入破坏前缀缓存 | 成本上升 | PKM 放在 prefix 固定位置；首次加载后 byte-stable；编辑后接受一次缓存失效 |
| whisper 依赖外部二进制 | 部分用户不可用 | 默认禁用；缺失时降级提示，不阻塞 |
| 多 Agent 并行引发竞态 | 数据损坏 | 子 Agent 独立 Session；写操作走 ApprovalModal |
| 剪贴板历史占空间 | 磁盘膨胀 | SQLite 自动清理；上限 1000 条 |
| OAuth2 token 刷新失败 | 邮件功能不可用 | 明确报错引导用户重新授权 |

### 7.2 设计约束（不可违背）

1. **零回归** — 编码模式所有现有功能不因 P8-P11 降级
2. **本地优先** — PKM、Recipe、剪贴板历史、语音转写全部本地，不上云
3. **`CGO_ENABLED=0`** — 所有新增依赖必须纯 Go（whisper 通过子进程调用，非 cgo）
4. **插件隔离** — 邮件 OAuth2 修补只在 `cmd/reasonix-plugin-mail/` 内，不侵入主仓 `internal/`
5. **配置驱动** — 所有新功能可通过配置开关，禁用后行为与改造前一致
6. **缓存友好** — PKM 注入系统提示时，位置和顺序固定，编辑后才失效

---

## 8. 不做的事情（明确排除）

1. **不**做云端 PKM 同步 — 个人知识库不上云（端到端加密同步留作 P12 长期展望）
2. **不**重写 Memory 接口 — 现有 `Docs` + `Store` 抽象足够，PKM 作为 `Docs` 的扩展
3. **不**做图形化 Recipe 编辑器 — Recipe 通过表单创建，保持声明式
4. **不**做语音克隆/TTS — 只做 ASR（语音转文字），不做 TTS（文字转语音）
5. **不**把 whisper 打进主二进制 — 外部依赖探测
6. **不**重写 AgentCanvas — 在现有 `parentId` 嵌套模型上扩展 step 节点，不引入新图模型

---

## 9. 验收总表（与 checklist.md 对应）

| 编号 | 验收项 | Phase |
|------|--------|-------|
| V1 | 首次启动创建 `~/.reasonix/memory/` 四个默认文件 | P8 |
| V2 | Agent 系统提示包含 PKM 四文件内容 | P8 |
| V3 | MemoryPanel 可编辑 PKM，保存后下轮生效 | P8 |
| V4 | Gmail OAuth2 收发邮件成功（无应用专用密码） | P8 |
| V5 | token 过期自动刷新 | P8 |
| V6 | ProgressStepper 点击步骤跳转消息 | P8 |
| V7 | AgentCanvas 渲染 step 节点 | P8 |
| V8 | 历史会话恢复 ProgressStepper 状态 | P8 |
| V9 | 助手模式侧边栏有「每日简报」入口 | P8 |
| V10 | DailyBriefPanel 显示待办/邮件/任务/会话 | P8 |
| V11 | 模式切换联动 Composer 占位符 | P9 |
| V12 | 模式切换联动 SlashMenu 推荐 | P9 |
| V13 | 模式切换联动右面板默认 Tab | P9 |
| V14 | RichToolCard「预览」按钮打开右面板 | P9 |
| V15 | 定时任务触发自动新建 Tab | P9 |
| V16 | 任务完成弹出系统通知 | P9 |
| V17 | OnboardingFlow 完成首任务反馈 | P9 |
| V18 | PKM 自动学习弹 ApprovalModal | P10 |
| V19 | 拒绝学习后本轮不再打扰 | P10 |
| V20 | 保存会话为 Recipe | P10 |
| V21 | 从 Recipe 创建定时任务 | P10 |
| V22 | 事件触发 Recipe | P10 |
| V23 | 剪贴板历史记录与搜索 | P10 |
| V24 | 剪贴板历史自动清理 | P10 |
| V25 | 语音输入热键唤起 | P10 |
| V26 | whisper 不可用时降级提示 | P10 |
| V27 | spawn_agent 并行执行两个子 Agent | P11 |
| V28 | wait_agent 等待并返回结构化结果 | P11 |
| V29 | AgentCanvas 渲染子图 | P11 |
| V30 | generate-tests Skill 生成 table-driven 测试 | P11 |
| V31 | review-pr Skill 输出审查报告 | P11 |
| V32 | 编码模式零回归（全部现有测试通过） | 全局 |
| V33 | 禁用所有新功能后行为与改造前一致 | 全局 |

---

## 10. 后续演进（不在 v3.0 内）

- **P12 生态与社区**：Skill 市场、插件市场、模板库扩展、主题市场、跨设备端到端加密同步
- **多 Agent 协作深化**：研究 Agent + 写作 Agent + 审核 Agent 分工
- **桌面宠物/趣味化**：主题系统增强、轻量化个性化
- **移动端伴侣**：基于 IM 集成的轻量手机 App

---

**文档版本**：v3.0（Draft）
**最后更新**：2026-07-02
**前置依赖**：v0.1 / v1.0 / v2.0 已完成
**预计周期**：P8（2-3 周） + P9（3-4 周） + P10（4-6 周） + P11（3-4 周）

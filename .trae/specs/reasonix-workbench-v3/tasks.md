# Tasks — Rexion Workbench v3.0

> 配套 [spec.md](./spec.md) 的可执行任务清单。
> 标注规则：`[ ]` 待办 · `[~]` 进行中 · `[x]` 已完成 · `[!]` 阻塞
>
> 每个 SubTask 后括号标注预估工作量：`XS`(<2h) / `S`(半天) / `M`(1-2天) / `L`(3-5天)。

---

## Phase 8 · 激活与修补（Activate & Fix）

> 目标：把已搭好但未接线的骨架激活。投入小、收益大。
> 前置依赖：无。

### P8-T1: 个人知识库激活（PKM Activation）

- [ ] **Task 1.1**: 启动时调用 `EnsureMemoryDir()` (M)
  - [ ] SubTask 1.1.1: 在 `internal/boot/boot.go` 的 `Boot()` 流程中，于 `memory.Load()` 之前调用 `memory.EnsureMemoryDir()` (XS)
  - [ ] SubTask 1.1.2: 在 `desktop/app.go` 的 `startup()` 中同步调用（Wails 启动路径独立于 CLI） (XS)
  - [ ] SubTask 1.1.3: 单元测试：首次启动时验证 `~/.Rexion/memory/` 目录和四个默认文件被创建 (S)

- [ ] **Task 1.2**: `memory.Set.Load()` 注入 PKM 四文件 (M)
  - [ ] SubTask 1.2.1: 在 `internal/memory/memory.go` 的 `Load()` 中，在组装 `Docs` 之后追加读取 PKM 目录下的四个文件 (S)
  - [ ] SubTask 1.2.2: 文件缺失时跳过（不报错，保持向后兼容） (XS)
  - [ ] SubTask 1.2.3: 注入顺序固定：`writing_style.md` → `preferences.md` → `people.md` → `projects.md` → 原 `Docs`（保证 byte-stable） (XS)
  - [ ] SubTask 1.2.4: 单元测试：`Load()` 返回的 `Set.Docs` 包含 PKM 文件内容 (S)

- [ ] **Task 1.3**: `memory.Set.Block()` 渲染 PKM 分区 (S)
  - [ ] SubTask 1.3.1: 在 `memory.go:127-148` 的 `Block()` 中，PKM 文件单独成一个 section（标题「个人知识库」） (XS)
  - [ ] SubTask 1.3.2: 在系统提示中以 `<personal-knowledge>` 标签包裹 PKM 内容，便于 LLM 识别 (XS)

- [ ] **Task 1.4**: MemoryPanel 前端支持 PKM 编辑 (M)
  - [ ] SubTask 1.4.1: 在 `desktop/frontend/src/components/MemoryPanel.tsx` 中新增「个人知识库」分区（与项目级记忆并列） (S)
  - [ ] SubTask 1.4.2: 实现四个文件的 Tab 切换 + 编辑器（复用现有 Markdown 编辑器） (S)
  - [ ] SubTask 1.4.3: 新增 Wails bound method `SavePKMFile(name, content string) error` 在 `desktop/app.go` (S)
  - [ ] SubTask 1.4.4: 保存后触发 Controller 重建（确保下一轮对话生效） (XS)

- [ ] **Task 1.5**: 配置开关 `[pkm] enabled` (S)
  - [ ] SubTask 1.5.1: 在 `internal/config/config.go` 新增 `PKMConfig` 结构 (XS)
  - [ ] SubTask 1.5.2: `memory.Load()` 在 `enabled=false` 时跳过 PKM 注入 (XS)
  - [ ] SubTask 1.5.3: 更新 `.env.example` / `Rexion.example.toml` (XS)

---

### P8-T2: 邮件 OAuth2 闭环

- [ ] **Task 2.1**: IMAP XOAUTH2 认证路径 (M)
  - [ ] SubTask 2.1.1: 在 `cmd/Rexion-plugin-mail/imap.go` 的 `imapConfig()` 中，优先尝试 OAuth2 token（若已持久化） (S)
  - [ ] SubTask 2.1.2: 在 `read_mail` 实现中，用 `OAuth2IMAPAuthString()` 替代 `PLAIN` 认证（当 token 可用时） (S)
  - [ ] SubTask 2.1.3: token 过期时调用 `RefreshToken()`，刷新后持久化并重试登录 (S)
  - [ ] SubTask 2.1.4: 无 OAuth2 配置且无 `MAIL_IMAP_PASS` 时返回明确错误 (XS)

- [ ] **Task 2.2**: SMTP XOAUTH2 认证路径 (M)
  - [ ] SubTask 2.2.1: 在 `cmd/Rexion-plugin-mail/smtp.go` 的 `smtpConfig()` 中，优先尝试 OAuth2 token (S)
  - [ ] SubTask 2.2.2: 在 `send_mail` 实现中，用 `OAuth2SMTPAuthString()` 替代 `PlainAuth`（当 token 可用时） (S)
  - [ ] SubTask 2.2.3: token 过期时刷新并重试 (S)

- [ ] **Task 2.3**: 集成测试 (M)
  - [ ] SubTask 2.3.1: 在 `cmd/Rexion-plugin-mail/main_test.go` 新增 OAuth2 路径测试（用 mock IMAP/SMTP 服务器） (M)
  - [ ] SubTask 2.3.2: 手动验收脚本：Gmail OAuth2 授权 → read_mail → send_mail 全流程 (S)

- [ ] **Task 2.4**: 文档更新 (S)
  - [ ] SubTask 2.4.1: 更新 `docs/office-capabilities-guide.md` 中邮件插件章节，说明 OAuth2 用法 (XS)
  - [ ] SubTask 2.4.2: 在 `cmd/Rexion-plugin-mail/README.md`（若不存在则创建）补充 OAuth2 配置步骤 (XS)

---

### P8-T3: ProgressStepper 与 AgentCanvas 联动

- [ ] **Task 3.1**: ProgressStepper `onStepClick` 实现 (S)
  - [ ] SubTask 3.1.1: 在 `desktop/frontend/src/App.tsx` 中，把 ProgressStepper 的 `onStepClick` 接到 Transcript 的 `scrollToTurn` 方法 (XS)
  - [ ] SubTask 3.1.2: 在 `desktop/frontend/src/components/Transcript.tsx` 中暴露 `scrollToTurn(turnIndex)`（通过 ref 或 imperative handle） (S)
  - [ ] SubTask 3.1.3: 跳转后给目标消息添加 `--highlight` class，CSS 动画闪烁 1 次 (XS)

- [ ] **Task 3.2**: AgentCanvas 渲染 step 节点 (M)
  - [ ] SubTask 3.2.1: 在 `desktop/frontend/src/lib/agentGraph.ts` 中扩展节点类型，新增 `step` 类型 (XS)
  - [ ] SubTask 3.2.2: 处理 `StepProgress` wire 事件，把每个 `event.Step` 转为图节点（parentId 指向当前任务节点） (S)
  - [ ] SubTask 3.2.3: step 节点图标：pending=○, in_progress=⏳, completed=✅ (XS)
  - [ ] SubTask 3.2.4: step 节点与后续 ToolDispatch 节点用边连接（通过 parentId 关联） (S)
  - [ ] SubTask 3.2.5: step 状态变更时更新节点图标（不新增节点，按 ID 更新） (S)

- [ ] **Task 3.3**: 跨会话恢复 ProgressStepper 状态 (M)
  - [ ] SubTask 3.3.1: 在 `desktop/frontend/src/lib/useController.ts` 中，会话恢复时从历史事件流过滤 `StepProgress` 事件 (S)
  - [ ] SubTask 3.3.2: 把过滤出的事件还原为 `Step[]` 数组传给 ProgressStepper (S)
  - [ ] SubTask 3.3.3: 历史会话所有步骤显示为 `completed` (XS)

---

### P8-T4: DailyBrief 面板（替代信息流）

- [ ] **Task 4.1**: 新增 DailyBriefPanel 组件 (L)
  - [ ] SubTask 4.1.1: 创建 `desktop/frontend/src/components/DailyBriefPanel.tsx` (M)
  - [ ] SubTask 4.1.2: 实现今日待办分区：通过 MCP 调用 `mcp__calendar__list_todo` 获取 `status=pending` 的待办 (S)
  - [ ] SubTask 4.1.3: 实现邮件摘要分区：通过 MCP 调用 `mcp__mail__search_mail` 获取今日未读 Top 5 (S)
  - [ ] SubTask 4.1.4: 实现定时任务执行结果分区：调用 `app.ListScheduledTasks()` + 各任务的 `lastRun` 状态 (S)
  - [ ] SubTask 4.1.5: 实现最近会话分区：调用 `app.ListSessions()` 取最近 3 条 (S)
  - [ ] SubTask 4.1.6: 各分区支持刷新按钮 (XS)

- [ ] **Task 4.2**: 待办一键完成 (S)
  - [ ] SubTask 4.2.1: 每条待办右侧「完成」按钮，点击调用 `mcp__calendar__update_todo` (XS)
  - [ ] SubTask 4.2.2: 完成后从列表移除，显示 toast 提示 (XS)

- [ ] **Task 4.3**: Sidebar 接入「每日简报」入口 (S)
  - [ ] SubTask 4.3.1: 在 `desktop/frontend/src/components/Sidebar.tsx` 助手模式分区新增「每日简报」导航项（icon: Newspaper） (XS)
  - [ ] SubTask 4.3.2: 在 `desktop/frontend/src/lib/i18n.tsx` 新增翻译 key `sidebar.dailyBrief` (XS)
  - [ ] SubTask 4.3.3: 点击后在右面板打开 DailyBriefPanel（注册到 `RightDockMode` 或新 dock 类型） (S)

- [ ] **Task 4.4**: 助手模式默认 Tab (S)
  - [ ] SubTask 4.4.1: 在 `App.tsx` 切换到 assistant 模式时，右面板默认切换到 `dailyBrief` Tab (XS)

---

### P8 验收
见 [checklist.md](./checklist.md) 的 P8 部分。

---

## Phase 9 · 体验闭环（Experience Loop）

> 目标：把跨模块联动打通，让用户感受到「离不开」。
> 前置依赖：P8 完成。

### P9-T1: 模式深度联动

- [ ] **Task 1.1**: Composer 占位符随模式 (S)
  - [ ] SubTask 1.1.1: 在 `desktop/frontend/src/components/Composer.tsx` 中，placeholder 根据 `workspaceType` 切换 (XS)
  - [ ] SubTask 1.1.2: 新增 i18n key：`composer.placeholder.coding` / `.office` / `.assistant` (XS)
  - [ ] SubTask 1.1.3: 编码模式：「描述你的编码任务，或用 / 触发 Skill…」(XS)
  - [ ] SubTask 1.1.4: 办公模式：「描述你要生成的文档或表格…」 (XS)
  - [ ] SubTask 1.1.5: 助手模式：「让我帮你查、提醒、整理…」 (XS)

- [ ] **Task 1.2**: SlashMenu Skill 推荐随模式 (M)
  - [ ] SubTask 1.2.1: 在 `desktop/frontend/src/components/SlashMenu.tsx` 中，根据 `workspaceType` 排序 Skill (S)
  - [ ] SubTask 1.2.2: 编码模式置顶：`explore` / `review` / `generate-tests` / `review-pr` (XS)
  - [ ] SubTask 1.2.3: 办公模式置顶：`weekly-report` / `sheet-analysis` / `meeting-minutes` / `contract-draft` (XS)
  - [ ] SubTask 1.2.4: 助手模式置顶：`daily-brief` / `research-report` / `generate-ppt` / `sheet-analysis` (XS)
  - [ ] SubTask 1.2.5: 置顶 Skill 标记「推荐」徽章 (XS)

- [ ] **Task 1.3**: 右面板默认 Tab 随模式 (S)
  - [ ] SubTask 1.3.1: 编码模式默认 `files` Tab (XS)
  - [ ] SubTask 1.3.2: 办公模式默认 `preview` Tab (XS)
  - [ ] SubTask 1.3.3: 助手模式默认 `dailyBrief` Tab（P8 完成后） (XS)

- [ ] **Task 1.4**: HomePanel 快捷操作已支持（验证） (XS)
  - [ ] SubTask 1.4.1: 验证 `HomePanel.tsx` 的 `QUICK_ACTIONS` 已按模式切换（代码审查确认） (XS)
  - [ ] SubTask 1.4.2: 修复 P8-T4 新增 `daily-brief` 后 assistant 模式的快捷操作引用 (XS)

---

### P9-T2: 富媒体卡片与右面板预览协同

- [ ] **Task 2.1**: RichToolCard「预览」按钮 (M)
  - [ ] SubTask 2.1.1: 在 `desktop/frontend/src/components/RichToolCard.tsx` 的文档/表格/PPT/图表卡片中新增「预览」按钮 (S)
  - [ ] SubTask 2.1.2: 从工具返回结果中提取文件路径（约定 schema：`{ "path": "/abs/...", "kind": "docx|xlsx|pptx|png" }`） (S)
  - [ ] SubTask 2.1.3: 点击后调用 `onPreview(path, kind)` 回调 (XS)

- [ ] **Task 2.2**: App.tsx 接预览回调 (M)
  - [ ] SubTask 2.2.1: 在 `App.tsx` 中接收 `onPreview`，根据 `kind` 切换右面板到 `preview` Tab 并加载对应 Viewer (S)
  - [ ] SubTask 2.2.2: docx → DocPreviewer，xlsx → SheetViewer，pptx → SlideViewer，png → 内联图片 (S)
  - [ ] SubTask 2.2.3: 路径不存在时显示错误提示 (XS)

---

### P9-T3: Scheduler 触发的 Skill 在新 Tab 自动打开

- [ ] **Task 3.1**: 后端 bound method (M)
  - [ ] SubTask 3.1.1: 在 `desktop/app.go` 新增 `OpenTabForScheduledTask(taskID string) error` (S)
  - [ ] SubTask 3.1.2: 该方法创建新 Tab、设置标题为「定时任务：{skill} {YYYY-MM-DD}」、切换到前台 (S)
  - [ ] SubTask 3.1.3: 在 `internal/scheduler/scheduler.go` 的 `executeTask` 中通过事件通知前端（或直接调用 bound method） (S)

- [ ] **Task 3.2**: 前端事件处理 (M)
  - [ ] SubTask 3.2.1: 在 `desktop/frontend/src/lib/useController.ts` 中监听 `scheduled_task_started` 事件 (S)
  - [ ] SubTask 3.2.2: 收到事件后调用 `app.OpenTabForScheduledTask(taskID)` (XS)
  - [ ] SubTask 3.2.3: 如果窗口最小化，先 `app.Show()` 再打开 Tab (XS)

- [ ] **Task 3.3**: 系统通知 (S)
  - [ ] SubTask 3.3.1: 任务启动时调用 `internal/notify` 发送系统通知「定时任务已启动：{skill}」 (XS)
  - [ ] SubTask 3.3.2: 任务完成时发送通知「{skill} 已完成，点击查看」 (XS)
  - [ ] SubTask 3.3.3: 通知点击跳转到对应 Tab（通过 `app.FocusTab(tabID)`） (S)

---

### P9-T4: 跨会话任务恢复（与 P8-T3 协同）

- [ ] **Task 4.1**: 历史会话恢复 ProgressStepper (M)
  - [ ] SubTask 4.1.1: 与 P8-T3.3 共用实现 (XS)
  - [ ] SubTask 4.1.2: 验证打开历史会话后步骤列表正确显示 (XS)

- [ ] **Task 4.2**: AgentCanvas 历史会话渲染 (M)
  - [ ] SubTask 4.2.1: 历史会话恢复时，从事件流重建整个图（包括 step + ToolDispatch + Result 节点） (S)
  - [ ] SubTask 4.2.2: 节点状态全部为已完成 (XS)

---

### P9-T5: 新手引导反馈回路

- [ ] **Task 5.1**: OnboardingFlow 完成首任务跟踪 (M)
  - [ ] SubTask 5.1.1: 在 `desktop/frontend/src/components/OnboardingOverlay.tsx` 中，身份选择后记录 `onboarding_task_pending=true` 到 localStorage (S)
  - [ ] SubTask 5.1.2: 引导用户发送第一条消息后，监听该会话的第一个 `TurnComplete` 事件 (S)
  - [ ] SubTask 5.1.3: 完成后弹出庆祝浮层「🎉 你完成了首个任务！」+ 清除 pending 标记 (S)
  - [ ] SubTask 5.1.4: 提供「查看更多 Skill」按钮跳转到 TemplateLibrary (XS)

- [ ] **Task 5.2**: HomePanel 显示引导进度 (S)
  - [ ] SubTask 5.2.1: 在 HomePanel 顶部，若 `onboarding_task_pending=true` 显示进度条「完成首个任务解锁全部功能」 (S)
  - [ ] SubTask 5.2.2: 完成后进度条消失，显示「新手任务已完成」徽章 (XS)

---

### P9 验收
见 [checklist.md](./checklist.md) 的 P9 部分。

---

## Phase 10 · 个人助手深化（Personal Assistant Deepen）

> 目标：让助手真的「懂你」，从被动响应到主动学习。
> 前置依赖：P8 完成（PKM 激活）。

### P10-T1: PKM 自动学习

- [ ] **Task 1.1**: 偏好识别器 (L)
  - [ ] SubTask 1.1.1: 新增 `internal/memory/autolearn.go`，定义 `AutoLearner` 结构 (S)
  - [ ] SubTask 1.1.2: 实现规则引擎识别 5 类声明：写作偏好 / 联系人 / 项目 / 身份 / 工具偏好 (M)
    - 写作偏好关键词：「我喜欢/我偏好/我的风格是…」
    - 联系人关键词：「XX 是我…/XX 负责…」
    - 项目关键词：「我在做…项目/我负责…」
    - 身份关键词：「我是开发者/我是产品经理…」
    - 工具偏好：「我喜欢用 VSCode/我用 Vim」
  - [ ] SubTask 1.1.3: 识别结果分类映射到 PKM 文件：偏好→`preferences.md`，联系人→`people.md`，项目→`projects.md`，写作→`writing_style.md` (S)

- [ ] **Task 1.2**: ApprovalModal 集成 (M)
  - [ ] SubTask 1.2.1: 在 `internal/control/controller.go` 的 `runTurnWithRawDisplay` 结束后调用 `autoLearner.Scan(lastUserMessage)` (S)
  - [ ] SubTask 1.2.2: 命中时 emit `event.AutoLearnProposal` 事件，含建议写入的文件名 + 内容片段 (S)
  - [ ] SubTask 1.2.3: 前端 ApprovalModal 接收事件，展示「检测到{类型}，是否记录到 {file}？」+ 内容预览 (S)
  - [ ] SubTask 1.2.4: 用户确认后调用 `app.AppendPKMFile(file, content)` 追加到对应文件段落 (S)

- [ ] **Task 1.3**: 防打扰机制 (S)
  - [ ] SubTask 1.3.1: 用户拒绝后，本轮对话内相同类型声明不再弹出（在 Controller 中维护 `suppressedTypes` set） (S)
  - [ ] SubTask 1.3.2: 每轮对话最多弹出 1 次询问（避免连续打扰） (XS)

- [ ] **Task 1.4**: 配置开关 (S)
  - [ ] SubTask 1.4.1: `[pkm] auto_learn` 控制是否启用自动学习 (XS)
  - [ ] SubTask 1.4.2: `[pkm] auto_learn_confirm` 控制是否需 ApprovalModal 确认（false 则直接写入） (XS)

---

### P10-T2: 自动化 Recipe（工作流配方）

- [ ] **Task 2.1**: Recipe 存储层 (M)
  - [ ] SubTask 2.1.1: 新增 `internal/recipe/recipe.go`，定义 `Recipe` 结构：`Name`/`Description`/`Skill`/`Params`/`Trigger`/`CreatedAt` (S)
  - [ ] SubTask 2.1.2: 实现 `Save()`/`Load()`/`List()`/`Delete()`，存储到 `~/.Rexion/recipes/<name>.json` (S)
  - [ ] SubTask 2.1.3: 单元测试 (S)

- [ ] **Task 2.2**: 保存会话为 Recipe UI (M)
  - [ ] SubTask 2.2.1: 在 `desktop/frontend/src/components/StatusBar.tsx` 或 Tab 右键菜单新增「保存为 Recipe」入口 (S)
  - [ ] SubTask 2.2.2: 新增 `SaveRecipeModal.tsx` 表单：名称、描述、Skill 名（从会话首个 `/skill` 命令提取）、参数模板（从首条 user message 提取） (M)
  - [ ] SubTask 2.2.3: 触发条件选择：手动 / 定时 / 事件（邮件/文件变更） (S)
  - [ ] SubTask 2.2.4: 保存调用 `app.SaveRecipe(recipe)` (XS)

- [ ] **Task 2.3**: SchedulerPanel Recipe 选择 (S)
  - [ ] SubTask 2.3.1: 在 `SchedulerPanel.tsx` 新建任务表单中新增「从 Recipe 选择」下拉 (S)
  - [ ] SubTask 2.3.2: 选择后自动填充 Skill 名和参数模板字段 (XS)

- [ ] **Task 2.4**: 事件触发 Recipe (L)
  - [ ] SubTask 2.4.1: Recipe 的 `Trigger` 支持 `mail_received` 事件类型，含匹配规则（发件人/主题/正文关键词） (S)
  - [ ] SubTask 2.4.2: 在 `Rexion-plugin-mail` 的 `read_mail` 工具返回后，触发事件检查（通过 hook 机制） (M)
  - [ ] SubTask 2.4.3: 命中时 `internal/recipe` 调度执行：创建新会话、发送 `/{skill} {params}` 消息 (M)
  - [ ] SubTask 2.4.4: 弹出系统通知「事件触发 Recipe：{name}」 (XS)

---

### P10-T3: 剪贴板历史

- [ ] **Task 3.1**: 后端 SQLite 存储 (M)
  - [ ] SubTask 3.1.1: 新增 `desktop/clipboard_history.go`，定义 `ClipboardEntry` 结构：`ID`/`Kind`/`Content`/`Preview`/`CreatedAt` (S)
  - [ ] SubTask 3.1.2: 实现 `RecordClipboard(kind, content)` / `ListClipboard(limit, offset)` / `SearchClipboard(query)` / `ClearOldClipboard(days)` (M)
  - [ ] SubTask 3.1.3: 持久化到 `~/.Rexion/clipboard_history.db`（SQLite） (S)
  - [ ] SubTask 3.1.4: 启动时自动清理超过 `retention_days` 的条目 (XS)

- [ ] **Task 3.2**: 剪贴板监听 (M)
  - [ ] SubTask 3.2.1: 在 `desktop/app.go` 启动一个 goroutine，定时（500ms）轮询系统剪贴板 (S)
  - [ ] SubTask 3.2.2: 内容变化时调用 `RecordClipboard` (XS)
  - [ ] SubTask 3.2.3: 支持三种 kind：text / image（保存为文件路径）/ file（保存路径列表） (S)
  - [ ] SubTask 3.2.4: 配置开关 `[clipboard_history] enabled` (XS)

- [ ] **Task 3.3**: FloatingWindow 剪贴板历史分区 (M)
  - [ ] SubTask 3.3.1: 在 `desktop/frontend/src/components/FloatingWindow.tsx` 新增「剪贴板历史」Tab（与「当前剪贴板」并列） (S)
  - [ ] SubTask 3.3.2: 列表展示：时间 + 类型图标 + 内容预览（前 80 字符） (S)
  - [ ] SubTask 3.3.3: 顶部搜索框，输入时调用 `app.SearchClipboard(query)` 实时过滤 (S)
  - [ ] SubTask 3.3.4: 点击条目调用 `app.WriteClipboard(content)` 重新复制 + toast 提示 (XS)

- [ ] **Task 3.4**: 自动清理与上限 (S)
  - [ ] SubTask 3.4.1: 超过 `max_entries` 时删除最旧条目 (XS)
  - [ ] SubTask 3.4.2: 配置项 `[clipboard_history] retention_days` / `max_entries` (XS)

---

### P10-T4: 语音输入

- [ ] **Task 4.1**: whisper 探测与配置 (S)
  - [ ] SubTask 4.1.1: 新增 `desktop/voice_input.go`，启动时探测 `whisper` / `whisper-cpp` 二进制（配置路径 → $PATH） (S)
  - [ ] SubTask 4.1.2: 探测结果存入 `app.voiceAvailable` (XS)
  - [ ] SubTask 4.1.3: 配置 `[voice_input] enabled` / `whisper_path` / `hotkey` (XS)

- [ ] **Task 4.2**: 全局热键注册 (M)
  - [ ] SubTask 4.2.1: 在 `desktop/tray.go` 或新增 `desktop/hotkey.go` 中注册全局热键（默认 `Ctrl+Shift+V`） (S)
  - [ ] SubTask 4.2.2: 热键触发时 emit `voice_input_started` 事件给前端 (XS)
  - [ ] SubTask 4.2.3: Windows 用 `golang.design/x/hotkey`，macOS 用相同库（纯 Go） (S)

- [ ] **Task 4.3**: 录音浮层 UI (M)
  - [ ] SubTask 4.3.1: 在 `desktop/frontend/src/components/Composer.tsx` 旁新增录音浮层（红色圆点 + 波形动画） (S)
  - [ ] SubTask 4.3.2: 监听 `voice_input_started` 显示浮层，开始录音（前端 `MediaRecorder` API） (S)
  - [ ] SubTask 4.3.3: 用户再次按键或停止说话 2 秒后结束录音 (XS)
  - [ ] SubTask 4.3.4: 录音数据传给后端 `app.TranscribeAudio(wavBytes []byte) (string, error)` (S)

- [ ] **Task 4.4**: whisper 调用 (M)
  - [ ] SubTask 4.4.1: 在 `desktop/voice_input.go` 实现 `TranscribeAudio`：保存 wav 到临时文件 → 调用 `whisper-cli` → 返回文本 (S)
  - [ ] SubTask 4.4.2: 调用失败时返回明确错误 (XS)
  - [ ] SubTask 4.4.3: 转写结果通过事件传给前端，填入 Composer (XS)

- [ ] **Task 4.5**: 降级处理 (S)
  - [ ] SubTask 4.5.1: `voiceAvailable=false` 时热键按下弹出 toast「语音输入需要 whisper，点击查看安装指引」 (S)
  - [ ] SubTask 4.5.2: 安装指引链接到 `docs/voice-input-setup.md`（新建） (XS)

---

### P10 验收
见 [checklist.md](./checklist.md) 的 P10 部分。

---

## Phase 11 · 编码能力再增强（Coding Enhance）

> 目标：守住编码核心能力不停滞，落地 P1-2 多 Agent 并行，新增测试/审查 Skill。
> 前置依赖：无（独立于 P8-P10）。

### P11-T1: 多 Agent 并行编排（落地 P1-2）

- [ ] **Task 1.1**: 角色与 Pool 定义 (L)
  - [ ] SubTask 1.1.1: 新增 `internal/agent/role.go`，定义 `Role` 结构 + 内置角色 `default`/`worker`/`explorer`/`monitor` (M)
  - [ ] SubTask 1.1.2: 新增 `internal/agent/pool.go`，定义 `Pool` 结构 + `Spawn`/`Wait`/`SendInput`/`Close` 方法 (M)
  - [ ] SubTask 1.1.3: 配置 `max_threads=6` / `max_depth=1` / `job_timeout=5m` (S)
  - [ ] SubTask 1.1.4: 单元测试：Pool 并发安全 (S)

- [ ] **Task 1.2**: spawn_agent / wait_agent / send_input / close_agent 工具 (L)
  - [ ] SubTask 1.2.1: 新增 `internal/agent/spawn.go`，实现四个工具（替代现有单次 API 调用的 TaskTool） (M)
  - [ ] SubTask 1.2.2: 子 Agent 拥有独立 Session、独立工具集（按 Role 过滤） (M)
  - [ ] SubTask 1.2.3: 子 Agent 继承父 Agent 的沙盒约束 (S)
  - [ ] SubTask 1.2.4: `wait_agent` 阻塞直到子 Agent 完成，返回 `AgentResult`（summary/tool_calls/files_write/duration/usage） (S)

- [ ] **Task 1.3**: 事件流扩展 (M)
  - [ ] SubTask 1.3.1: 在 `internal/event/event.go` 新增 `AgentSpawned` / `AgentProgress` / `AgentCompleted` / `AgentClosed` 事件 (S)
  - [ ] SubTask 1.3.2: 在 `internal/serve/wire.go` 新增对应 wire 事件 (S)
  - [ ] SubTask 1.3.3: 子 Agent 事件嵌套在父 Agent 事件下（通过 `parentAgentID` 字段） (S)

- [ ] **Task 1.4**: AgentCanvas 子图渲染 (M)
  - [ ] SubTask 1.4.1: 在 `lib/agentGraph.ts` 中处理 `AgentSpawned` 事件，渲染子图容器节点 (S)
  - [ ] SubTask 1.4.2: 子图内显示该子 Agent 的所有 ToolDispatch/Result 节点 (S)
  - [ ] SubTask 1.4.3: `AgentCompleted` 时子图节点边框变绿 (XS)
  - [ ] SubTask 1.4.4: 子图可折叠（点击收起/展开） (S)

- [ ] **Task 1.5**: 配置与文档 (S)
  - [ ] SubTask 1.5.1: 更新 `.env.example` / `Rexion.example.toml` 的 `[agents]` 节 (XS)
  - [ ] SubTask 1.5.2: 更新 `docs/SPEC.md` 中 Agent 章节 (S)

---

### P11-T2: generate-tests Skill

- [ ] **Task 2.1**: Skill 定义 (M)
  - [ ] SubTask 2.1.1: 新增 `.Rexion/skills/generate-tests.md` (S)
  - [ ] SubTask 2.1.2: 声明 `runAs: subagent` + `allowed-tools: [read_file, grep, glob, write_file, edit_file]` (XS)
  - [ ] SubTask 2.1.3: prompt 要求：读取目标函数 → 分析签名 → 生成 table-driven 测试 → 写入 `_test.go` (S)

- [ ] **Task 2.2**: 测试生成质量约束 (S)
  - [ ] SubTask 2.2.1: 必须覆盖正常/边界/错误三类用例 (XS)
  - [ ] SubTask 2.2.2: 测试函数命名 `TestXxx_CaseName` (XS)
  - [ ] SubTask 2.2.3: 生成后调用 `bash: go test ./...` 验证能通过（可选） (S)

- [ ] **Task 2.3**: 注册到 SlashMenu (XS)
  - [ ] SubTask 2.3.1: 编码模式 SlashMenu 置顶 `generate-tests` (XS)

---

### P11-T3: review-pr Skill

- [ ] **Task 3.1**: Skill 定义 (M)
  - [ ] SubTask 3.1.1: 新增 `.Rexion/skills/review-pr.md` (S)
  - [ ] SubTask 3.1.2: 声明 `runAs: subagent` + `allowed-tools: [bash, read_file, grep]` (XS)
  - [ ] SubTask 3.1.3: prompt 要求：`git diff {base}...HEAD` → 逐文件分析 → 输出审查报告（问题/建议/风险） (S)

- [ ] **Task 3.2**: 审查报告格式 (S)
  - [ ] SubTask 3.2.1: Markdown 格式：文件名 + 行号 + 严重级别（🔴问题/🟡建议/🔵风险）+ 描述 (S)
  - [ ] SubTask 3.2.2: 末尾汇总：问题数 / 建议数 / 总体评价 (XS)

- [ ] **Task 3.3**: 集成测试 (S)
  - [ ] SubTask 3.3.1: 在本仓库制造一个测试 PR，运行 Skill 验证输出 (S)

---

### P11 验收
见 [checklist.md](./checklist.md) 的 P11 部分。

---

## 全局任务（贯穿所有 Phase）

### G-T1: 回归测试

- [ ] **Task G1.1**: 每个 Phase 完成后运行全量测试 (S)
  - [ ] SubTask G1.1.1: `go test ./...` 通过 (XS)
  - [ ] SubTask G1.1.2: `cd desktop/frontend && pnpm test` 通过（如有） (XS)
  - [ ] SubTask G1.1.3: 编码模式端到端测试通过（read/edit/bash/grep 等核心工具） (S)

### G-T2: 配置兼容性

- [ ] **Task G2.1**: 禁用新功能后行为与改造前一致 (S)
  - [ ] SubTask G2.1.1: `[pkm] enabled=false` → memory.Load 不注入 PKM (XS)
  - [ ] SubTask G2.1.2: `[clipboard_history] enabled=false` → 不监听剪贴板 (XS)
  - [ ] SubTask G2.1.3: `[voice_input] enabled=false` → 不注册热键 (XS)
  - [ ] SubTask G2.1.4: `[agents]` 节缺失 → spawn_agent 工具不注册（保持向后兼容） (XS)

### G-T3: 文档同步

- [ ] **Task G3.1**: 每个 Phase 完成后更新文档 (M)
  - [ ] SubTask G3.1.1: 更新 `docs/SPEC.md` 对应章节 (S)
  - [ ] SubTask G3.1.2: 更新 `docs/office-capabilities-guide.md`（涉及插件时） (S)
  - [ ] SubTask G3.1.3: 更新 `Rexion.md`（涉及项目约定变更时） (S)
  - [ ] SubTask G3.1.4: 更新 `.env.example` / `Rexion.example.toml` (XS)

---

## 依赖关系图

```
P8-T1 (PKM 激活) ─┬─→ P10-T1 (PKM 自动学习)
                   └─→ P10-T2 (Recipe，部分依赖)

P8-T3 (ProgressStepper 联动) ─→ P9-T4 (跨会话恢复)

P8-T4 (DailyBrief) ─→ P9-T1 (模式联动，助手模式默认 Tab)

P9-T3 (Scheduler→新 Tab) — 独立

P10-T3 (剪贴板历史) — 独立
P10-T4 (语音输入) — 独立

P11-T1 (多 Agent) — 独立
P11-T2 (generate-tests) — 独立
P11-T3 (review-pr) — 独立
```

**最小阻力路径**：
1. P8-T1 → P8-T2 → P8-T3 → P8-T4（全部激活）
2. P11-T2 / P11-T3（小 Skill，可并行）
3. P9-T1 → P9-T2 → P9-T3（体验闭环）
4. P10-T1 → P10-T2（深化助手）
5. P10-T3 / P10-T4（独立增强）
6. P11-T1（多 Agent，最大块）

---

**文档版本**：v3.0
**最后更新**：2026-07-02

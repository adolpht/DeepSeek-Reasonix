# Checklist — Reasonix Workbench v3.0

> 配套 [spec.md](./spec.md) 与 [tasks.md](./tasks.md) 的验收清单。
> 每项验收对应 spec.md §9 验收总表中的编号（V1-V33）。
> 标注规则：`[ ]` 待验收 · `[x]` 通过 · `[!]` 失败 · `[-]` 不适用

---

## Phase 8 · 激活与修补

### P8-T1 个人知识库激活

- [ ] **V1**: 首次启动创建 `~/.reasonix/memory/` 四个默认文件
  - [ ] 删除 `~/.reasonix/memory/` 目录后启动 Reasonix
  - [ ] 启动后目录存在
  - [ ] `people.md` / `projects.md` / `preferences.md` / `writing_style.md` 四个文件均存在
  - [ ] 每个文件包含中文示例模板内容
- [ ] **V2**: Agent 系统提示包含 PKM 四文件内容
  - [ ] 启动 Reasonix 进入任意会话
  - [ ] 通过 `reasonix doctor` 或日志确认系统提示中包含 `<personal-knowledge>` 标签
  - [ ] 标签内含四个文件的内容
  - [ ] 顺序为：writing_style → preferences → people → projects → 原 Docs
- [ ] **V3**: MemoryPanel 可编辑 PKM，保存后下轮生效
  - [ ] 打开 MemoryPanel，看到「个人知识库」分区
  - [ ] 可切换四个文件 Tab
  - [ ] 编辑 `preferences.md` 添加一行「测试偏好」并保存
  - [ ] 发送一条新消息，通过 `reasonix doctor` 确认系统提示已更新
  - [ ] PKM 位置在 prefix 固定位置（byte-stable，编辑前不破坏缓存）
- [ ] P8-T1.5: `[pkm] enabled=false` 时 memory.Load 跳过 PKM 注入
  - [ ] 配置 `[pkm] enabled=false` 后重启
  - [ ] 系统提示不包含 `<personal-knowledge>` 标签
  - [ ] `~/.reasonix/memory/` 目录即使存在也不被读取

---

### P8-T2 邮件 OAuth2 闭环

- [ ] **V4**: Gmail OAuth2 收发邮件成功（无应用专用密码）
  - [ ] 配置 Gmail OAuth2 client_id / client_secret
  - [ ] 调用 `mcp__mail__oauth2_authorize` 完成授权流程
  - [ ] 确认 token 持久化到本地
  - [ ] **不设置** `MAIL_IMAP_PASS` 环境变量
  - [ ] 调用 `mcp__mail__read_mail` 成功返回邮件列表
  - [ ] 调用 `mcp__mail__send_mail` 成功发送一封测试邮件
- [ ] **V5**: token 过期自动刷新
  - [ ] 手动把持久化的 `access_token` 改为无效值（保留有效 `refresh_token`）
  - [ ] 调用 `read_mail`
  - [ ] 插件自动用 refresh_token 换取新 access_token
  - [ ] 持久化新 token
  - [ ] 重试登录成功，返回邮件
- [ ] P8-T2.1.4: 无 OAuth2 配置且无 `MAIL_IMAP_PASS` 时返回明确错误
  - [ ] 清除 OAuth2 token 和 `MAIL_IMAP_PASS`
  - [ ] 调用 `read_mail`
  - [ ] 返回错误信息：「请先配置 OAuth2（调用 oauth2_authorize）或设置 MAIL_IMAP_PASS 环境变量」
- [ ] P8-T2.4: 文档已更新
  - [ ] `docs/office-capabilities-guide.md` 包含 OAuth2 用法章节
  - [ ] `cmd/reasonix-plugin-mail/README.md` 包含 OAuth2 配置步骤

---

### P8-T3 ProgressStepper 与 AgentCanvas 联动

- [ ] **V6**: ProgressStepper 点击步骤跳转消息
  - [ ] 在一个多步骤任务执行中或完成后
  - [ ] 点击 ProgressStepper 中某个 `completed` 状态的步骤
  - [ ] Transcript 滚动到该步骤对应的消息
  - [ ] 该消息高亮闪烁 1 次
- [ ] **V7**: AgentCanvas 渲染 step 节点
  - [ ] 打开 AgentCanvas（Trace 面板）
  - [ ] 执行一个多步骤任务（如 `/skill weekly-report`）
  - [ ] Canvas 中看到 step 节点（与 ToolDispatch 节点并列）
  - [ ] step 节点图标随状态变化：○ → ⏳ → ✅
  - [ ] step 节点与后续 ToolDispatch 节点有边连接
- [ ] **V8**: 历史会话恢复 ProgressStepper 状态
  - [ ] 完成一个多步骤任务
  - [ ] 切换到其他会话
  - [ ] 切回原会话
  - [ ] ProgressStepper 显示该会话的所有步骤，状态全部为 `completed`

---

### P8-T4 DailyBrief 面板

- [ ] **V9**: 助手模式侧边栏有「每日简报」入口
  - [ ] 切换到助手模式
  - [ ] 侧边栏看到「每日简报」导航项（Newspaper 图标）
  - [ ] 鼠标悬停显示 tooltip
- [ ] **V10**: DailyBriefPanel 显示待办/邮件/任务/会话
  - [ ] 点击「每日简报」打开面板
  - [ ] 「今日待办」分区显示 `status=pending` 的待办
  - [ ] 「邮件摘要」分区显示今日未读 Top 5
  - [ ] 「定时任务执行结果」分区显示昨日任务 lastRun 状态
  - [ ] 「最近会话」分区显示最近 3 条会话主题
  - [ ] 每个分区有刷新按钮，点击后重新加载
- [ ] P8-T4.2: 待办一键完成
  - [ ] 点击某条待办的「完成」按钮
  - [ ] 调用 `update_todo` 成功
  - [ ] 列表实时移除该条
  - [ ] 显示 toast「已完成」
- [ ] P8-T4.4: 助手模式默认 Tab
  - [ ] 切换到助手模式
  - [ ] 右面板自动切换到 `dailyBrief` Tab

---

### P8 回归测试

- [ ] P8-REG-1: 编码模式所有现有测试通过
  - [ ] `go test ./...` 全部 PASS
  - [ ] `cd desktop/frontend && pnpm test` 全部 PASS（如有测试）
- [ ] P8-REG-2: 禁用所有 P8 新功能后行为与改造前一致
  - [ ] `[pkm] enabled=false`
  - [ ] 不影响邮件插件原有 `MAIL_IMAP_PASS` 路径
  - [ ] ProgressStepper / AgentCanvas 在没有 StepProgress 事件时退化为原有行为

---

## Phase 9 · 体验闭环

### P9-T1 模式深度联动

- [ ] **V11**: 模式切换联动 Composer 占位符
  - [ ] 切换到编码模式 → 占位符为「描述你的编码任务…」
  - [ ] 切换到办公模式 → 占位符为「描述你要生成的文档或表格…」
  - [ ] 切换到助手模式 → 占位符为「让我帮你查、提醒、整理…」
- [ ] **V12**: 模式切换联动 SlashMenu 推荐
  - [ ] 编码模式输入 `/` → 顶部显示 `explore` / `review` / `generate-tests` / `review-pr`，带「推荐」徽章
  - [ ] 办公模式输入 `/` → 顶部显示 `weekly-report` / `sheet-analysis` / `meeting-minutes` / `contract-draft`
  - [ ] 助手模式输入 `/` → 顶部显示 `daily-brief` / `research-report` / `generate-ppt` / `sheet-analysis`
- [ ] **V13**: 模式切换联动右面板默认 Tab
  - [ ] 编码模式 → 右面板默认 `files` Tab
  - [ ] 办公模式 → 右面板默认 `preview` Tab
  - [ ] 助手模式 → 右面板默认 `dailyBrief` Tab

---

### P9-T2 富媒体卡片与右面板预览协同

- [ ] **V14**: RichToolCard「预览」按钮打开右面板
  - [ ] 触发 Agent 生成一个 docx 文件
  - [ ] 对话中出现文档卡片，含「预览」按钮
  - [ ] 点击「预览」
  - [ ] 右面板切换到 `preview` Tab
  - [ ] DocPreviewer 加载该 docx，显示前 5 页
- [ ] P9-T2.2.2: 不同 kind 卡片对应不同 Viewer
  - [ ] xlsx 卡片 → SheetViewer
  - [ ] pptx 卡片 → SlideViewer
  - [ ] png 卡片 → 内联图片
- [ ] P9-T2.2.3: 路径不存在时显示错误提示
  - [ ] 手动删除生成的文件后点击「预览」
  - [ ] 显示「文件不存在：/path/to/file」

---

### P9-T3 Scheduler 触发的 Skill 在新 Tab 自动打开

- [ ] **V15**: 定时任务触发自动新建 Tab
  - [ ] 在 SchedulerPanel 创建一个 1 分钟后触发的 `weekly-report` 任务
  - [ ] 等待触发时间到达
  - [ ] 桌面端自动新建 Tab，标题为「定时任务：weekly-report 2026-07-02」
  - [ ] 该 Tab 切换到前台
  - [ ] Tab 内显示 ProgressStepper + Transcript 执行过程
- [ ] P9-T3.2.3: 窗口最小化时先 Show 再打开 Tab
  - [ ] 把 Reasonix 窗口最小化到托盘
  - [ ] 等待定时任务触发
  - [ ] 窗口自动还原 + Tab 自动切换
- [ ] **V16**: 任务完成弹出系统通知
  - [ ] 任务执行完成
  - [ ] 系统通知「weekly-report 已完成，点击查看」
  - [ ] 点击通知跳转到该 Tab

---

### P9-T4 跨会话任务恢复

- [ ] P9-T4.1: 历史会话恢复 ProgressStepper（与 V8 共用）
  - [ ] 已在 V8 验证
- [ ] P9-T4.2: AgentCanvas 历史会话渲染
  - [ ] 切换到一个历史会话
  - [ ] 打开 AgentCanvas
  - [ ] 图中包含该会话的所有 step + ToolDispatch + Result 节点
  - [ ] 节点状态全部为已完成

---

### P9-T5 新手引导反馈回路

- [ ] **V17**: OnboardingFlow 完成首任务反馈
  - [ ] 新用户首次启动（清空 localStorage）
  - [ ] 完成 OnboardingFlow 身份选择
  - [ ] HomePanel 顶部显示进度条「完成首个任务解锁全部功能」
  - [ ] 发送第一条消息
  - [ ] 等待第一个 `TurnComplete` 事件
  - [ ] 弹出庆祝浮层「🎉 你完成了首个任务！」
  - [ ] 进度条消失，显示「新手任务已完成」徽章
  - [ ] 点击「查看更多 Skill」跳转到 TemplateLibrary

---

### P9 回归测试

- [ ] P9-REG-1: 编码模式零回归
  - [ ] 所有编码模式现有测试通过
  - [ ] 项目树、代码 Diff、Git 操作、终端、Skill 等行为与改造前一致
- [ ] P9-REG-2: 模式切换不丢失会话状态
  - [ ] 在编码模式执行任务中途切换到办公模式
  - [ ] 切回编码模式
  - [ ] 任务状态、Transcript、ProgressStepper 均保留

---

## Phase 10 · 个人助手深化

### P10-T1 PKM 自动学习

- [ ] **V18**: PKM 自动学习弹 ApprovalModal
  - [ ] 启用 `[pkm] auto_learn=true` + `auto_learn_confirm=true`
  - [ ] 在对话中说「我写报告喜欢用要点式，不要客套话」
  - [ ] Agent 识别为写作偏好
  - [ ] 弹出 ApprovalModal：「检测到写作偏好，是否写入 `preferences.md`？」
  - [ ] 内容预览正确
  - [ ] 点击确认
  - [ ] 验证 `~/.reasonix/memory/preferences.md` 的「写作风格」段落已追加
- [ ] P10-T1.1.2: 识别 5 类声明
  - [ ] 写作偏好：「我喜欢/我偏好…」→ `preferences.md` 或 `writing_style.md`
  - [ ] 联系人：「张三是我领导…」→ `people.md`
  - [ ] 项目：「我在做 XX 项目…」→ `projects.md`
  - [ ] 身份：「我是开发者…」→ `preferences.md`
  - [ ] 工具偏好：「我用 VSCode…」→ `preferences.md`
- [ ] **V19**: 拒绝学习后本轮不再打扰
  - [ ] 在 ApprovalModal 中点击「拒绝」
  - [ ] 本轮对话内再说类似偏好声明
  - [ ] 不再弹出 ApprovalModal
- [ ] P10-T1.3.2: 每轮最多 1 次询问
  - [ ] 一轮对话中包含多个不同类型声明
  - [ ] 只弹出 1 次 ApprovalModal
- [ ] P10-T1.4: 配置开关
  - [ ] `[pkm] auto_learn=false` → 不识别、不弹出
  - [ ] `[pkm] auto_learn_confirm=false` → 直接写入，不弹 ApprovalModal

---

### P10-T2 自动化 Recipe

- [ ] **V20**: 保存会话为 Recipe
  - [ ] 完成一个成功的会话（如执行 `/skill weekly-report`）
  - [ ] 在 Tab 右键菜单点击「保存为 Recipe」
  - [ ] 弹出 SaveRecipeModal 表单
  - [ ] 名称、描述、Skill 名、参数模板自动填充
  - [ ] 选择触发条件「手动」
  - [ ] 保存
  - [ ] 验证 `~/.reasonix/recipes/<name>.json` 存在且内容正确
- [ ] **V21**: 从 Recipe 创建定时任务
  - [ ] 打开 SchedulerPanel → 新建定时任务
  - [ ] 表单中有「从 Recipe 选择」下拉
  - [ ] 选择刚才保存的 Recipe
  - [ ] Skill 名和参数模板自动填充
  - [ ] 保存任务并验证可执行
- [ ] **V22**: 事件触发 Recipe
  - [ ] 创建一个 Recipe，触发条件为「收到含『发票』的邮件」
  - [ ] 模拟收到一封主题含「发票」的邮件
  - [ ] 自动创建新会话执行该 Recipe
  - [ ] 弹出系统通知「事件触发 Recipe：发票整理」
  - [ ] 验证新 Tab 打开、执行过程可见

---

### P10-T3 剪贴板历史

- [ ] **V23**: 剪贴板历史记录与搜索
  - [ ] 启用 `[clipboard_history] enabled=true`
  - [ ] 复制几段文本（中英文混合）
  - [ ] 打开 FloatingWindow → 「剪贴板历史」Tab
  - [ ] 列表显示所有复制过的条目（时间倒序）
  - [ ] 每条显示时间 + 类型图标 + 前 80 字符预览
  - [ ] 在搜索框输入关键词
  - [ ] 列表实时过滤为匹配项
  - [ ] 点击某条目
  - [ ] 内容重新复制到系统剪贴板
  - [ ] 显示 toast「已复制」
- [ ] P10-T3.2.3: 支持三种 kind
  - [ ] 复制纯文本 → kind=text
  - [ ] 复制图片 → kind=image，保存为文件路径
  - [ ] 复制文件 → kind=file，保存路径列表
- [ ] **V24**: 剪贴板历史自动清理
  - [ ] 配置 `retention_days=1`
  - [ ] 手动插入一条 2 天前的条目到 SQLite
  - [ ] 重启 Reasonix
  - [ ] 该条目已被清理
- [ ] P10-T3.4.1: 超过上限删除最旧
  - [ ] 配置 `max_entries=10`
  - [ ] 复制 15 次内容
  - [ ] 列表只保留最近 10 条

---

### P10-T4 语音输入

- [ ] **V25**: 语音输入热键唤起
  - [ ] 启用 `[voice_input] enabled=true` + 配置有效 `whisper_path`
  - [ ] 在 Composer 聚焦状态
  - [ ] 按下 `Ctrl+Shift+V`
  - [ ] 显示录音浮层（红色圆点 + 波形动画）
  - [ ] 说一段话
  - [ ] 停止说话 2 秒后浮层消失
  - [ ] 转写结果填入 Composer 输入框
- [ ] P10-T4.3.2: 再次按键结束录音
  - [ ] 录音中再次按 `Ctrl+Shift+V`
  - [ ] 立即结束录音并转写
- [ ] **V26**: whisper 不可用时降级提示
  - [ ] 启用 `[voice_input] enabled=true` 但不安装 whisper
  - [ ] 按下 `Ctrl+Shift+V`
  - [ ] 弹出 toast「语音输入需要 whisper，点击查看安装指引」
  - [ ] 其他功能不受影响
- [ ] P10-T4.5.2: 安装指引文档存在
  - [ ] `docs/voice-input-setup.md` 存在
  - [ ] 包含 Windows / macOS / Linux 安装步骤

---

### P10 回归测试

- [ ] P10-REG-1: 禁用所有 P10 新功能后行为与改造前一致
  - [ ] `[pkm] auto_learn=false`
  - [ ] `[clipboard_history] enabled=false`
  - [ ] `[voice_input] enabled=false`
  - [ ] `[recipe] enabled=false`
- [ ] P10-REG-2: 编码模式零回归
  - [ ] `go test ./...` 通过
  - [ ] 现有编码功能全部正常

---

## Phase 11 · 编码能力再增强

### P11-T1 多 Agent 并行编排

- [ ] **V27**: spawn_agent 并行执行两个子 Agent
  - [ ] 在对话中触发主 Agent 调用 `spawn_agent` 启动两个 worker 角色
  - [ ] 两个子 Agent 独立 Session 并行执行
  - [ ] 两个子 Agent 拥有完整的 Agent 循环（推理→工具→反馈）
  - [ ] 子 Agent 工具集按 Role 过滤（worker 有写工具，explorer 只读）
  - [ ] 最大并发数 `max_threads=6` 生效
  - [ ] 最大嵌套深度 `max_depth=1` 防止递归
- [ ] **V28**: wait_agent 等待并返回结构化结果
  - [ ] 主 Agent 调用 `wait_agent` 等待某个子 Agent
  - [ ] 阻塞直到该子 Agent 完成
  - [ ] 返回 `AgentResult` 含 summary / tool_calls / files_read / files_write / duration / usage
- [ ] P11-T1.2.3: 子 Agent 继承父 Agent 沙盒约束
  - [ ] 父 Agent 在 `workspace-write` 模式
  - [ ] 子 Agent 也限制为 `workspace-write`
  - [ ] 子 Agent 尝试越界写操作被阻断
- [ ] P11-T1.2.4: send_input / close_agent
  - [ ] 主 Agent 调用 `send_input` 向运行中子 Agent 发送追加指令
  - [ ] 子 Agent 收到并处理
  - [ ] 主 Agent 调用 `close_agent` 强制终止子 Agent
  - [ ] 子 Agent 进程清理干净
- [ ] **V29**: AgentCanvas 渲染子图
  - [ ] 打开 AgentCanvas
  - [ ] 主 Agent 节点下渲染两个子图容器
  - [ ] 子图内显示各自子 Agent 的 ToolDispatch/Result 节点
  - [ ] 子 Agent 完成时子图边框变绿
  - [ ] 子图可折叠（点击收起/展开）

---

### P11-T2 generate-tests Skill

- [ ] **V30**: generate-tests Skill 生成 table-driven 测试
  - [ ] 在对话中说「为 `internal/control/controller.go` 的 `Send` 方法生成测试」
  - [ ] 触发 `generate-tests` Skill
  - [ ] Skill 读取目标函数源码
  - [ ] 分析函数签名
  - [ ] 生成 table-driven 测试
  - [ ] 测试覆盖正常 / 边界 / 错误三类用例
  - [ ] 测试函数命名为 `TestSend_CaseName`
  - [ ] 写入到 `internal/control/controller_test.go`（或追加到现有文件）
  - [ ] 运行 `go test ./internal/control/...` 通过

---

### P11-T3 review-pr Skill

- [ ] **V31**: review-pr Skill 输出审查报告
  - [ ] 在当前分支制造若干改动（修改 / 新增 / 删除文件）
  - [ ] 在对话中说「审查当前分支相对于 main 的 diff」
  - [ ] 触发 `review-pr` Skill
  - [ ] Skill 调用 `git diff main...HEAD` 获取变更
  - [ ] 逐文件分析变更
  - [ ] 输出 Markdown 审查报告
  - [ ] 报告含：文件名 + 行号 + 严重级别（🔴问题/🟡建议/🔵风险）+ 描述
  - [ ] 末尾汇总：问题数 / 建议数 / 总体评价

---

### P11 回归测试

- [ ] P11-REG-1: 现有 TaskTool 行为兼容
  - [ ] 现有调用 `task` 工具的 Skill 仍可工作（向后兼容）
  - [ ] 或 `task` 工具被 `spawn_agent` 完全替代且 Skill 已迁移
- [ ] P11-REG-2: 编码模式零回归
  - [ ] `go test ./...` 通过
  - [ ] 现有编码功能全部正常

---

## 全局验收

- [ ] **V32**: 编码模式零回归（全部现有测试通过）
  - [ ] P8-REG-1 / P9-REG-1 / P10-REG-1 / P11-REG-1 全部通过
  - [ ] 端到端测试：read_file / edit_file / bash / grep / glob / write_file 等核心工具正常
  - [ ] CodeGraph / Skill / Subagent / Memory 等核心系统正常
- [ ] **V33**: 禁用所有新功能后行为与改造前一致
  - [ ] `[pkm] enabled=false` + `auto_learn=false`
  - [ ] `[clipboard_history] enabled=false`
  - [ ] `[voice_input] enabled=false`
  - [ ] `[recipe] enabled=false`
  - [ ] `[agents]` 节缺失（spawn_agent 不注册）
  - [ ] 邮件插件走原有 `MAIL_IMAP_PASS` 路径
  - [ ] ProgressStepper / AgentCanvas 在无 StepProgress 事件时退化为原有行为
  - [ ] DailyBrief 入口不显示（或显示但无数据时不报错）
  - [ ] 模式联动可关闭（如配置 `[ui] mode_linkage=false`）

---

## 验收流程建议

### 每个 Phase 完成后

1. **自测**：开发者按本 checklist 逐项打勾
2. **回归**：运行 `go test ./...` + 前端测试
3. **交叉验证**：另一开发者（或用户视角）重新执行一遍
4. **文档同步**：确认 `docs/` 下相关文档已更新
5. **合并**：PR 合并到主分支

### 全部完成后

1. **端到端验收**：按 V1-V33 顺序执行全部验收项
2. **用户场景演练**：
   - 新用户场景：Onboarding → 首任务 → 保存 Recipe → 定时触发
   - 开发者场景：编码 → review-pr → generate-tests → 多 Agent 并行
   - 办公场景：周报生成 → 表格分析 → PPT 制作 → 文档预览
   - 助手场景：每日简报 → 邮件处理 → 语音输入 → 剪贴板历史
3. **性能基线**：对比 v2.0 的响应时间、内存占用、磁盘占用
4. **发布**：更新 `CHANGELOG.md`，打 tag `v3.0.0`

---

**文档版本**：v3.0
**最后更新**：2026-07-02

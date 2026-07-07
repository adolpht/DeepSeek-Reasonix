# Tasks

## Phase 5：ToC 界面改造 + PPT + 调研

### P5-1: 界面架构改造

- [x] Task 1: 扩展 WorkspaceType 类型系统
  - [x] SubTask 1.1: 在 `types.ts` 中将 `WorkspaceType` 扩展为 `"coding" | "office" | "assistant"`
  - [x] SubTask 1.2: 在 Go 端 `wire.go` / `app.go` 中同步新增 WorkspaceType 值
  - [x] SubTask 1.3: 在 `bridge.ts` 中更新 `SetWorkspaceType` 方法签名
  - [x] SubTask 1.4: 更新 `i18n.tsx` 新增 "assistant" 模式翻译

- [x] Task 2: 实现 ModeSwitcher 组件
  - [x] SubTask 2.1: 创建 `ModeSwitcher.tsx` 组件，渲染三模式切换器（编码/办公/助手）
  - [x] SubTask 2.2: 替换顶栏中现有 WorkspaceTypeSwitch 为 ModeSwitcher
  - [x] SubTask 2.3: 实现模式切换联动：WorkspaceType + 侧边栏 + 右面板 + 主题

- [x] Task 3: 侧边栏动态化重构
  - [x] SubTask 3.1: 将侧边栏从 App.tsx 内联代码抽取为独立组件结构
  - [x] SubTask 3.2: 实现侧边栏分区布局（首页/工作台/资源/管理）
  - [x] SubTask 3.3: 实现侧边栏内容随 WorkspaceType 动态渲染
  - [x] SubTask 3.4: 编码模式：项目树 + Skill 模板 + 开发工具插件
  - [x] SubTask 3.5: 办公模式：文档模板库 + 最近文档 + 办公工具插件
  - [x] SubTask 3.6: 助手模式：日程 + 待办 + 信息流 + 生活工具插件

- [x] Task 4: 右侧面板预览 Tab
  - [x] SubTask 4.1: 在 RightDockMode 中新增 "preview" 模式
  - [x] SubTask 4.2: 实现预览 Tab 根据模式显示不同内容（编码→Diff，办公→文档预览，助手→摘要）
  - [x] SubTask 4.3: 启用 Context Tab（移除 `SHOW_CONTEXT_DOCK=false` 限制）

- [x] Task 5: 首页面板（HomePanel）
  - [x] SubTask 5.1: 创建 `HomePanel.tsx` 组件
  - [x] SubTask 5.2: 实现时间感知问候语
  - [x] SubTask 5.3: 实现快捷操作卡片（基于身份推荐，3-4 个）
  - [x] SubTask 5.4: 实现最近任务列表（调用后端 API 获取历史）
  - [x] SubTask 5.5: 实现常用 Skill 快捷入口
  - [x] SubTask 5.6: 实现每日提示
  - [x] SubTask 5.7: 首页作为默认视图（新 Tab 或启动时显示）

- [x] Task 6: 任务进度条（ProgressStepper）
  - [x] SubTask 6.1: 创建 `ProgressStepper.tsx` 组件
  - [x] SubTask 6.2: 在 WireEvent 中扩展步骤进度事件类型
  - [x] SubTask 6.3: 在主对话区顶部集成 ProgressStepper
  - [x] SubTask 6.4: 实现步骤点击跳转到对应消息

- [x] Task 7: 富媒体工具卡片（RichToolCard）
  - [x] SubTask 7.1: 创建 `RichToolCard.tsx` 组件，根据工具返回类型渲染不同卡片
  - [x] SubTask 7.2: 实现文档卡片（缩略图 + 操作按钮）
  - [x] SubTask 7.3: 实现表格卡片（内嵌表格视图 + 排序筛选）
  - [x] SubTask 7.4: 实现图表卡片（内嵌图片 + 保存按钮）
  - [x] SubTask 7.5: 实现代码卡片（语法高亮 + Diff + 应用更改）
  - [x] SubTask 7.6: 替换 Transcript 中现有工具输出为 RichToolCard

- [x] Task 8: 亮色主题与办公风格
  - [x] SubTask 8.1: 在 `theme.ts` 中新增办公风格预设（米白/浅灰 + 蓝色强调）
  - [x] SubTask 8.2: 在 `styles.css` 中新增办公风格 CSS 变量集
  - [x] SubTask 8.3: 实现模式关联主题自动切换逻辑
  - [x] SubTask 8.4: 在设置中新增「统一主题」开关

- [x] Task 9: Composer 输入框增强
  - [x] SubTask 9.1: 在 Composer 中新增附件按钮 📎，弹出文件选择对话框
  - [x] SubTask 9.2: 支持图片/PDF/Office 文件作为附件上传
  - [x] SubTask 9.3: 增强拖拽文件引用，支持从系统文件管理器拖入

### P5-2: 新手引导

- [x] Task 10: 新手引导流程（OnboardingFlow）
  - [x] SubTask 10.1: 创建 `OnboardingFlow.tsx` 组件
  - [x] SubTask 10.2: 实现欢迎页
  - [x] SubTask 10.3: 实现身份选择页（开发者/办公人员/自由职业者/其他）
  - [x] SubTask 10.4: 实现 API Key 配置引导（支持扫码/粘贴/跳过）
  - [x] SubTask 10.5: 实现引导式首次任务（根据身份推荐不同任务）
  - [x] SubTask 10.6: 实现跳过引导功能
  - [x] SubTask 10.7: 在 App.tsx 中集成 OnboardingFlow（检测首次启动）

### P5-3: 意图分类器

- [x] Task 11: 意图分类器（IntentClassifier）
  - [x] SubTask 11.1: 在 Go 端 `internal/control/` 中实现规则引擎版分类器
  - [x] SubTask 11.2: 定义三类意图关键词库（编码/办公/助手）
  - [x] SubTask 11.3: 实现 Skill 匹配逻辑（用户输入匹配 Skill 描述时路由到对应模式）
  - [x] SubTask 11.4: 在 Controller Submit 流程中集成分类器
  - [x] SubTask 11.5: 前端接收分类结果事件，展示模式切换建议
  - [x] SubTask 11.6: 实现自动模式切换（可关闭，设置项控制）
  - [x] SubTask 11.7: 分类结果在事件流中可见（WireEvent 扩展）

### P5-4: PPT 生成插件

- [x] Task 12: PPT 生成 MCP 插件（Rexion-plugin-slides）
  - [x] SubTask 12.1: 创建 `cmd/Rexion-plugin-slides/` 目录和 main.go 入口
  - [x] SubTask 12.2: 实现 MCP 服务器框架（stdio JSON-RPC）
  - [x] SubTask 12.3: 实现 `create_ppt` 工具：从 Markdown 大纲生成 PPT
  - [x] SubTask 12.4: 实现 `add_slide` 工具：添加单页幻灯片
  - [x] SubTask 12.5: 实现 `apply_theme` 工具：应用主题模板
  - [x] SubTask 12.6: 实现 `add_chart` 工具：插入图表页
  - [x] SubTask 12.7: 实现 `export_pdf` 工具：PPT 导出为 PDF
  - [x] SubTask 12.8: 集成 `unidoc/unioffice` 依赖（纯 Go，CGO_ENABLED=0 兼容）
  - [x] SubTask 12.9: 编写单元测试

- [x] Task 13: PPT 生成 Skill
  - [x] SubTask 13.1: 创建 `generate-ppt.md` Skill 文件
  - [x] SubTask 13.2: 定义 Skill frontmatter（description, runAs: subagent, tools 列表）
  - [x] SubTask 13.3: 编写执行步骤 Prompt（规划大纲→调研→生成→应用主题→返回）

- [x] Task 14: PPT 预览组件（SlideViewer）
  - [x] SubTask 14.1: 创建 `SlideViewer.tsx` 组件
  - [x] SubTask 14.2: 实现幻灯片缩略图网格渲染
  - [x] SubTask 14.3: 实现点击放大查看单页
  - [x] SubTask 14.4: 集成到右侧预览面板

### P5-5: 搜索调研插件

- [x] Task 15: 搜索调研 MCP 插件（Rexion-plugin-search）
  - [x] SubTask 15.1: 创建 `cmd/Rexion-plugin-search/` 目录和 main.go 入口
  - [x] SubTask 15.2: 实现 MCP 服务器框架（stdio JSON-RPC）
  - [x] SubTask 15.3: 实现 `web_search` 工具：集成搜索 API（SerpAPI/Bing）
  - [x] SubTask 15.4: 实现 `web_extract` 工具：从网页提取结构化信息
  - [x] SubTask 15.5: 实现 `compare_table` 工具：生成对比表格
  - [x] SubTask 15.6: 支持用户自带 API Key 配置
  - [x] SubTask 15.7: 编写单元测试

- [x] Task 16: 竞品调研 Skill
  - [x] SubTask 16.1: 创建 `research-report.md` Skill 文件
  - [x] SubTask 16.2: 定义 Skill frontmatter（description, runAs: subagent, tools 列表）
  - [x] SubTask 16.3: 编写执行步骤 Prompt（拆解维度→搜索→提取→生成报告）

- [x] Task 17: 搜索结果卡片
  - [x] SubTask 17.1: 在 RichToolCard 中新增搜索结果卡片类型
  - [x] SubTask 17.2: 实现搜索结果列表渲染（标题+摘要+URL）

### P5-6: 文档/表格预览增强

- [x] Task 18: DocPreviewer 组件
  - [x] SubTask 18.1: 创建 `DocPreviewer.tsx` 组件
  - [x] SubTask 18.2: 实现 docx → HTML 分页预览（前 5 页）
  - [x] SubTask 18.3: 实现「用默认应用打开」「导出到工作区」按钮
  - [x] SubTask 18.4: 实现预览结果缓存（mediaTokenStore）

- [x] Task 19: SheetViewer 组件
  - [x] SubTask 19.1: 创建 `SheetViewer.tsx` 组件
  - [x] SubTask 19.2: 实现 xlsx → 简易表格视图
  - [x] SubTask 19.3: 实现排序和筛选功能
  - [x] SubTask 19.4: 实现「用默认应用打开」「导出到工作区」按钮

- [x] Task 20: PDF 预览
  - [x] SubTask 20.1: 在 DocPreviewer 中扩展 PDF 分页图片预览
  - [x] SubTask 20.2: 实现图片文件内联预览

### P5-7: 数据模型与后端支撑

- [x] Task 21: 数据模型扩展
  - [x] SubTask 21.1: 在 Go 端新增 scheduled_tasks 表结构
  - [x] SubTask 21.2: 新增 todos 表结构
  - [x] SubTask 21.3: 新增 notifications 表结构
  - [x] SubTask 21.4: 实现数据库迁移逻辑（SQLite，兼容现有 JSONL）

- [x] Task 22: Wails 绑定层扩展
  - [x] SubTask 22.1: 在 `app.go` 中新增首页数据相关导出方法
  - [x] SubTask 22.2: 新增通知相关导出方法
  - [x] SubTask 22.3: 在 `bridge.ts` 中同步更新 AppBindings 接口
  - [x] SubTask 22.4: 更新浏览器 Dev Mock 实现

---

## Phase 6：个人化 + 自动化 + 远程

### P6-1: 日程与待办管理

- [x] Task 23: 日程管理 MCP 插件（Rexion-plugin-calendar）
  - [x] SubTask 23.1: 创建 `cmd/Rexion-plugin-calendar/` 目录和 main.go 入口
  - [x] SubTask 23.2: 实现 `read_event` 工具：读取系统日历事件
  - [x] SubTask 23.3: 实现 `add_event` 工具：创建日历事件
  - [x] SubTask 23.4: 实现 `list_todo` 工具：查看待办列表
  - [x] SubTask 23.5: 实现 `add_todo` / `update_todo` 工具
  - [x] SubTask 23.6: macOS Calendar / Windows Outlook 集成
  - [x] SubTask 23.7: 编写单元测试

- [x] Task 24: 日程视图组件
  - [x] SubTask 24.1: 创建 `CalendarPanel.tsx` 组件
  - [x] SubTask 24.2: 实现日/周/月视图
  - [x] SubTask 24.3: 集成到助手模式侧边栏

### P6-2: 邮件处理

- [x] Task 25: 邮件处理 MCP 插件（Rexion-plugin-mail）
  - [x] SubTask 25.1: 创建 `cmd/Rexion-plugin-mail/` 目录和 main.go 入口
  - [x] SubTask 25.2: 实现 IMAP 客户端（`go-imap` 纯 Go 库）
  - [x] SubTask 25.3: 实现 SMTP 发送
  - [x] SubTask 25.4: 实现 `read_mail` 工具：读取收件箱
  - [x] SubTask 25.5: 实现 `send_mail` 工具：发送邮件
  - [x] SubTask 25.6: 实现 `search_mail` 工具：搜索邮件
  - [x] SubTask 25.7: 实现 `classify_mail` 工具：LLM 分类邮件
  - [x] SubTask 25.8: 支持 OAuth2 授权（Gmail/Outlook）
  - [x] SubTask 25.9: 编写单元测试

- [x] Task 26: 邮件处理 Skill
  - [x] SubTask 26.1: 创建 `email-summary.md` Skill
  - [x] SubTask 26.2: 创建 `invoice-collect.md` Skill（发票整理）

### P6-3: IM 远程控制

- [x] Task 27: IM 远程控制 MCP 插件（Rexion-plugin-im）
  - [x] SubTask 27.1: 创建 `cmd/Rexion-plugin-im/` 目录和 main.go 入口
  - [x] SubTask 27.2: 实现本地 HTTP Bot 服务框架
  - [x] SubTask 27.3: 实现企业微信机器人 Webhook 对接
  - [x] SubTask 27.4: 实现飞书机器人 API 对接
  - [x] SubTask 27.5: 实现钉钉机器人 API 对接
  - [x] SubTask 27.6: 实现远程指令解析与执行（创建会话任务）
  - [x] SubTask 27.7: 实现执行结果推送
  - [x] SubTask 27.8: 编写单元测试

### P6-4: 剪贴板助手

- [x] Task 28: 剪贴板助手
  - [x] SubTask 28.1: 在 Go 端实现全局热键注册（Ctrl+Shift+R）
  - [x] SubTask 28.2: 创建 `FloatingWindow.tsx` 悬浮窗组件（Wails 多窗口）
  - [x] SubTask 28.3: 实现剪贴板读取 → Agent 处理 → 结果写入剪贴板
  - [x] SubTask 28.4: 实现处理选项菜单（翻译/总结/润色/续写/解释/优化）
  - [x] SubTask 28.5: 确保 2 秒内响应

### P6-5: 定时任务

- [x] Task 29: 定时任务调度器
  - [x] SubTask 29.1: 在 Go 端集成 `robfig/cron` 调度引擎
  - [x] SubTask 29.2: 实现 Cron 表达式解析与任务注册
  - [x] SubTask 29.3: 实现任务执行日志记录
  - [x] SubTask 29.4: 实现执行结果通知

- [x] Task 30: SchedulerPanel UI
  - [x] SubTask 30.1: 创建 `SchedulerPanel.tsx` 组件
  - [x] SubTask 30.2: 实现定时任务列表展示
  - [x] SubTask 30.3: 实现新增/编辑/暂停/删除操作
  - [x] SubTask 30.4: 集成到侧边栏管理区域

### P6-6: 长期记忆增强

- [x] Task 31: 个人知识库
  - [x] SubTask 31.1: 实现 `~/.Rexion/memory/` 目录初始化
  - [x] SubTask 31.2: 创建默认记忆文件（people.md / projects.md / preferences.md / writing_style.md）
  - [x] SubTask 31.3: 在 Agent 上下文中自动引用记忆文件
  - [x] SubTask 31.4: 创建 `MemoryPanel.tsx` 组件，支持多文件切换和编辑
  - [x] SubTask 31.5: 集成到助手模式侧边栏

---

# Task Dependencies

- Task 1 (类型扩展) → Task 2 (ModeSwitcher) → Task 3 (侧边栏) → Task 5 (HomePanel)
- Task 1 → Task 4 (右面板预览)
- Task 1 → Task 8 (主题)
- Task 2 → Task 6 (ProgressStepper) → Task 7 (RichToolCard)
- Task 7 → Task 17 (搜索卡片)
- Task 12 (PPT插件) → Task 13 (PPT Skill) → Task 14 (SlideViewer)
- Task 15 (搜索插件) → Task 16 (调研 Skill) → Task 17 (搜索卡片)
- Task 18/19/20 (预览组件) → Task 4 (右面板集成)
- Task 21 (数据模型) → Task 29/30 (定时任务)
- Task 22 (Wails绑定) → Task 5/6/7/10/11 (前端组件)
- Task 10 (新手引导) 依赖 Task 2 (ModeSwitcher) 和 Task 5 (HomePanel)
- Task 11 (意图分类器) 依赖 Task 1 (类型扩展) 和 Task 2 (ModeSwitcher)

**P5 已全部完成** ✅
**P6 已全部完成** ✅

- Task 23/25/27 (MCP插件) 已并行开发完成
- Task 28 (剪贴板) 已完成
- Task 29 (调度器) 已完成（依赖 Task 21 数据模型）
- Task 31 (记忆) 已完成

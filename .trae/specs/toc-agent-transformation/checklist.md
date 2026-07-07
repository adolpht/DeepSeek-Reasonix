# Rexion ToC 个人 Agent 改造验收检查清单

## Phase 5 验收

### P5-1: 界面架构改造

- [ ] WorkspaceType 类型已扩展为 "coding" | "office" | "assistant"，前后端类型一致
- [ ] ModeSwitcher 组件渲染在顶栏中央，支持三模式切换
- [ ] 模式切换联动：WorkspaceType + 侧边栏内容 + 右面板内容 + 主题同步变化
- [ ] 侧边栏已重构为动态分区布局（首页/工作台/资源/管理），内容随模式变化
- [ ] 编码模式侧边栏显示：项目树 + Skill 模板 + 开发工具插件
- [ ] 办公模式侧边栏显示：文档模板库 + 最近文档 + 办公工具插件
- [ ] 助手模式侧边栏显示：日程 + 待办 + 信息流 + 生活工具插件
- [ ] 右侧面板新增「预览」Tab，根据模式显示不同内容
- [ ] Context Tab 已启用（移除 SHOW_CONTEXT_DOCK=false 限制）
- [ ] 编码模式下所有现有功能（项目树、代码 Diff、Git 操作、终端、Skill）行为与改造前完全一致（零回归）

### P5-2: 首页面板

- [ ] HomePanel 组件已创建，包含问候语、快捷操作、最近任务、常用 Skill、每日提示
- [ ] 问候语根据时间段变化（早上好/下午好/晚上好）
- [ ] 快捷操作卡片基于用户身份推荐（3-4 个）
- [ ] 最近任务列表显示任务名称、时间、状态
- [ ] 点击快捷操作卡片可自动切换模式并预填 Skill 命令
- [ ] 首页作为新 Tab 或启动时的默认视图

### P5-3: 任务进度条

- [ ] ProgressStepper 组件已创建，在主对话区顶部显示
- [ ] 显示总进度百分比、当前步骤编号/总步骤数
- [ ] 每个步骤显示完成状态（✅/⏳/⬚）和描述
- [ ] 点击已完成步骤可跳转到对应消息位置
- [ ] WireEvent 已扩展步骤进度事件类型

### P5-4: 富媒体工具卡片

- [ ] RichToolCard 组件已创建，根据工具返回类型渲染不同卡片
- [ ] 文档卡片：缩略图预览 + 文件名 + 打开/导出/编辑按钮
- [ ] 表格卡片：内嵌小型表格视图（前 10 行）+ 排序/筛选 + 打开/导出按钮
- [ ] 图表卡片：内嵌图片 + 保存为 PNG 按钮
- [ ] 代码卡片：语法高亮 + Diff 视图 + 应用更改按钮
- [ ] Transcript 中现有工具输出已替换为 RichToolCard

### P5-5: 新手引导

- [ ] OnboardingFlow 组件已创建
- [ ] 欢迎页展示「Rexion 是你的 AI 工作伙伴」
- [ ] 身份选择页支持：开发者/办公人员/自由职业者/其他
- [ ] API Key 配置引导支持扫码/粘贴/跳过（本地模型）
- [ ] 引导式首次任务根据身份推荐不同任务
- [ ] 跳过引导功能正常，跳过后默认编码模式
- [ ] 首次启动自动触发引导，非首次启动不触发

### P5-6: 意图分类器

- [ ] 规则引擎版分类器已实现（关键词 + Skill 匹配）
- [ ] 三类意图关键词库已定义（编码/办公/助手）
- [ ] Controller Submit 流程中已集成分类器
- [ ] 前端接收分类结果事件，展示模式切换建议
- [ ] 自动模式切换可通过设置关闭
- [ ] 分类结果在事件流中可见

### P5-7: PPT 生成插件

- [ ] Rexion-plugin-slides 独立二进制已创建
- [ ] create_ppt 工具：从 Markdown 大纲生成 PPT，每页文字不超过 100 字
- [ ] add_slide 工具：添加单页幻灯片
- [ ] apply_theme 工具：应用主题模板（professional/creative/minimal）
- [ ] add_chart 工具：插入图表页（柱状图/折线图/饼图）
- [ ] export_pdf 工具：PPT 导出为 PDF
- [ ] 依赖 unidoc/unioffice，CGO_ENABLED=0 兼容
- [ ] generate-ppt Skill 已创建，支持一句话生成 PPT
- [ ] SlideViewer 组件已创建，支持幻灯片缩略图网格和点击放大

### P5-8: 搜索调研插件

- [ ] Rexion-plugin-search 独立二进制已创建
- [ ] web_search 工具：返回搜索结果列表（标题、摘要、URL），至少 5 个来源
- [ ] web_extract 工具：从网页提取结构化信息
- [ ] compare_table 工具：生成对比表格，可选导出 xlsx
- [ ] 支持用户自带 API Key 配置
- [ ] research-report Skill 已创建，支持一句话调研生成报告
- [ ] 搜索结果卡片已集成到 RichToolCard

### P5-9: 文档/表格预览增强

- [ ] DocPreviewer 组件已创建，支持 docx → HTML 分页预览（前 5 页）
- [ ] SheetViewer 组件已创建，支持 xlsx → 简易表格视图 + 排序筛选
- [ ] PDF 分页图片预览已实现
- [ ] 图片文件内联预览已实现
- [ ] 「用默认应用打开」「导出到工作区」按钮功能正常
- [ ] 预览结果缓存（mediaTokenStore）已实现

### P5-10: 亮色主题与办公风格

- [ ] 办公风格预设已新增（米白/浅灰 + 蓝色强调）
- [ ] 办公风格 CSS 变量集已添加到 styles.css
- [ ] 模式关联主题自动切换：编码→暗色，办公/助手→亮色
- [ ] 设置中「统一主题」开关已实现，启用后所有模式使用同一主题

### P5-11: Composer 输入框增强

- [ ] 附件按钮 📎 已添加，弹出文件选择对话框
- [ ] 支持图片/PDF/Office 文件作为附件上传
- [ ] 拖拽文件引用支持从系统文件管理器拖入

### P5-12: 数据模型与后端支撑

- [ ] scheduled_tasks 表已创建（id, name, cron, skill, parameters, enabled, last_run, next_run, created_at）
- [ ] todos 表已创建（id, title, description, due_date, priority, status, source, created_at, updated_at）
- [ ] notifications 表已创建（id, kind, title, body, read, created_at）
- [ ] 数据库迁移逻辑已实现（SQLite，兼容现有 JSONL）
- [ ] Wails 绑定层已扩展（首页数据、通知相关导出方法）
- [ ] bridge.ts AppBindings 接口已同步更新
- [ ] 浏览器 Dev Mock 实现已更新

---

## Phase 5 总体验收

- [ ] 新用户安装后 3 分钟内完成首个任务
- [ ] 三种模式可切换，侧边栏内容随模式动态变化
- [ ] 一句话生成 15 页 PPT，可在界面内预览
- [ ] 一句话调研一个主题，返回 docx 报告 + xlsx 对比表
- [ ] Agent 生成的 docx/xlsx 在右侧面板可预览
- [ ] 编码模式功能不降级（零回归）

---

## Phase 6 验收

### P6-1: 日程与待办管理

- [ ] Rexion-plugin-calendar 插件已创建
- [ ] read_event 工具：读取系统日历事件（macOS/Windows）
- [ ] add_event 工具：创建日历事件
- [ ] list_todo / add_todo / update_todo 工具功能正常
- [ ] CalendarPanel 组件已创建，支持日/周/月视图
- [ ] 日程视图已集成到助手模式侧边栏

### P6-2: 邮件处理

- [ ] Rexion-plugin-mail 插件已创建
- [ ] IMAP 客户端已实现（go-imap 纯 Go 库）
- [ ] SMTP 发送已实现
- [ ] read_mail / send_mail / search_mail / classify_mail 工具功能正常
- [ ] OAuth2 授权支持 Gmail/Outlook
- [ ] email-summary / invoice-collect Skill 已创建

### P6-3: IM 远程控制

- [ ] Rexion-plugin-im 插件已创建
- [ ] 本地 HTTP Bot 服务框架已实现
- [ ] 企业微信机器人 Webhook 对接正常
- [ ] 飞书机器人 API 对接正常
- [ ] 钉钉机器人 API 对接正常
- [ ] 远程指令解析与执行正常
- [ ] 执行结果推送正常
- [ ] 所有 IM 连接走本地 HTTP 回调，不经过第三方服务器

### P6-4: 剪贴板助手

- [ ] 全局热键 Ctrl+Shift+R 注册成功
- [ ] FloatingWindow 悬浮窗组件已创建（Wails 多窗口）
- [ ] 剪贴板读取 → Agent 处理 → 结果写入剪贴板流程正常
- [ ] 处理选项菜单已实现（翻译/总结/润色/续写/解释/优化）
- [ ] 悬浮窗 2 秒内响应

### P6-5: 定时任务

- [ ] robfig/cron 调度引擎已集成
- [ ] Cron 表达式解析与任务注册正常
- [ ] 任务执行日志记录正常
- [ ] 执行结果通知正常
- [ ] SchedulerPanel UI 已创建
- [ ] 定时任务列表展示正常（名称、Cron、Skill、上次执行时间、状态）
- [ ] 新增/编辑/暂停/删除操作正常

### P6-6: 长期记忆增强

- [ ] ~/.Rexion/memory/ 目录初始化正常
- [ ] 默认记忆文件已创建（people.md / projects.md / preferences.md / writing_style.md）
- [ ] Agent 自动引用记忆文件作为上下文
- [ ] MemoryPanel 组件已创建，支持多文件切换和编辑
- [ ] 记忆面板已集成到助手模式侧边栏

---

## Phase 6 总体验收

- [ ] 通过微信发送「帮我生成周报」，电脑端自动执行并推送结果
- [ ] 定时任务按计划执行，结果可查
- [ ] 邮件自动分类，摘要准确率 > 80%
- [ ] 全局热键呼出悬浮窗，2 秒内响应
- [ ] Agent 能引用个人偏好（如「按我喜欢的风格写」）

---

## 通用约束验收

- [ ] 本地优先：所有数据处理在本机完成，不强制上云
- [ ] 零回归：编码模式功能不因改造而降级
- [ ] 插件隔离：新增能力通过 MCP 插件接入，主仓 internal/ 不膨胀
- [ ] 配置驱动：所有新功能可通过配置开关，禁用后行为与改造前一致
- [ ] CGO_ENABLED=0：所有新增依赖必须纯 Go
- [ ] 缓存友好：长任务走 subagent，主会话保持 system prompt 稳定

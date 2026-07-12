# DeepSeek-Reasonix 竞品对标优化计划

## 一、当前能力盘点

### 已有能力（优势项）

| 领域 | 能力 | 对标竞品 |
|------|------|----------|
| **Agent并行** | `spawn_agent` + `wait_agent` + `send_input` + `close_agent`，支持多角色（default/worker/explorer/monitor） | Claude Code并行Agent、Codex多Agent |
| **双模型协作** | executor + planner 分离，cache-stable sessions | 独有优势，竞品均未实现 |
| **Subagent技能系统** | 22个内置Skill（explore/research/review/security-review/test/contract-draft/weekly-report/meeting-minutes/generate-ppt/product-design/sheet-clean/sheet-analysis/email-summary/invoice-collect/research-report/analyze-project/doc-reviewer/generate-tests/review-pr/install-capability/init等） | 超越Claude Code/Trae的技能数 |
| **MCP插件生态** | 9个官方插件（office/sheet/slides/calendar/mail/im/search/dws/example），stdio + HTTP双传输 | 标配水平 |
| **权限与沙箱** | allow/ask/deny规则 + workspace sandbox + macOS Seatbelt + bash校验 | 超越多数竞品 |
| **调度器** | cron定时任务 + 执行日志 + 通知回调 | WorkBuddy的"定时自动化"同级 |
| **CodeGraph** | tree-sitter符号/调用图，零embedding成本 | 独有优势 |
| **Plan模式** | auto_plan分类器 + evidence-backed step sign-off | 与Claude Code的plan模式同级 |
| **桌面端** | Wails桌面客户端 + 集成终端 + 预览 + DiffView + AgentCanvas + 审批弹窗 + 工作流编辑器 | 基本框架已有 |
| **IM集成** | 钉钉/飞书/企微 bot + callback retry | WorkBuddy同级 |
| **会话管理** | branch/switch/rewind/compact/tree | 较完善 |
| **记忆系统** | AGENTS.md + memory store + auto-learn + quickadd | 基础能力已有 |

### 缺失能力（差距项）

| 领域 | 缺失 | 竞品已有 |
|------|------|----------|
| **Git Worktree隔离** | 多Agent并行时无代码隔离，存在冲突风险 | Claude Code、Codex均实现 |
| **Browser Use** | 无浏览器自动化能力（仅web_fetch抓取） | Trae Work、Codex、Devin Desktop均实现 |
| **设计转代码** | 无设计稿解析→代码能力 | CodeBuddy内置Figma、Trae Design模式 |
| **PR自动审查/合并** | 仅有代码审查skill，无PR状态追踪和自动修复 | Claude Code的auto-fix/auto-merge |
| **跨设备同步** | 会话不可跨CLI/桌面/Web迁移 | Claude Code的/desktop + 手机接力 |
| **Computer Use** | 无桌面级应用操作能力 | Codex/ChatGPT Work的Computer Use |
| **全局记忆** | 无跨会话持久化记忆（仅AGENTS.md项目级） | Trae Work全局记忆、Windsurf Memories |
| **可拖拽面板布局** | 桌面端面板布局固定 | Claude Code拖拽式面板 |
| **Remote远程控制** | IM bot仅能接收消息推送，无法反向触发Agent执行 | WorkBuddy微信远程控制 |
| **Skills市场** | 无社区Skills分享/安装市场 | Claude Code Skills、Codex Skills市场 |
| **多会话侧边栏** | 桌面端多会话管理不成熟 | Claude Code/Codex的会话侧边栏 |

---

## 二、优化方向与优先级

按**影响力 × 实现难度**排序，分为P0（高优必做）、P1（中期重要）、P2（远期增强）三级。

### P0：核心差距弥补（1-2个迭代周期）

#### 1. Git Worktree 多Agent隔离

**问题**：当前`spawn_agent`多个worker角色并行写代码时，会互相覆盖同一工作区文件，导致冲突和数据丢失。

**方案**：
- 在`spawn_agent`执行时，若检测到当前目录为Git仓库，自动为子Agent创建Git Worktree
- Worktree路径：`.rexion/worktrees/<agent-id>/`
- 子Agent的`workspace_root`自动指向其Worktree
- 新增内置工具 `merge_worktree`：将子Agent的Worktree变更合并回主分支
- 子Agent结束后自动清理Worktree（可配置保留）

**涉及文件**：
- `internal/agent/spawn.go` — SpawnAgentTool增加worktree创建逻辑
- `internal/agent/pool.go` — Pool管理worktree生命周期
- `internal/sandbox/sandbox.go` — sandbox confine感知worktree路径
- `internal/tool/builtin/` — 新增merge_worktree工具
- `desktop/frontend/src/components/` — AgentCanvas显示worktree状态

**验证**：两个worker Agent同时修改不同文件 → 无冲突；修改同一文件 → merge_worktree提示冲突。

---

#### 2. Browser Use 浏览器自动化

**问题**：仅有`web_fetch`做HTTP抓取，无法操作浏览器进行UI测试、表单填写、截图对比等。这是竞品标配。

**方案**：
- 新增MCP插件 `rexion-plugin-browser`
- 基于Chrome DevTools Protocol（CDP）实现
- 核心工具：`browser_navigate`、`browser_click`、`browser_type`、`browser_screenshot`、`browser_evaluate`
- 桌面端PreviewPanel集成浏览器视图，支持元素选择反馈给Agent
- 安全：Browser Use需用户明确授权，默认在沙箱中运行

**涉及文件**：
- `cmd/rexion-plugin-browser/` — 新MCP插件目录
- `desktop/frontend/src/components/PreviewPanel.tsx` — 增强浏览器预览
- `desktop/frontend/src/components/BrowserPanel.tsx` — 新建浏览器控制面板
- `internal/skill/builtins.go` — 新增browser相关skill

**验证**：Agent能自动打开浏览器、导航页面、填写表单、截图并分析。

---

#### 3. 桌面端多会话管理与面板重构

**问题**：桌面端缺乏多会话并行管理界面，面板布局固定不可调。

**方案**：
- 新增SessionSidebar组件：展示所有活跃/历史会话，按项目分组，支持状态过滤
- 实现可拖拽面板布局：终端/预览/Diff/编辑器可自由排列（基于react-mosaic或类似库）
- 侧边会话切换时保留各会话状态
- 新增"Side Chat"概念：从主会话拉出旁路对话，共享上下文但不污染主线

**涉及文件**：
- `desktop/frontend/src/components/SessionSidebar.tsx` — 新建
- `desktop/frontend/src/components/SideChat.tsx` — 新建
- `desktop/frontend/src/components/DraggableLayout.tsx` — 新建
- `desktop/frontend/src/App.tsx` — 重构布局架构
- `desktop/frontend/src/components/AgentCanvas.tsx` — 适配多会话

**验证**：用户可同时运行3个Agent会话，侧边栏显示状态，面板可拖拽重排。

---

### P1：体验增强（2-4个迭代周期）

#### 4. 全局记忆系统

**问题**：当前记忆仅限于AGENTS.md（项目级），无跨项目、跨会话的持久记忆。用户偏好、常用模式、历史决策无法沉淀。

**方案**：
- 扩展`internal/memory/`，增加全局记忆层
- 记忆类型：用户偏好（语言/风格/命名习惯）、常用模式（项目结构偏好）、历史决策（为何选择X而非Y）
- 自动学习：从对话中提取关键决策和偏好，存储到`~/.config/rexion/memory/`
- 桌面端新增MemoryPanel增强：显示全局/项目记忆，支持搜索和编辑
- 每次会话启动时，全局记忆注入系统提示

**涉及文件**：
- `internal/memory/store.go` — 增加全局存储后端
- `internal/memory/autolearn.go` — 增强自动学习逻辑
- `internal/agent/agent.go` — 会话启动时加载全局记忆
- `desktop/frontend/src/components/MemoryPanel.tsx` — 增强UI
- `Rexion.toml` — 新增`[memory]`配置段

**验证**：用户在项目A中设定"使用TypeScript strict模式"，在项目B中Agent自动遵循。

---

#### 5. PR自动审查与CI集成

**问题**：代码审查skill仅输出报告，无法追踪PR状态、自动修复CI失败、自动合并。

**方案**：
- 新增内置工具 `pr_monitor`：通过GitHub CLI追踪PR状态
- 新增内置工具 `auto_fix_ci`：CI失败时自动分析日志、提出修复
- review-pr skill增强：支持`--auto-fix`和`--auto-merge`模式
- 桌面端SourceControlPanel增强：显示PR状态、CI检查结果

**涉及文件**：
- `internal/tool/builtin/prmonitor.go` — 新建
- `internal/tool/builtin/autofix.go` — 新建
- `internal/skill/builtins.go` — review-pr skill增强
- `desktop/frontend/src/components/SourceControlPanel.tsx` — 增强PR状态展示
- `Rexion.toml` — 新增`[git]`配置段（auto_fix, auto_merge开关）

**验证**：Agent创建PR后自动监控CI，失败时自动修复并推送。

---

#### 6. IM远程控制增强

**问题**：当前IM插件仅支持消息推送，用户无法通过IM反向触发Agent执行任务。

**方案**：
- IM插件增加`command`通道：用户发消息→Agent执行→结果回推
- 支持"工作模式"切换：Ask（仅问答）/ Plan（先规划再执行）/ Craft（直接执行）
- 安全：IM触发的任务默认使用Plan模式（需确认后才执行），敏感操作需二次验证
- 支持微信个人号绑定（参考WorkBuddy方案）

**涉及文件**：
- `cmd/rexion-plugin-im/bot.go` — 增加命令解析和执行调度
- `cmd/rexion-plugin-im/dingtalk.go` / `feishu.go` / `wecom.go` — 各平台命令处理
- `internal/agent/agent.go` — 支持IM触发的会话创建
- `desktop/frontend/src/components/IMSessionsPanel.tsx` — 增强远程会话管理

**验证**：用户在钉钉/飞书发"帮我整理桌面Q2数据文件夹的Excel"，Agent执行后回推结果文件。

---

#### 7. Skills市场与安装体验

**问题**：Skills仅支持本地文件，无社区分享和一键安装机制。

**方案**：
- 新增 `rexion skill search <keyword>` 命令：搜索官方Skills索引
- 新增 `rexion skill install <name>` 命令：一键安装
- 官方维护Skills索引仓库（GitHub），社区可PR提交
- 桌面端新增SkillsBrowser组件：浏览、搜索、安装、管理Skills
- 复用已有的`install_source`基础设施

**涉及文件**：
- `internal/cli/skill_view.go` — 增加search/install子命令
- `internal/installsource/skill.go` — 增强远程skill安装
- `desktop/frontend/src/components/TemplateLibrary.tsx` — 扩展为Skills浏览器
- `desktop/frontend/src/components/CapabilitiesPanel.tsx` — 增强skill管理

**验证**：`rexion skill search "deploy"` 返回可用skills，`rexion skill install "deploy-vercel"` 一键安装。

---

### P2：差异化突破（远期）

#### 8. 设计转代码能力

**问题**：无法解析设计稿（Figma/图片）生成代码。CodeBuddy和Trae均有此能力。

**方案**：
- 新增MCP插件 `rexion-plugin-design`
- 支持上传设计稿图片，使用多模态模型解析布局→生成HTML/CSS/Vue/React代码
- 支持Figma文件URL导入（需Figma API Token）
- 新增skill `design-to-code`：设计稿→组件代码→可运行页面
- 集成到桌面端PreviewPanel，支持设计稿与生成代码的并排对比

**涉及文件**：
- `cmd/rexion-plugin-design/` — 新MCP插件
- `internal/skill/builtins.go` — 新增design-to-code skill
- `desktop/frontend/src/components/DesignPanel.tsx` — 新建设计面板

**验证**：上传一个网页截图，Agent生成对应的HTML/CSS代码，在PreviewPanel中可预览。

---

#### 9. Computer Use 桌面级操作

**问题**：Agent无法操作桌面应用（Word、Excel等），只能通过MCP插件间接操作文件。

**方案**：
- 基于Windows UI Automation / macOS Accessibility API实现
- 新增工具 `computer_click`、`computer_type`、`computer_screenshot`、`computer_app_switch`
- 安全：所有操作需用户实时授权，敏感操作（删除、发送）需二次确认
- 桌面端增加"监督模式"：显示Agent的每一步操作，用户可随时中断

**涉及文件**：
- `cmd/rexion-plugin-computer/` — 新MCP插件
- `internal/skill/builtins.go` — 新增office-automation skill
- `desktop/frontend/src/components/SupervisionOverlay.tsx` — 新建监督层

**验证**：Agent自动打开Excel、读取数据、创建图表、保存文件。

---

#### 10. 跨设备会话迁移

**问题**：会话绑定在本地，无法在CLI→桌面→手机之间无缝切换。

**方案**：
- 会话存储支持云端同步（可选，基于用户配置的对象存储）
- 新增CLI命令 `rexion session push` / `rexion session pull`
- 桌面端增加"Continue on another device"按钮
- 手机端（Web/H5）可查看和接续桌面端会话

**涉及文件**：
- `internal/agent/store.go` — 增加云端同步后端
- `internal/agent/session.go` — 会话序列化/反序列化
- `internal/cli/` — 新增session push/pull命令
- `desktop/frontend/src/components/` — 跨设备UI

**验证**：CLI中开始对话→桌面端继续→手机端查看结果。

---

## 三、实施路线图

```
Phase 1 (P0) — 核心差距弥补 ✅ 已完成
├── Git Worktree隔离        ✅ 已实现 — internal/agent/worktree.go + merge_worktree工具
├── Browser Use             ✅ 已实现 — cmd/rexion-plugin-browser/ (CDP协议,6个浏览器工具)
└── 多会话管理+面板重构      ✅ 已实现 — SessionSidebar + SideChat + DraggableLayout组件

Phase 2 (P1) — 体验增强 ✅ 已完成
├── 全局记忆系统            ✅ 已实现 — internal/memory/global.go + recall_global/remember_global工具
├── PR自动审查+CI集成       ✅ 已实现 — pr_monitor + auto_fix_ci工具 + [git]配置段
├── IM远程控制增强          ✅ 已实现 — command.go/dispatcher.go + /ask /plan /craft模式
└── Skills市场              ✅ 已实现 — registry.go/remote.go + skill search/install命令 + SkillsBrowser组件

Phase 3 (P2) — 差异化突破 ✅ 已完成
├── 设计转代码              ✅ 已实现 — rexion-plugin-design/ (5工具) + design-to-code skill + DesignPanel
├── Computer Use            ✅ 已实现 — rexion-plugin-computer/ (9工具,Windows API) + SupervisionOverlay
└── 跨设备会话迁移          ✅ 已实现 — sync.go/serialize.go + session push/pull命令 + SessionSync组件
```

## 四、差异化定位建议

在与竞品对标的同时，应强化以下**独有优势**，形成差异化壁垒：

| 差异化方向 | 现有基础 | 强化策略 |
|-----------|---------|---------|
| **双模型协作** | executor + planner已实现 | 增加visualizer角色（设计→代码），形成三模型流水线 |
| **CodeGraph零成本** | tree-sitter符号图已实现 | 包装为"代码知识图谱"卖点，强调无embedding成本、离线可用 |
| **中文办公场景** | 22个中文Skills + IM/邮件/日历/DWS插件 | 打造"最懂中国开发者的Agent"品牌，持续补充国内特有场景 |
| **安全合规** | sandbox + permission + bash校验已实现 | 对标信通院评估标准，成为国内首批通过智能体治理认证的开源产品 |
| **调度器** | cron定时任务已实现 | 结合IM远程控制，实现"手机发微信→定时执行→结果推送"闭环 |

## 五、预期效果

- **P0完成后**：产品在Agent并行安全性、浏览器自动化、桌面交互体验三个维度对齐Claude Code/Codex，消除核心差距
- **P1完成后**：在中文办公场景（IM远程控制+全局记忆+Skills生态）形成对国外竞品的差异化优势，对标WorkBuddy/Trae Work
- **P2完成后**：在设计转代码和Computer Use上实现能力闭环，成为功能最全面的国产开源Agent产品

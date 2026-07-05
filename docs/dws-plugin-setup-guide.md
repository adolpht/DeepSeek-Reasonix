# Reasonix dws 插件配置引导

## 概述

`reasonix-plugin-dws` 是 Reasonix 的钉钉工作台集成插件,通过封装 [dws CLI](https://github.com/DingTalk-Real-AI/dingtalk-workspace-cli) 将钉钉全产品能力暴露为 MCP 工具,使 AI 可以通过自然语言操作钉钉的 20+ 产品。

### 与 IM 插件的关系

| 插件 | 定位 | 核心能力 |
|------|------|---------|
| **IM 插件** | 消息入口/出口 | 被动接收 IM 消息、回推执行结果 |
| **dws 插件** | 业务数据操作 | 主动操作钉钉产品数据(通讯录/日历/文档/表格/审批/考勤等) |

两者互补:IM 插件负责"听"和"说",dws 插件负责"做"。典型场景——用户在钉钉群 @机器人"帮我查明天日程",IM 插件收到消息后,AI 调用 dws 插件查询日历,再通过 IM 插件回推结果。

### 支持的钉钉产品

dws CLI 覆盖以下产品能力(持续扩展中):

- 通讯录 (contact) — 搜索用户、查询部门
- 日历 (calendar) — 创建/查询/删除日程
- AI 表格 (aitable) — 查询/创建/更新记录
- AI 搜问 (aisearch) — 知识库问答
- 群聊与机器人 (chat) — 发送消息、管理群
- 待办 (todo) — 创建/查询待办任务
- 审批 (approval) — 提交/查询审批
- 考勤 (attendance) — 查询考勤记录
- 日志 (report) — 提交/查询日报周报
- DING 消息 (ding) — 发送 DING
- 钉钉文档 (document) — 读写文档
- 钉钉云盘 (drive) — 上传/下载文件
- AI 听记 (minutes) — 查询会议纪要
- 邮箱 (mail) — 收发邮件
- 在线电子表格 (axls) — 读写在线表格
- 知识库 (wiki) — 管理知识库

---

## 一、前置条件

> **一键体验**:如果你已安装 dws CLI,在 Reasonix 中启用 dws 插件即可自动完成认证检测和授权——无需手动执行任何命令。详见下方第二节。

### 1.1 安装 dws CLI

**macOS / Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/DingTalk-Real-AI/dingtalk-workspace-cli/main/scripts/install.sh | sh
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/DingTalk-Real-AI/dingtalk-workspace-cli/main/scripts/install.ps1 | iex
```

**npm:**

```bash
npm install -g dingtalk-workspace-cli
```

**验证安装:**

```bash
dws version
```

### 1.2 登录认证

dws 使用 OAuth 认证。有两种方式完成登录:

**方式一:自动授权(推荐)**

在 Reasonix 中启用 dws 插件后,插件会自动检测认证状态。如果未认证,会自动执行 `dws auth login` 打开浏览器授权页面——与 WorkBuddy 的体验一致:管理员审批后即可直接使用。

**方式二:手动授权**

```bash
dws auth login
```

浏览器会自动打开钉钉授权页面,扫码或点击授权后即完成登录。凭证会保存在本地,后续无需重复登录。

**验证认证状态:**

```bash
dws auth status --format json
```

> **管理员开启 CLI 访问**:如果你的组织尚未开启 CLI 访问权限,授权时会提示向管理员发送申请。管理员在[开发者平台](https://open-dev.dingtalk.com/) → 更多 → 基本信息 → CLI 访问管理 → 开启即可。

---

## 二、配置 Reasonix

### 2.1 GUI 配置

1. 启动 Reasonix 桌面端
2. 侧边栏 → 「设置」(齿轮图标)
3. 进入「办公插件」标签页
4. 找到「钉钉工作台 (dws)」卡片,点击「配置」展开
5. 按需填写:
   - **dws 可执行文件路径** — 可选,默认从 PATH 查找 `dws`;若安装路径不在 PATH 中可手动指定完整路径
6. 点击「保存并启用」
7. 插件启动后会自动调用 `dws_check` 工具检测安装和认证状态;如未认证会自动打开浏览器授权

### 2.2 TOML 配置

在 `reasonix.toml` 中添加:

```toml
[[plugins]]
name           = "dws"
command        = "reasonix-plugin-dws"
auto_start_tool = "dws_check"

# 可选:指定 dws 二进制路径(默认从 PATH 查找)
[plugins.env]
DWS_PATH = "/usr/local/bin/dws"
```

如果 dws 已在 PATH 中,无需配置 `DWS_PATH`:

```toml
[[plugins]]
name           = "dws"
command        = "reasonix-plugin-dws"
auto_start_tool = "dws_check"
```

> `auto_start_tool = "dws_check"` 使插件启动时自动检测 dws 安装和认证状态,未认证时自动打开浏览器授权。GUI 启用时此选项自动配置。

---

## 三、工具调用清单

| 工具 | 说明 | 只读 |
|------|------|------|
| `mcp__dws__dws_check` | 自动健康检查:检测 dws 安装和认证状态,未认证时自动打开浏览器授权(自动启动工具) | 是 |
| `mcp__dws__dws_call` | 执行 dws CLI 命令,操作钉钉产品 | 否 |
| `mcp__dws__dws_schema` | 发现可用产品和工具参数结构 | 是 |
| `mcp__dws__dws_auth` | 管理认证状态(status/login/logout) | 否 |

### 3.1 dws_call — 通用命令执行

执行任意 dws CLI 子命令,操作钉钉产品数据。

**参数:**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `command` | string | 是 | dws 子命令路径,如 `contact user search`、`calendar event list` |
| `args` | string[] | 否 | 额外标志和参数,如 `["--query", "张三", "--format", "json"]` |
| `timeout` | integer | 否 | 超时秒数(默认 30) |

**示例:**

```
# 搜索通讯录
mcp__dws__dws_call(command="contact user search", args=["--query", "张三", "--format", "json"])

# 查询明天日程
mcp__dws__dws_call(command="calendar event list", args=["--start", "2026-07-05", "--end", "2026-07-05", "--format", "json"])

# 创建待办
mcp__dws__dws_call(command="todo task create", args=["--title", "完成报告", "--format", "json", "--yes"])

# 预览操作(不实际执行)
mcp__dws__dws_call(command="approval instance create", args=["--template", "请假", "--dry-run", "--format", "json"])
```

> 建议始终添加 `--format json` 获取结构化输出。写操作建议先加 `--dry-run` 预览,确认后再加 `--yes` 执行。

### 3.2 dws_schema — 能力发现

查看 dws 支持的产品和工具参数结构,帮助 AI 了解可用的操作。

**参数:**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `tool` | string | 否 | 工具路径(如 `aitable.query_records`),省略则列出所有产品 |
| `jq` | string | 否 | jq 表达式,过滤 schema 输出 |

**示例:**

```
# 列出所有可用产品
mcp__dws__dws_schema()

# 查看 AI 表格工具的参数结构
mcp__dws__dws_schema(tool="aitable.query_records")

# 查看日历相关工具
mcp__dws__dws_schema(tool="calendar")
```

### 3.3 dws_auth — 认证管理

管理 dws 的 OAuth 认证状态。

**参数:**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `action` | string | 否 | 操作:`status`(默认)/`login`/`logout` |

**示例:**

```
# 检查认证状态
mcp__dws__dws_auth(action="status")

# 登录(会打开浏览器)
mcp__dws__dws_auth(action="login")

# 登出
mcp__dws__dws_auth(action="logout")
```

---

## 四、典型工作流

### 4.1 首次使用

配置了 `auto_start_tool = "dws_check"` 后,首次使用流程被极大简化:

```
# 1. 在 Reasonix 中启用 dws 插件
# → 插件自动调用 dws_check:
#   - 检测 dws 是否安装
#   - 如未安装,返回安装指引
#   - 如已安装未认证,自动打开浏览器进行 OAuth 授权
#   - 管理员审批后即可使用

# 2. (可选) 手动重新检查状态
mcp__dws__dws_check()

# 3. 发现可用产品
mcp__dws__dws_schema()

# 4. 查看具体工具参数
mcp__dws__dws_schema(tool="calendar.event_list")

# 5. 执行操作
mcp__dws__dws_call(command="calendar event list", args=["--format", "json"])
```

### 4.2 配合 IM 插件使用

用户在钉钉群 @机器人 "帮我查张三的手机号":

```
# IM 插件收到消息(AutoStartTool 已自动启动 Stream)
mcp__im__list_pending_commands(limit=10)
# → command_id="cmd-1", content="帮我查张三的手机号"

# AI 调用 dws 查询通讯录
mcp__dws__dws_call(command="contact user search", args=["--query", "张三", "--format", "json"])
# → { "mobile": "138xxxx1234", ... }

# 通过 IM 插件回推结果
mcp__im__mark_command_done(
  command_id="cmd-1",
  result="张三的手机号: 138xxxx1234"
)
```

---

## 五、环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `DWS_PATH` | dws 二进制文件路径 | `dws`(从 PATH 查找) |

---

## 六、常见问题

### Q1: dws_call 提示 `dws failed: exec: "dws": executable file not found`

**原因**: dws 未安装或不在 PATH 中。

**解决**:
1. 安装 dws CLI(见 1.1 节)
2. 或在配置中设置 `DWS_PATH` 指向 dws 的完整路径
3. 插件启用时会自动检测并提示安装方法

### Q2: dws_call 返回认证错误

**原因**: dws 未登录或 OAuth token 已过期。

**解决**: 插件启用时会自动检测认证状态并触发授权。也可以手动执行:
- `mcp__dws__dws_check()` — 自动检测并授权
- `mcp__dws__dws_auth(action="login")` — 手动触发授权

### Q3: 如何知道 dws 支持哪些操作?

**解决**: 调用 `mcp__dws__dws_schema()` 列出所有产品,或 `mcp__dws__dws_schema(tool="产品名")` 查看具体工具参数。

### Q4: 写操作(创建/删除)如何安全执行?

**建议**:
1. 先加 `--dry-run` 预览操作内容
2. 确认无误后加 `--yes` 实际执行
3. 始终加 `--format json` 获取结构化结果

### Q5: dws 命令超时怎么办?

**解决**: 增大 `timeout` 参数(默认 30 秒),如:

```
mcp__dws__dws_call(command="drive file download", args=["--file-id", "xxx"], timeout=120)
```

### Q6: Windows 上 dws 路径如何配置?

Windows 上 dws 安装后可能不在 PATH 中,需在配置中指定完整路径:

```toml
[plugins.env]
DWS_PATH = "C:\\Users\\YourName\\bin\\dws.exe"
```

或在 GUI 的「dws 可执行文件路径」中填入完整路径。

---

## 七、参考链接

- dws CLI 仓库: https://github.com/DingTalk-Real-AI/dingtalk-workspace-cli
- dws CLI 中文文档: https://github.com/DingTalk-Real-AI/dingtalk-workspace-cli/blob/main/README_zh.md
- 钉钉开放平台: https://open-dev.dingtalk.com/

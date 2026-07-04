# Reasonix IM 插件配置引导

## 概述

`reasonix-plugin-im` 是 Reasonix 的即时通讯集成插件,支持从企业微信(WeCom)、飞书(Feishu)、钉钉(DingTalk)接收远程指令,并把执行结果回推到对应的 IM 群或会话。

插件支持两种接入模式,可根据是否拥有公网 IP 自由选择:

| 模式 | 启动工具 | 公网 IP 要求 | 支持平台 | 推荐场景 |
|------|---------|-------------|---------|---------|
| **Webhook 模式** | `start_bot` | ✅ 需公网 IP 或内网穿透 | WeCom / Feishu / DingTalk | 公司服务器、有公网入口 |
| **Stream 长连接模式** | `start_stream` | ❌ 无需公网 IP | Feishu / DingTalk | 个人电脑、本地部署 |

**核心区别**:Webhook 模式需要在本地启动 HTTP 服务并暴露给公网,IM 平台回调 URL 主动推送消息;Stream 模式由插件主动连接到 IM 平台的 WebSocket 网关,所有流量走出站,无需任何入站端口。

---

## 一、环境变量完整清单

所有环境变量均为**可选**,可在 GUI「设置 → 办公插件 → IM 即时通讯」中配置,也可直接写入 `reasonix.toml` 的 `[[plugins]]` 段。

### 1.1 通用配置(Webhook 模式)

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `IM_BOT_PORT` | 本地 HTTP 监听端口 | `9876` |
| `IM_BOT_TOKEN` | Webhook 验证 Token(可空,填写后 IM 平台回调 URL 需带 `?token=<该值>`) | 空 |

### 1.2 Webhook 模式凭证(按需配置)

| 变量 | 适用平台 | 备注 |
|------|---------|------|
| `IM_WECOM_KEY` | 企业微信 | 群机器人 Webhook Key |
| `IM_FEISHU_KEY` | 飞书 | 群机器人 Webhook Key |
| `IM_DINGTALK_KEY` | 钉钉 | 群机器人 Access Token |
| `IM_DINGTALK_SECRET` | 钉钉 | 群机器人签名密钥(可选,启用 HMAC 校验) |

> ⚠️ 配置任一 `IM_*_KEY` 后,`start_bot` 工具调用时即使不传 `platforms` 参数,也会自动启用对应平台。

### 1.3 Stream 长连接模式凭证(钉钉 / 飞书)

| 变量 | 说明 |
|------|------|
| `IM_DINGTALK_APP_KEY` | 钉钉企业应用 AppKey(开发者后台获取) |
| `IM_DINGTALK_APP_SECRET` | 钉钉企业应用 AppSecret |
| `IM_FEISHU_APP_ID` | 飞书企业应用 App ID |
| `IM_FEISHU_APP_SECRET` | 飞书企业应用 App Secret |

> ⚠️ Stream 凭证与 Webhook 凭证**互斥**。同一平台只需配置一种模式。
> 配置 `IM_DINGTALK_APP_KEY` 或 `IM_FEISHU_APP_ID` 后,`start_stream` 工具会自动探测要启用的平台,无需显式传 `platforms`。

### 1.4 自动启动配置

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `auto_start_tool` | 插件初始化后自动调用的 MCP 工具名 | 空(不自动调用) |

> 配置 `auto_start_tool = "start_stream"` 或 `auto_start_tool = "start_bot"` 后,插件在会话启动时会自动建立连接,无需手动调用启动工具。GUI 启用 IM 插件时此选项自动设为 `start_stream`。

---

## 二、平台配置步骤

### 2.1 钉钉 Stream 模式(推荐,无需公网 IP)

#### 步骤 1:创建钉钉企业内部应用

1. 访问 [钉钉开放平台](https://open-dev.dingtalk.com/)
2. 进入「应用开发 → 企业内部开发 → 创建应用」
3. 填写应用名称(如 `Reasonix Bot`)、应用描述

#### 步骤 2:启用机器人能力

1. 应用详情页 → 「应用能力 → 添加应用能力 → 机器人」
2. 消息接收模式选择 **「Stream 模式」**(关键!不要选 HTTP 模式)
3. 配置机器人名称、回调字段(可任意,Stream 模式下不会被使用)

#### 步骤 3:发布应用并授权

1. 「版本管理与发布 → 创建版本 → 发布」
2. 「权限管理 → 申请权限」中授予:
   - `qyapi_send_group_message`(企业内群发消息)
   - `qyapi_chat_send`(单聊消息发送)
3. 「应用发布 → 授权范围」选择可见的部门/用户

#### 步骤 4:获取凭证并配置 Reasonix

在应用详情「基础信息 → 凭证与基础信息」处复制:
- AppKey → 填入 `IM_DINGTALK_APP_KEY`
- AppSecret → 填入 `IM_DINGTALK_APP_SECRET`

GUI 路径:**设置 → 办公插件 → IM 即时通讯 → 配置 → 钉钉企业应用 AppKey (Stream 模式)**

或写入 `reasonix.toml`:

```toml
[[plugins]]
name = "im"
command = "reasonix-plugin-im"
auto_start_tool = "start_stream"

[plugins.env]
IM_DINGTALK_APP_KEY = "dingxxxxxx"
IM_DINGTALK_APP_SECRET = "yyyyyyyy"
```

> `auto_start_tool = "start_stream"` 使插件在会话启动时自动建立 Stream 长连接,无需手动执行 `start_stream`。GUI 启用时此选项自动配置。

#### 步骤 5:验证

配置 `auto_start_tool` 后,插件会在会话启动时自动连接。检查日志( stderr) 应出现:

```
plugin: auto-start tool called server=im tool=start_stream
DingTalk stream started (client_id=dingxxxxxx), receiving messages via WebSocket — no public IP needed
```

也可手动验证:

```
mcp__im__start_stream()
```

在钉钉中 @机器人 发送消息,Reasonix 中执行 `list_pending_commands` 应能看到入队消息。

---

### 2.2 飞书 Stream 模式(推荐,无需公网 IP)

#### 步骤 1:创建飞书企业自建应用

1. 访问 [飞书开放平台](https://open.feishu.cn/app)
2. 「创建企业自建应用」
3. 填写应用名称、应用描述

#### 步骤 2:启用机器人能力与事件订阅

1. 应用详情 → 「添加应用能力 → 机器人」
2. 「事件与回调 → 事件配置 → 事件订阅方式」选择 **「长连接接收事件」**(关键!)
3. 订阅事件:`接收消息 v2.0`(`im.message.receive_v1`)

#### 步骤 3:配置权限

应用详情 → 「权限管理 → 开通权限」:
- `im:message`(读取与发送消息)
- `im:message:send_as_bot`(以机器人身份发送消息)
- `im:resource`(读取资源)

#### 步骤 4:发布应用并授权

「版本管理与发布 → 创建版本 → 申请发布」,管理员审核通过后,在「应用可用范围」中添加可见用户。

#### 步骤 5:获取凭证并配置 Reasonix

应用详情「凭证与基础信息」处复制:
- App ID → 填入 `IM_FEISHU_APP_ID`
- App Secret → 填入 `IM_FEISHU_APP_SECRET`

GUI 路径:**设置 → 办公插件 → IM 即时通讯 → 配置 → 飞书企业应用 App ID (Stream 模式)**

或写入 `reasonix.toml`:
```toml
[[plugins]]
name = "im"
command = "reasonix-plugin-im"
auto_start_tool = "start_stream"

[plugins.env]
IM_FEISHU_APP_ID = "cli_xxxxxx"
IM_FEISHU_APP_SECRET = "yyyyyyyy"
```

> `auto_start_tool = "start_stream"` 使插件在会话启动时自动建立 Stream 长连接,无需手动执行 `start_stream`。GUI 启用时此选项自动配置。

#### 步骤 6:验证

配置 `auto_start_tool` 后,插件会在会话启动时自动连接。检查日志( stderr) 应出现:

```
plugin: auto-start tool called server=im tool=start_stream
Feishu stream started (app_id=cli_xxxxxx), receiving messages via WebSocket — no public IP needed
```

也可手动验证:

```
mcp__im__start_stream()
```

---

### 2.3 钉钉 Webhook 模式(需公网 IP)

#### 步骤 1:创建钉钉自定义机器人

1. 进入目标钉钉群 → 「群设置 → 智能群助手 → 添加机器人 → 自定义」
2. 安全设置勾选「加签」,记录 Secret(填入 `IM_DINGTALK_SECRET`)
3. Webhook URL 中 `access_token=` 后的串填入 `IM_DINGTALK_KEY`

#### 步骤 2:配置回调入口(需公网可达)

将本地 `start_bot` 启动的 HTTP 服务暴露到公网,假设公网域名为 `https://example.com`:

```
https://example.com/im/dingtalk?token=<IM_BOT_TOKEN 的值>
```

> 没有公网 IP 时可用 [ngrok](https://ngrok.com/) / [frp](https://github.com/fatedier/frp) 临时穿透,但稳定性差,**推荐改用 Stream 模式**。

#### 步骤 3:在 Reasonix 中配置并启动

```toml
[[plugins]]
name = "im"
command = "reasonix-plugin-im"
auto_start_tool = "start_bot"

[plugins.env]
IM_BOT_PORT = "9876"
IM_BOT_TOKEN = "任意复杂字符串"
IM_DINGTALK_KEY = "access_token 串"
IM_DINGTALK_SECRET = "加签 Secret"
```

```
mcp__im__start_bot()
```

---

### 2.4 飞书 Webhook 模式(需公网 IP)

#### 步骤 1:创建飞书自定义机器人

目标群 → 「设置 → 群机器人 → 添加机器人 → 自定义机器人」,记录 Webhook 地址中 `open.feishu.cn/open-apis/bot/v2/hook/` 后的串,填入 `IM_FEISHU_KEY`。

#### 步骤 2:配置公网回调入口

```
https://example.com/im/feishu?token=<IM_BOT_TOKEN 的值>
```

#### 步骤 3:启动

```toml
[plugins.env]
IM_FEISHU_KEY = "飞书 Webhook Key"
IM_BOT_TOKEN = "任意复杂字符串"
```

```
mcp__im__start_bot()
```

> 飞书首次配置事件订阅时会发送 `challenge` 验证请求,插件已自动处理。

---

### 2.5 企业微信 Webhook 模式(需公网 IP)

企业微信目前**仅支持 Webhook 模式**,无法使用 Stream 长连接。

#### 步骤 1:创建企业微信群机器人

目标群 → 「群设置 → 添加群机器人 → 新创建一个机器人」,记录 Webhook URL 中 `webhook.send?key=` 后的串,填入 `IM_WECOM_KEY`。

#### 步骤 2:配置公网回调入口

```
https://example.com/im/wecom?token=<IM_BOT_TOKEN 的值>
```

#### 步骤 3:启动

```toml
[plugins.env]
IM_WECOM_KEY = "企微 Webhook Key"
IM_BOT_TOKEN = "任意复杂字符串"
```

```
mcp__im__start_bot()
```

---

## 三、GUI 配置流程

1. 启动 Reasonix 桌面端
2. 侧边栏 → 「设置」(齿轮图标)
3. 进入「办公插件」标签页
4. 找到「IM 即时通讯」卡片,点击「配置」展开
5. 根据所选模式填写对应字段:
   - **Stream 模式**:仅填 4 个 Stream 凭证字段(AppKey/AppSecret 或 App ID/App Secret)
   - **Webhook 模式**:填 `IM_BOT_PORT`、`IM_BOT_TOKEN`(可选)和对应平台的 `IM_*_KEY` / `IM_DINGTALK_SECRET`
6. 点击「保存并启用」
7. 卡片右上角的开关会自动切换到「已连接」状态
8. 启动模式:
   - Stream 模式:在 Reasonix 对话中执行 `mcp__im__start_stream()`
   - Webhook 模式:执行 `mcp__im__start_bot()`

---

## 四、工具调用清单

| 工具 | 说明 |
|------|------|
| `mcp__im__start_bot` | 启动 Webhook HTTP 服务器(需公网 IP) |
| `mcp__im__stop_bot` | 停止 Webhook 服务器 |
| `mcp__im__start_stream` | 启动 Stream 长连接(钉钉/飞书,无需公网 IP) |
| `mcp__im__stop_stream` | 停止 Stream 长连接 |
| `mcp__im__list_pending_commands` | 列出待处理的远程指令 |
| `mcp__im__mark_command_done` | 标记指令已执行,并把结果回推到原平台 |
| `mcp__im__send_message` | 主动向指定 IM 平台 Webhook 推送消息 |

### 典型工作流

配置 `auto_start_tool` 后,Stream/Bot 会在会话启动时自动运行,无需手动执行第 1 步。

```
# 1. 启动接入(配置 auto_start_tool 后可跳过)
mcp__im__start_stream()            # Stream 模式
mcp__im__start_bot(port=9876)      # Webhook 模式

# 2. 轮询待处理指令
mcp__im__list_pending_commands(limit=10)

# 3. 执行业务逻辑后回推结果
mcp__im__mark_command_done(
  command_id="cmd-1-abc123",
  result="✅ 已为您完成日程查询:\n- 明天 09:00 项目周会\n- 14:00 客户拜访"
)

# 4. 关闭
mcp__im__stop_stream()
mcp__im__stop_bot()
```

---

## 五、常见问题

### Q1:Stream 模式启动失败,提示 `app_id and app_secret are required`

**原因**:未配置 Stream 凭证环境变量。

**解决**:在 GUI 配置面板填入 4 个 Stream 字段中的对应两项,或在 `reasonix.toml` 中配置 `IM_DINGTALK_APP_KEY` / `IM_FEISHU_APP_ID` 等。

### Q2:钉钉机器人收不到消息

排查清单:
1. 钉钉开放平台 → 应用能力 → 机器人 → 消息接收模式必须为 **「Stream 模式」**
2. 应用已发布并通过审核
3. 应用可见范围包含当前用户
4. 机器人被添加到了对应群中(群设置 → 智能群助手)
5. Reasonix 日志(stderr)有 `DingTalk stream: queued msg` 记录 → 表示已收到,问题在 `mark_command_done` 回推环节

### Q3:飞书机器人能收到消息但回复失败,提示 `Feishu reply failed: code=99991663`

**原因**:飞书应用未开通 `im:message:send_as_bot` 权限,或应用未发布。

**解决**:在飞书开放平台 → 权限管理中重新申请权限,然后重新发布版本并通知管理员审批。

### Q4:Webhook 模式下 IM 平台提示「连接超时」

**原因**:本地 HTTP 服务未暴露到公网,或公网域名/端口配置错误。

**解决**:
- 检查 `start_bot` 是否已成功启动(执行 `list_pending_commands` 不报错)
- 用 curl 从公网测试回调 URL 是否可达:`curl https://example.com/im/health` 应返回 `ok`
- 没有公网 IP 时改用 Stream 模式

### Q5:企业微信为何不支持 Stream 模式?

截至 2026 年,企业微信开放平台**仅提供 HTTP Webhook 回调**,未开放长连接 SDK。这是平台能力限制,非插件实现问题。如需在无公网 IP 环境下使用企业微信,建议:
- 改用 ngrok / frp 内网穿透
- 或迁移到钉钉/飞书(均支持 Stream 模式)

### Q6:能否同时启用 Webhook 和 Stream 两种模式?

可以,但**同一平台不要重复配置**。常见组合:
- 钉钉 + 飞书走 Stream 模式,企业微信走 Webhook 模式
- 配置:`IM_DINGTALK_APP_KEY` / `IM_FEISHU_APP_ID` / `IM_WECOM_KEY` 同时存在
- 启动:`start_stream()` + `start_bot(platforms=["wecom"])` 同时调用

### Q7:Stream 模式断线后会自动重连吗?

会。钉钉 SDK 和飞书 SDK 默认开启了 `AutoReconnect`,断线后会自动重连,无需手动干预。日志中会出现 `StreamClient reconnect success` 记录。

---

## 六、安全建议

1. **`IM_BOT_TOKEN` 必填**:Webhook 模式下,在回调 URL 后附加 `?token=<该值>` 可防止伪造请求
2. **凭证定期轮换**:AppSecret / Webhook Key 泄露后应立即在开放平台重置,并在 Reasonix 中更新配置
3. **不要把凭证提交到 Git**:`reasonix.toml` 含敏感信息时应加入 `.gitignore`,或用 `~/.reasonix/config.toml`(用户级配置,优先级高于项目级)
4. **应用可见范围最小化**:钉钉/飞书应用发布时,仅授权必要部门/用户,避免全员可用

---

## 七、参考链接

- 钉钉 Stream 模式官方文档:https://open.dingtalk.com/document/development/stream
- 钉钉 Stream Go SDK:https://github.com/open-dingtalk/dingtalk-stream-sdk-go
- 飞书长连接官方文档:https://open.feishu.cn/document/server-side-sdk/java-sdk-guide/handle-events
- 飞书 Go SDK:https://github.com/larksuite/oapi-sdk-go
- 企业微信群机器人:https://developer.work.weixin.qq.com/document/path/91770

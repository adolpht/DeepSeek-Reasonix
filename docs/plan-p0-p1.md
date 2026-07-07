# Rexion P0/P1 改造详细计划

> 版本：v1.0 | 日期：2026-06-29 | 状态：规划中

---

## 目录

- [一、项目现状总结](#一项目现状总结)
- [二、P0 改造计划（必须完成）](#二p0-改造计划必须完成)
  - [P0-1: Linux 内核级沙盒](#p0-1-linux-内核级沙盒)
  - [P0-2: Windows 沙盒](#p0-2-windows-沙盒)
  - [P0-3: 无头执行模式增强](#p0-3-无头执行模式增强)
  - [P0-4: 结构化 apply_patch 工具](#p0-4-结构化-apply_patch-工具)
  - [P0-5: 三级沙盒模式](#p0-5-三级沙盒模式)
- [三、P1 改造计划（重要）](#三p1-改造计划重要)
  - [P1-1: SQLite 持久化线程](#p1-1-sqlite-持久化线程)
  - [P1-2: 多 Agent 并行编排](#p1-2-多-agent-并行编排)
  - [P1-3: MCP 服务端模式](#p1-3-mcp-服务端模式)
  - [P1-4: REPL 工具](#p1-4-repl-工具)
  - [P1-5: tool_search 工具](#p1-5-tool_search-工具)
- [四、依赖关系与执行顺序](#四依赖关系与执行顺序)
- [五、通用改造原则](#五通用改造原则)

---

## 一、项目现状总结

### 已有能力

| 能力 | 实现位置 | 状态 |
|------|----------|------|
| macOS Seatbelt 沙盒 | `internal/sandbox/seatbelt_darwin.go` | ✅ 完成 |
| 权限系统 (deny > allow > ask, 默认 ask) | `internal/permission/permission.go` | ✅ 完成 |
| 文件写入沙盒 (confine) | `internal/tool/builtin/confine.go` | ✅ 完成 |
| 无头执行 (Rexion run) | `internal/cli/cli.go:runAgent()` | ✅ 基础完成 |
| 文件编辑 (search/replace) | `internal/tool/builtin/editfile.go` | ✅ 完成 |
| 批量编辑 (原子 multi_edit) | `internal/tool/builtin/multiedit.go` | ✅ 完成 |
| 子 Agent (TaskTool, 单次 API 调用) | `internal/agent/task.go` | ✅ 基础完成 |
| MCP 客户端 (stdio + HTTP) | `internal/plugin/` | ✅ 完成 |
| 会话持久化 (JSONL) | `internal/agent/save.go` | ✅ 完成 |
| 会话分支 | `internal/agent/branch.go` | ✅ 完成 |
| 上下文压缩 | `internal/agent/compact.go` | ✅ 完成 |
| 并行工具执行 (只读工具并行, 最大8) | `internal/agent/agent.go:executeBatch()` | ✅ 完成 |

### 核心差距

| 差距 | 影响 |
|------|------|
| Linux/Windows 无沙盒 | 在非 macOS 平台上命令完全不受限 |
| 无头模式缺乏 CI/CD 语义 | 无法可靠集成到自动化流水线 |
| 编辑工具仅支持字符串匹配 | 大规模重构时补丁可靠性不足 |
| 子 Agent 是单次 API 调用 | 无法执行真正的多步自主任务 |
| 无 MCP 服务端 | 无法被其他 Agent 调用 |
| 会话存储仅 JSONL | 缺乏并发安全、结构化查询能力 |

---

## 二、P0 改造计划（必须完成）

---

### P0-1: Linux 内核级沙盒

**目标**：在 Linux 上实现与 macOS Seatbelt 等价的内核级沙盒隔离，默认网络关闭、文件系统受限。

**当前状态**：`seatbelt_other.go` 中 `Apply()` 为 no-op，Linux 上命令无任何隔离。

#### 架构设计

```
                    ┌─────────────────────────┐
                    │     sandbox.Policy       │  ← 泛化接口
                    │  (不再绑定 Seatbelt)      │
                    └──────────┬──────────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
    ┌─────────▼──────┐ ┌──────▼─────────┐ ┌────▼───────────┐
    │  DarwinPolicy   │ │  LinuxPolicy   │ │ WindowsPolicy  │
    │  (Seatbelt)     │ │ (bwrap+seccomp)│ │ (JobObject)    │
    └────────────────┘ └────────────────┘ └────────────────┘
```

#### 接口重构

**当前接口**（紧耦合 macOS）：
```go
type Policy interface {
    SeatbeltProfile() string  // macOS 专用
    FullName() string
}
```

**新接口**（平台无关）：
```go
type Policy interface {
    Name() string
    // Apply 修改 exec.Cmd 使其受到沙盒约束
    Apply(cmd *exec.Cmd) (*exec.Cmd, error)
    // Platform 返回该 Policy 适用的平台
    Platform() string // "darwin", "linux", "windows"
}

// SeatbeltPolicy 嵌入原 Policy，仅 darwin 构建
type SeatbeltPolicy struct { ... }

// BwrapPolicy 仅 linux 构建
type BwrapPolicy struct { ... }
```

#### Linux 沙盒实现方案

**方案：bubblewrap (bwrap) + seccomp**

| 层 | 机制 | 作用 |
|----|------|------|
| 文件系统 | bubblewrap `--bind-ro` / `--bind` / `--dev` / `--proc` | 只读挂载系统目录，读写挂载工作目录 |
| 网络 | bubblewrap `--unshare-net` | 完全禁止网络访问 |
| 进程 | bubblewrap `--die-with-parent` | 父进程退出时自动终止子进程 |
| 系统调用 | seccomp BPF filter | 限制危险系统调用 (mount, ptrace, keyctl 等) |
| 用户命名空间 | bubblewrap `--unshare-user` | 可选：UID 映射隔离 |

**三级策略对应的 bwrap 参数**：

```
Read-Only (suggest):
  bwrap --ro-bind / / --dev /dev --proc /proc \
        --unshare-net --die-with-parent --

Workspace-Write (默认):
  bwrap --ro-bind / / --bind {workdir} {workdir} \
        --dev /dev --proc /proc \
        --unshare-net --die-with-parent --

Full-Access:
  无沙盒，直接执行
```

#### 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/sandbox/sandbox.go` | 修改 | 重构 Policy 接口，新增 `Apply()` 和 `Name()` |
| `internal/sandbox/seatbelt_darwin.go` | 修改 | SeatbeltPolicy 适配新接口 |
| `internal/sandbox/seatbelt_other.go` | 删除 | 不再需要 no-op 降级 |
| `internal/sandbox/bwrap_linux.go` | 新增 | bubblewrap 沙盒实现 |
| `internal/sandbox/seccomp_linux.go` | 新增 | seccomp BPF 过滤器 |
| `internal/sandbox/policy.go` | 新增 | 平台无关的策略工厂 `NewPolicy(mode, workdir)` |
| `internal/sandbox/bwrap_test.go` | 新增 | Linux 沙盒测试 |
| `internal/config/config.go` | 修改 | 新增 seccomp 配置项 |

#### 配置扩展

```toml
[sandbox]
enabled = true
mode = "workspace-write"   # "read-only" | "workspace-write" | "full-access"

# Linux 专用
[sandbox.linux]
backend = "bwrap"           # "bwrap" | "landlock" | "none"
bwrap_path = "/usr/bin/bwrap"  # 自动检测或手动指定
seccomp = true              # 是否启用 seccomp 过滤器
unshare_user = false        # 是否启用用户命名空间
```

#### bwrap 可用性检测

```
启动时检测流程：
1. 查找 bwrap 二进制（配置路径 → $PATH → 常见路径）
2. 执行 `bwrap --version` 验证可用
3. 若不可用，降级到应用层 confine（文件写入检查）
4. 发出 Notice 告警用户当前无内核级沙盒
```

#### 验收标准

- [ ] Linux 上 bash 工具默认运行在 bwrap 沙盒中
- [ ] workspace-write 模式下工作目录可写，其他路径只读
- [ ] read-only 模式下所有文件系统操作只读
- [ ] 默认网络隔离（无法 curl/wget）
- [ ] bwrap 不可用时优雅降级 + 告警
- [ ] seccomp 阻止危险系统调用
- [ ] 现有 macOS Seatbelt 功能不受影响

---

### P0-2: Windows 沙盒

**目标**：在 Windows 上实现基于 Job Object + 限制令牌的沙盒隔离。

**当前状态**：与 Linux 相同，`Apply()` 为 no-op。

#### 实现方案

| 层 | 机制 | 作用 |
|----|------|------|
| 进程组 | Windows Job Object | 进程树管理，资源限制 |
| 文件系统 | 限制令牌 (Restricted Token) | 移除写权限 SID，仅保留工作目录写权限 |
| 网络 | Windows Firewall 规则 | 出站连接阻断（可选） |
| 注册表 | 限制令牌 | 阻止注册表写入 |

#### 关键实现

```go
// internal/sandbox/jobobject_windows.go

type JobObjectPolicy struct {
    mode    string  // "read-only" | "workspace-write" | "full-access"
    workDir string
}

func (p *JobObjectPolicy) Apply(cmd *exec.Cmd) (*exec.Cmd, error) {
    switch p.mode {
    case "read-only":
        // 创建限制令牌：移除所有写权限 SID
        // 将工作目录设为只读 ACL
        token := createRestrictedToken(true) // readOnly=true
        cmd.SysProcAttr = &syscall.SysProcAttr{
            Token: token,
        }
    case "workspace-write":
        // 创建限制令牌：移除非工作目录的写权限
        token := createRestrictedToken(false) // readOnly=false
        applyDirectoryACL(p.workDir, true)    // 工作目录可写
        cmd.SysProcAttr = &syscall.SysProcAttr{
            Token: token,
        }
    case "full-access":
        // 不做任何限制
    }
    // 创建 Job Object 限制进程树
    job := createJobObject()
    assignProcessToJob(job, cmd.Process)
    return cmd, nil
}
```

#### 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/sandbox/jobobject_windows.go` | 新增 | Windows Job Object + 限制令牌实现 |
| `internal/sandbox/acl_windows.go` | 新增 | Windows ACL 操作辅助函数 |
| `internal/sandbox/jobobject_test.go` | 新增 | Windows 沙盒测试 |
| `internal/config/config.go` | 修改 | 新增 Windows 沙盒配置项 |

#### 配置扩展

```toml
[sandbox.windows]
backend = "jobobject"  # "jobobject" | "none"
restrict_network = true # 是否通过防火墙规则阻断出站
```

#### 验收标准

- [ ] Windows 上 bash 工具运行在 Job Object 沙盒中
- [ ] workspace-write 模式下仅工作目录可写
- [ ] read-only 模式下所有文件操作只读
- [ ] 子进程随 Job Object 终止而终止
- [ ] 限制令牌正确移除不需要的权限

---

### P0-3: 无头执行模式增强

**目标**：使 `Rexion run` 成为 CI/CD 就绪的自动化执行引擎，具备明确的退出码、结构化输出、超时重试等能力。

**当前状态**：`runAgent()` 已有基础实现，但缺乏 CI/CD 所需的语义化退出码、结构化输出、差异汇总等。

#### 退出码语义

| 退出码 | 含义 | CI/CD 解读 |
|--------|------|------------|
| 0 | 任务成功完成 | 通过 |
| 1 | 任务执行失败（Agent 报错） | 失败 |
| 2 | 权限被拒绝（需要审批但处于非交互模式） | 需人工介入 |
| 3 | 超时 | 需增加超时时间 |
| 4 | 上下文溢出（压缩后仍超限） | 需更大的上下文窗口 |
| 5 | 模型 API 错误（限流/鉴权/不可用） | 基础设施问题 |

#### 结构化输出

新增 `--format json` 标志，输出结构化结果：

```json
{
  "status": "success",
  "exit_code": 0,
  "summary": "Fixed the authentication bug in src/auth/login.go",
  "files_modified": ["src/auth/login.go", "src/auth/login_test.go"],
  "diff": "diff --git a/src/auth/login.go\n...",
  "tool_calls": 12,
  "duration_ms": 45230,
  "usage": {
    "prompt_tokens": 15234,
    "completion_tokens": 3421,
    "cache_hit_tokens": 12000,
    "cost_usd": 0.034
  },
  "steps": [
    {"tool": "read_file", "file": "src/auth/login.go", "status": "ok"},
    {"tool": "edit_file", "file": "src/auth/login.go", "status": "ok"},
    {"tool": "bash", "command": "go test ./src/auth/...", "status": "ok"}
  ]
}
```

#### 新增 CLI 标志

```
Rexion run [flags] <prompt>

新增标志：
  --format <text|json>         输出格式（默认 text）
  --timeout <duration>         总执行超时（默认 10m）
  --approval-mode <auto|ask>   审批模式（默认 auto，ask 遇到审批需求时退出码 2）
  --max-retries <n>            API 错误最大重试次数（默认 3）
  --diff                       成功后输出 git diff
  --quiet                      仅输出最终结果，不输出中间过程
```

#### 执行流程改造

```
当前流程：
  setup() → ctrl.Run(ctx, prompt) → return 0/1

改造后流程：
  setup() → ctrl.Run(ctx, prompt)
    ↓
  判断 Run 返回的错误类型：
    ├── nil              → exitCode=0
    ├── PermissionErr    → exitCode=2
    ├── TimeoutErr       → exitCode=3
    ├── ContextOverflow  → exitCode=4
    ├── APIErr           → exitCode=5
    └── 其他             → exitCode=1
    ↓
  收集结果数据（通过 event sink）
    ├── 文件变更列表（来自 write_file/edit_file 的 ToolResult）
    ├── diff 汇总（通过 git diff 或文件对比）
    ├── usage 统计（来自 Usage 事件）
    └── 工具调用记录（来自 ToolDispatch + ToolResult 事件）
    ↓
  输出结果
    ├── --format text: 原有文本输出 + 退出码
    └── --format json: JSON 结构化输出
```

#### 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/cli/cli.go` | 修改 | 重构 `runAgent()`，新增标志、退出码、结构化输出 |
| `internal/agent/errors.go` | 新增 | 语义化错误类型（PermissionErr, TimeoutErr 等） |
| `internal/cli/result.go` | 新增 | 结构化结果收集器（从 event sink 汇总数据） |
| `internal/cli/diff.go` | 新增 | 执行后 diff 收集逻辑 |
| `internal/control/controller.go` | 修改 | `Run()` 返回语义化错误 |

#### 验收标准

- [ ] `Rexion run` 返回正确的语义化退出码
- [ ] `--format json` 输出完整结构化结果
- [ ] `--timeout` 超时后优雅终止（发送中断、等待清理）
- [ ] `--approval-mode ask` 遇到审批需求时退出码 2
- [ ] `--diff` 输出完整的文件变更差异
- [ ] CI/CD 管道中可通过退出码判断任务状态

---

### P0-4: 结构化 apply_patch 工具

**目标**：新增基于统一 diff 格式的 `apply_patch` 工具，支持上下文行匹配和模糊匹配，提升大规模代码编辑的可靠性。

**当前状态**：`edit_file` 使用精确字符串匹配（`strings.Count` 必须 == 1），`multi_edit` 是原子化的多步 edit_file。

#### 为什么需要 apply_patch

| 场景 | edit_file | apply_patch |
|------|-----------|-------------|
| 单行替换 | ✅ 简单可靠 | ✅ 同样可靠 |
| 多文件编辑 | ❌ 需多次调用 | ✅ 一次调用多文件 |
| 行号偏移 | ❌ old_string 必须精确匹配 | ✅ 上下文行模糊匹配 |
| 大范围重构 | ❌ 易出错 | ✅ 统一 diff 格式更健壮 |
| 新建文件 | ❌ 需要 write_file | ✅ diff 中标记新文件 |
| 删除文件/行 | ❌ 需要 delete_range | ✅ diff 中标记删除 |

#### 统一 diff 格式定义

```
--- a/path/to/file.go
+++ b/path/to/file.go
@@ -10,7 +10,7 @@ func existingFunc() {
     existingLine1
     existingLine2
-    oldLine
+    newLine
     existingLine3
     existingLine4
```

工具参数：
```json
{
  "name": "apply_patch",
  "parameters": {
    "type": "object",
    "properties": {
      "patch": {
        "type": "string",
        "description": "统一 diff 格式的补丁，可包含多个文件的修改"
      },
      "fuzzy_match": {
        "type": "boolean",
        "description": "是否启用模糊匹配（允许空白差异），默认 true"
      }
    },
    "required": ["patch"]
  }
}
```

#### 解析器设计

```
输入: patch 字符串
  │
  ├─ 1. 解析为 PatchSet（多个 FilePatch）
  │     ├── 每个文件: old_path, new_path, hunks[]
  │     └── 每个 hunk: old_start, old_count, new_start, new_count, lines[]
  │
  ├─ 2. 按文件分组，逐文件应用
  │     ├── 读取文件内容（按行分割）
  │     ├── 对每个 hunk，在目标行附近搜索匹配
  │     │   ├── 精确匹配：上下文行 + 删除行完全一致
  │     │   └── 模糊匹配：忽略尾部空白、tab/space 差异
  │     ├── 匹配成功：执行替换（删除 - 行，插入 + 行）
  │     └── 匹配失败：记录错误，跳过该 hunk
  │
  ├─ 3. 原子性保证
  │     ├── 所有 hunk 解析成功后才写入
  │     ├── 写入前创建检查点快照
  │     └── 写入失败时回滚
  │
  └─ 4. 返回结果
        ├── 成功: "Applied patch to N files (M hunks)"
        └── 部分失败: "Applied N/M hunks. Failed: ..."
```

#### 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/tool/builtin/applypatch.go` | 新增 | apply_patch 工具实现 |
| `internal/diff/parser.go` | 新增 | 统一 diff 解析器 |
| `internal/diff/apply.go` | 新增 | 补丁应用逻辑（精确+模糊匹配） |
| `internal/diff/parser_test.go` | 新增 | 解析器测试 |
| `internal/diff/apply_test.go` | 新增 | 应用逻辑测试 |
| `internal/tool/builtin/init.go` 或注册点 | 修改 | 注册新工具 |

#### 与现有工具的关系

- `edit_file` / `multi_edit`：**保留不删除**，适用于精确的小范围编辑
- `apply_patch`：适用于大范围重构和多文件同时编辑
- 系统提示中引导模型：小修改用 edit_file，大重构用 apply_patch

#### 验收标准

- [ ] 正确解析标准统一 diff 格式
- [ ] 支持单文件多 hunk 应用
- [ ] 支持多文件一次补丁
- [ ] 模糊匹配容忍空白差异
- [ ] 匹配失败时明确报错（行号、原因）
- [ ] 原子性：全部 hunk 成功才写入
- [ ] 新建文件和删除文件支持
- [ ] 与 checkpoint 系统集成（写入前快照）

---

### P0-5: 三级沙盒模式

**目标**：实现 read-only / workspace-write / full-access 三级沙盒模式，每一级对应不同的 OS 级强制隔离策略。

**当前状态**：有三种 Policy（NetworkOnly/FileOnly/None），但概念模型与 Codex 不对齐，且非 macOS 上无实际效果。

#### 模式定义

| 模式 | 文件系统 | 网络 | 进程 | 适用场景 |
|------|----------|------|------|----------|
| **read-only** (suggest) | 全部只读 | 禁止 | 受限 | 代码审查、建议 |
| **workspace-write** (默认) | 工作目录可写，其他只读 | 禁止 | 受限 | 日常编程 |
| **full-access** (danger) | 无限制 | 允许 | 无限制 | 信任环境 |

#### 权限矩阵

```
                    read-only    workspace-write    full-access
                    ─────────    ──────────────    ───────────
read_file           ✅           ✅                 ✅
glob/grep/ls        ✅           ✅                 ✅
web_fetch           ❌           ❌                  ✅
edit_file           ❌           ✅                  ✅
write_file          ❌           ✅                  ✅
bash (只读命令)      ✅           ✅                  ✅
bash (写命令)        ❌           ✅                  ✅
bash (网络命令)      ❌           ❌                  ✅
task (子Agent)      ✅(只读)     ✅                  ✅
MCP 工具            按工具声明    按工具声明           ✅
```

#### 实现方案

**1. 配置层**

```toml
[sandbox]
mode = "workspace-write"  # "read-only" | "workspace-write" | "full-access"
```

**2. 策略工厂**

```go
// internal/sandbox/policy.go

func NewPolicy(mode string, workDir string) Policy {
    switch mode {
    case "read-only":
        return newReadOnlyPolicy(workDir)
    case "workspace-write":
        return newWorkspaceWritePolicy(workDir)
    case "full-access":
        return newFullAccessPolicy()
    default:
        return newWorkspaceWritePolicy(workDir) // 安全默认
    }
}
```

**3. 工具层拦截**

```go
// 在 executeBatch 之前，根据沙盒模式过滤工具调用

func (a *Agent) enforceSandboxMode(calls []provider.ToolCall) []provider.ToolCall {
    switch a.sandboxMode {
    case "read-only":
        // 过滤掉所有非 ReadOnly 工具调用
        // 替换为错误消息："当前为 read-only 模式，无法执行写操作"
    case "workspace-write":
        // 过滤掉需要网络的操作
        // bash 命令检查：包含网络操作则拒绝
    case "full-access":
        // 不过滤
    }
}
```

**4. bash 命令网络检测**

```go
// internal/permission/bash_network.go

func BashRequiresNetwork(command string) bool {
    // 检测网络相关命令：curl, wget, ssh, scp, rsync, ping, dig, nslookup 等
    // 检测端口转发、代理设置等
    networkCommands := []string{"curl", "wget", "ssh", "scp", "rsync",
                                "ping", "dig", "nslookup", "nc", "telnet"}
    // 解析管道和子命令
}
```

#### CLI 标志

```
Rexion chat --sandbox read-only     # 建议模式
Rexion chat                         # 默认 workspace-write
Rexion chat --sandbox full-access   # 危险模式（需确认）
Rexion run  --sandbox workspace-write "fix the bug"
```

#### 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/sandbox/policy.go` | 新增 | 统一策略工厂 + 三模式定义 |
| `internal/sandbox/mode.go` | 新增 | 沙盒模式常量 + 工具过滤逻辑 |
| `internal/permission/bash_network.go` | 新增 | bash 网络命令检测 |
| `internal/agent/agent.go` | 修改 | `executeBatch` 前加入模式拦截 |
| `internal/cli/cli.go` | 修改 | 新增 `--sandbox` 标志 |
| `internal/config/config.go` | 修改 | 统一沙盒配置结构 |
| `internal/boot/boot.go` | 修改 | 传递沙盒模式到 Agent |

#### 验收标准

- [ ] 三种模式可通过配置或 CLI 标志选择
- [ ] read-only 模式下所有写操作被 OS 级 + 应用级双重阻断
- [ ] workspace-write 模式下工作目录可写、网络阻断
- [ ] full-access 模式下不施加任何限制
- [ ] 运行时切换模式需用户确认
- [ ] 沙盒模式在事件流中可见（Notice）

---

## 三、P1 改造计划（重要）

---

### P1-1: SQLite 持久化线程

**目标**：引入 SQLite 作为会话存储后端，支持跨进程并发安全访问、结构化查询、快速随机访问。

**当前状态**：JSONL 文件存储，全量重写保存，无并发安全保证。

#### 数据模型

```sql
-- 线程（会话）
CREATE TABLE threads (
    id          TEXT PRIMARY KEY,           -- UUID
    title       TEXT,                       -- 首条用户消息摘要
    model       TEXT NOT NULL,              -- 使用的模型
    workspace   TEXT,                       -- 工作目录
    scope       TEXT DEFAULT 'project',     -- 'project' | 'global'
    parent_id   TEXT,                       -- 分支来源线程
    fork_turn   INTEGER,                    -- 分支点 turn 编号
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    FOREIGN KEY (parent_id) REFERENCES threads(id)
);

-- Turn（一轮对话）
CREATE TABLE turns (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    thread_id   TEXT NOT NULL,
    turn_num    INTEGER NOT NULL,           -- 在线程中的序号
    created_at  DATETIME NOT NULL,
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE,
    UNIQUE(thread_id, turn_num)
);

-- Item（turn 内的原子事件）
CREATE TABLE items (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_id     INTEGER NOT NULL,
    item_order  INTEGER NOT NULL,           -- turn 内的顺序
    kind        TEXT NOT NULL,              -- 'user' | 'assistant' | 'tool_call' | 'tool_result' | 'system'
    content     TEXT,                       -- 文本内容
    tool_name   TEXT,                       -- 工具名（tool_call / tool_result）
    tool_args   TEXT,                       -- JSON 参数（tool_call）
    tool_output TEXT,                       -- 工具输出（tool_result）
    tool_error  TEXT,                       -- 错误信息
    reasoning   TEXT,                       -- 推理内容
    duration_ms INTEGER,                    -- 执行耗时
    created_at  DATETIME NOT NULL,
    FOREIGN KEY (turn_id) REFERENCES turns(id) ON DELETE CASCADE
);

-- 压缩存档
CREATE TABLE compaction_archives (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    thread_id   TEXT NOT NULL,
    turn_start  INTEGER NOT NULL,
    turn_end    INTEGER NOT NULL,
    summary     TEXT NOT NULL,              -- 压缩摘要
    archive_data TEXT NOT NULL,             -- 原始消息 JSON（完整保留）
    created_at  DATETIME NOT NULL,
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE
);

-- 索引
CREATE INDEX idx_threads_workspace ON threads(workspace);
CREATE INDEX idx_threads_updated ON threads(updated_at DESC);
CREATE INDEX idx_items_turn ON items(turn_id);
CREATE INDEX idx_items_kind ON items(kind);
```

#### 存储抽象层

```go
// internal/agent/store.go

type Store interface {
    // 线程操作
    CreateThread(ctx context.Context, t *Thread) error
    GetThread(ctx context.Context, id string) (*Thread, error)
    ListThreads(ctx context.Context, opts ListOpts) ([]*Thread, error)
    UpdateThread(ctx context.Context, t *Thread) error
    DeleteThread(ctx context.Context, id string) error

    // 消息操作
    AppendMessages(ctx context.Context, threadID string, msgs []provider.Message) error
    GetMessages(ctx context.Context, threadID string) ([]provider.Message, error)
    ReplaceMessages(ctx context.Context, threadID string, msgs []provider.Message) error

    // 压缩存档
    SaveArchive(ctx context.Context, a *Archive) error
    LoadArchive(ctx context.Context, threadID string, fromTurn int) (*Archive, error)

    // 迁移
    Close() error
}
```

#### 双后端策略

- **SQLiteStore**：新默认后端，支持并发安全
- **JSONLStore**：旧后端保留，用于迁移和导出

```go
func NewStore(cfg StoreConfig) (Store, error) {
    switch cfg.Backend {
    case "sqlite":
        return NewSQLiteStore(cfg.Path)
    case "jsonl":
        return NewJSONLStore(cfg.Dir)
    default:
        // 自动检测：已有 .jsonl 文件用 JSONL，否则用 SQLite
        return autoDetectStore(cfg)
    }
}
```

#### 迁移方案

```
启动时检测：
1. 若 sessions/ 目录仅有 .jsonl 文件 → 自动迁移到 SQLite
2. 迁移过程：逐文件解析 JSONL → 写入 SQLite → 原文件加 .migrated 后缀
3. 迁移完成后 SQLite 成为默认后端
4. 提供 Rexion session export --format jsonl 导出功能
```

#### 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/agent/store.go` | 新增 | Store 接口定义 |
| `internal/agent/sqlite_store.go` | 新增 | SQLite 后端实现 |
| `internal/agent/jsonl_store.go` | 新增 | JSONL 后端适配（包装现有逻辑） |
| `internal/agent/migrate_sqlite.go` | 新增 | JSONL → SQLite 迁移工具 |
| `internal/agent/save.go` | 修改 | 适配 Store 接口 |
| `internal/agent/session.go` | 修改 | 使用 Store 替代直接文件操作 |
| `internal/agent/branch.go` | 修改 | 分支元数据存入 SQLite |
| `internal/config/config.go` | 修改 | 新增 store 配置 |
| `go.mod` | 修改 | 新增 `github.com/mattn/go-sqlite3` 依赖 |

#### 配置扩展

```toml
[store]
backend = "sqlite"       # "sqlite" | "jsonl"
path = ""                # SQLite 数据库路径（默认 ~/.config/Rexion/sessions.db）
auto_migrate = true      # 是否自动从 JSONL 迁移
```

#### 验收标准

- [ ] SQLite 后端支持所有当前 JSONL 后端的功能
- [ ] 多进程并发安全（WAL 模式 + 适当锁策略）
- [ ] 自动迁移现有 JSONL 会话
- [ ] 会话列表查询性能 < 50ms（1000+ 会话）
- [ ] 消息按 turn 结构化存储和查询
- [ ] 压缩存档完整保留原始数据
- [ ] 提供 JSONL 导出功能

---

### P1-2: 多 Agent 并行编排

**目标**：实现真正的多 Agent 并行执行能力，支持角色定义、并行 spawn、Agent 间通信、深度/并发控制。

**当前状态**：TaskTool 仅做单次 API 调用（无工具、无循环、无沙盒），是极简实现。

#### 架构设计

```
                    ┌──────────────────┐
                    │   Parent Agent   │
                    │   (主 Agent)     │
                    └────────┬─────────┘
                             │ spawn
              ┌──────────────┼──────────────┐
              │              │              │
     ┌────────▼──────┐ ┌────▼─────────┐ ┌──▼──────────┐
     │  Child Agent 1 │ │ Child Agent 2│ │ Child Agent 3│
     │  (worker)      │ │ (explorer)   │ │ (worker)     │
     │  独立 Session  │ │ 独立 Session │ │ 独立 Session │
     │  过滤后工具集  │ │ 只读工具集   │ │ 过滤后工具集 │
     └───────────────┘ └──────────────┘ └──────────────┘
```

#### 四原语设计

```go
// internal/agent/spawn.go

// SpawnAgent 启动一个子 Agent
type SpawnAgentParams struct {
    Prompt    string            // 任务描述
    Role      string            // 角色：default/worker/explorer/monitor
    Model     string            // 可选：覆盖默认模型
    Tools     []string          // 可选：指定工具白名单
    MaxSteps  int               // 可选：最大步数
    Sandbox   string            // 可选：覆盖沙盒模式
}

// WaitAgent 等待子 Agent 完成
type WaitAgentParams struct {
    AgentID   string            // 子 Agent ID
    Timeout   time.Duration     // 等待超时
}

// SendInput 向运行中的子 Agent 发送追加指令
type SendInputParams struct {
    AgentID   string            // 子 Agent ID
    Message   string            // 追加消息
}

// CloseAgent 强制终止子 Agent
type CloseAgentParams struct {
    AgentID   string            // 子 Agent ID
}
```

#### Agent 角色系统

```go
// internal/agent/role.go

type Role struct {
    Name        string
    Description string          // 何时使用此角色
    Model       string          // 使用的模型（空则继承父 Agent）
    SystemAddon string          // 追加到系统提示的指令
    Tools       []string        // 允许的工具列表（空则全部）
    ReadOnly    bool            // 是否只限制为只读工具
    MaxSteps    int             // 最大步数
    SandboxMode string          // 沙盒模式
}

// 内置角色
var BuiltInRoles = map[string]Role{
    "default": {
        Name:        "default",
        Description: "通用子 Agent，全功能",
    },
    "worker": {
        Name:        "worker",
        Description: "执行型 Agent，负责实现和修复",
        SystemAddon: "你是一个执行型 Agent，专注于实现代码修改和修复问题。",
    },
    "explorer": {
        Name:        "explorer",
        Description: "探索型 Agent，只读研究",
        ReadOnly:    true,
        SystemAddon: "你是一个探索型 Agent，专注于阅读代码、搜索模式、收集证据。你不应修改任何文件。",
    },
    "monitor": {
        Name:        "monitor",
        Description: "监控型 Agent，等待和轮询长时间运行的命令",
        MaxSteps:    50,
        SystemAddon: "你是一个监控型 Agent，专注于等待命令完成、轮询状态。保持耐心，定期检查。",
    },
}
```

#### 并行执行管理器

```go
// internal/agent/pool.go

type Pool struct {
    mu       sync.RWMutex
    agents   map[string]*ChildAgent    // 活跃子 Agent
    results  map[string]*AgentResult   // 已完成的结果
    maxDepth int                       // 最大嵌套深度（默认 1）
    maxConc  int                       // 最大并发数（默认 6）
    parent   *Agent                    // 父 Agent 引用
}

type ChildAgent struct {
    ID        string
    Role      Role
    Agent     *Agent                   // 独立的 Agent 实例
    Session   *Session                 // 独立的 Session
    Cancel    context.CancelFunc       // 取消函数
    Done      chan struct{}            // 完成信号
    Result    *AgentResult             // 执行结果
}

type AgentResult struct {
    Summary    string                  // 最终回答
    ToolCalls  int                     // 工具调用次数
    FilesRead  []string                // 读取的文件
    FilesWrite []string                // 写入的文件
    Duration   time.Duration           // 执行耗时
    Usage      provider.Usage          // Token 用量
    Error      error                   // 错误
}
```

#### 与现有 TaskTool 的关系

- **替换**：新系统完全替代现有 TaskTool
- `task` 工具升级为 `spawn_agent`，支持角色、并行、工具传递
- 现有技能系统中的子 Agent 调用逐步迁移到新接口

#### 事件流扩展

```go
// 新增事件类型
event.AgentSpawned    // 子 Agent 启动 {AgentID, Role, Prompt}
event.AgentProgress   // 子 Agent 进度 {AgentID, ToolName, Status}
event.AgentCompleted  // 子 Agent 完成 {AgentID, Result}
event.AgentClosed     // 子 Agent 被关闭 {AgentID}
```

#### 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/agent/role.go` | 新增 | 角色定义 + 内置角色 |
| `internal/agent/pool.go` | 新增 | 并行执行管理器 |
| `internal/agent/spawn.go` | 新增 | spawn_agent/wait_agent/send_input/close_agent 工具 |
| `internal/agent/task.go` | 修改 | 升级为完整子 Agent（替代单次 API 调用） |
| `internal/agent/agent.go` | 修改 | Agent 持有 Pool 引用，深度控制 |
| `internal/event/event.go` | 修改 | 新增 Agent 相关事件类型 |
| `internal/config/config.go` | 修改 | 新增 agents 配置节 |
| `internal/boot/boot.go` | 修改 | 初始化 Pool，注册新工具 |

#### 配置扩展

```toml
[agents]
max_threads = 6           # 最大并发子 Agent 数
max_depth = 1             # 最大嵌套深度（防止递归）
job_timeout = "5m"        # 单个子 Agent 超时
default_model = ""        # 子 Agent 默认模型（空则继承父 Agent）

# 自定义角色
[[agents.roles]]
name = "reviewer"
description = "代码审查 Agent"
model = "deepseek-chat"
read_only = true
system_addon = "你是一个代码审查专家..."

[[agents.roles]]
name = "tester"
description = "测试 Agent"
max_steps = 30
sandbox_mode = "workspace-write"
system_addon = "你专注于运行测试并修复失败..."
```

#### 验收标准

- [ ] 可同时 spawn 多个子 Agent 并行执行
- [ ] 子 Agent 拥有完整的 Agent 循环（推理→工具→反馈）
- [ ] 子 Agent 工具集按角色过滤
- [ ] 最大并发数可配置
- [ ] 最大嵌套深度防止递归
- [ ] 子 Agent 继承父 Agent 的沙盒约束
- [ ] wait_agent 可等待特定子 Agent 完成
- [ ] send_input 可向运行中的子 Agent 发送追加指令
- [ ] 子 Agent 事件嵌套显示在父 Agent 事件下
- [ ] 子 Agent 完成后返回结构化结果

---

### P1-3: MCP 服务端模式

**目标**：让 Rexion 可以作为 MCP 工具服务器暴露，被其他 Agent（Claude Code、Cursor、Copilot 等）调用。

**当前状态**：仅有 MCP 客户端（连接外部 MCP 服务器），无服务端能力。

#### 架构设计

```
┌─────────────────────────────────┐
│  外部 Agent (Claude Code 等)     │
│  MCP Client                     │
└─────────────┬───────────────────┘
              │ stdio / HTTP
              │ JSON-RPC 2.0
┌─────────────▼───────────────────┐
│  Rexion MCP Server            │
│  ┌─────────────────────────┐    │
│  │  MCP 协议层              │    │
│  │  initialize / tools/*   │    │
│  └──────────┬──────────────┘    │
│  ┌──────────▼──────────────┐    │
│  │  Agent 引擎              │    │
│  │  (Controller.Run)       │    │
│  └──────────┬──────────────┘    │
│  ┌──────────▼──────────────┐    │
│  │  工具 / 沙盒 / 权限      │    │
│  └─────────────────────────┘    │
└─────────────────────────────────┘
```

#### 暴露的工具

| 工具名 | 说明 | 参数 |
|--------|------|------|
| `Rexion_code` | 代码修改 Agent | `prompt`, `sandbox_mode` |
| `Rexion_explore` | 代码探索（只读） | `prompt` |
| `Rexion_review` | 代码审查 | `target`, `focus` |
| `Rexion_test` | 测试运行和修复 | `command`, `fix` |

#### 启动方式

```bash
# stdio 模式（供 Cursor/VS Code 等 IDE 调用）
Rexion mcp-server --transport stdio

# HTTP 模式（供远程 Agent 调用）
Rexion mcp-server --transport http --addr 0.0.0.0:9090

# 配置方式
# 在 Cursor 的 .cursor/mcp.json 中：
{
  "mcpServers": {
    "Rexion": {
      "command": "Rexion",
      "args": ["mcp-server", "--transport", "stdio"]
    }
  }
}
```

#### 实现要点

```go
// internal/mcpserver/server.go

type Server struct {
    controller *control.Controller
    transport  string  // "stdio" | "http"
    addr       string  // HTTP 监听地址
}

func (s *Server) HandleInitialize(req InitializeRequest) InitializeResult {
    return InitializeResult{
        ProtocolVersion: "2024-11-05",
        Capabilities: ServerCapabilities{
            Tools: &ToolsCapability{ListChanged: false},
        },
        ServerInfo: Implementation{
            Name:    "Rexion",
            Version: version,
        },
    }
}

func (s *Server) HandleToolsList() ListToolsResult {
    return ListToolsResult{
        Tools: []Tool{
            {Name: "Rexion_code", Description: "...", InputSchema: ...},
            {Name: "Rexion_explore", Description: "...", InputSchema: ...},
            {Name: "Rexion_review", Description: "...", InputSchema: ...},
            {Name: "Rexion_test", Description: "...", InputSchema: ...},
        },
    }
}

func (s *Server) HandleToolsCall(req CallToolRequest) CallToolResult {
    // 将 MCP 工具调用映射为 Controller.Run()
    result, err := s.controller.Run(ctx, prompt)
    return CallToolResult{
        Content: []Content{
            {Type: "text", Text: result},
        },
    }
}
```

#### 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/mcpserver/server.go` | 新增 | MCP 服务端核心 |
| `internal/mcpserver/transport_stdio.go` | 新增 | stdio 传输层 |
| `internal/mcpserver/transport_http.go` | 新增 | HTTP 传输层 |
| `internal/mcpserver/handler.go` | 新增 | JSON-RPC 请求处理器 |
| `internal/cli/cli.go` | 修改 | 新增 `mcp-server` 子命令 |
| `cmd/Rexion/main.go` | 无需改动 | cli.Run 已覆盖 |

#### 验收标准

- [ ] `Rexion mcp-server --transport stdio` 正常启动
- [ ] Claude Code / Cursor 可发现和调用 Rexion 工具
- [ ] stdio 传输遵循 JSON-RPC 2.0 + MCP 协议
- [ ] HTTP 传输支持 SSE 流式响应
- [ ] 工具调用映射到 Controller.Run()
- [ ] 沙盒模式生效（外部调用默认 workspace-write）

---

### P1-4: REPL 工具

**目标**：新增持久化 REPL 工具，支持 JS/Python 代码执行，变量跨调用保持。

**当前状态**：bash 工具可执行单次命令，但无持久状态，每次调用独立进程。

#### 设计方案

```
┌──────────────────────────────────────┐
│           REPL Manager               │
│  ┌────────────┐  ┌────────────┐     │
│  │  JS REPL   │  │ Python REPL│     │
│  │ (node -i)  │  │(python -i) │     │
│  │  Session 1 │  │  Session 1 │     │
│  └────────────┘  └────────────┘     │
│                                      │
│  生命周期：Agent 会话级              │
│  沙盒：继承 bash 工具的沙盒策略       │
└──────────────────────────────────────┘
```

#### 工具定义

**js_eval**：
```json
{
  "name": "js_eval",
  "parameters": {
    "type": "object",
    "properties": {
      "code": {"type": "string", "description": "要执行的 JavaScript 代码"},
      "reset": {"type": "boolean", "description": "是否重置 REPL 状态"}
    },
    "required": ["code"]
  }
}
```

**python_eval**：
```json
{
  "name": "python_eval",
  "parameters": {
    "type": "object",
    "properties": {
      "code": {"type": "string", "description": "要执行的 Python 代码"},
      "reset": {"type": "boolean", "description": "是否重置 REPL 状态"}
    },
    "required": ["code"]
  }
}
```

#### 实现方案

```go
// internal/tool/builtin/repl.go

type REPLManager struct {
    mu       sync.Mutex
    sessions map[string]*REPLSession  // key: "js" | "python"
}

type REPLSession struct {
    lang    string
    cmd     *exec.Cmd           // 持久化子进程
    stdin   io.WriteCloser      // 标准输入管道
    stdout  *lineReader         // 带超时的行读取器
    buf     bytes.Buffer        // 输出缓冲
}

func (m *REPLManager) Eval(lang, code string, reset bool) (string, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    if reset {
        m.kill(lang)
    }

    session, ok := m.sessions[lang]
    if !ok {
        session = m.start(lang)
        m.sessions[lang] = session
    }

    // 写入代码 + 特殊分隔标记
    delimiter := fmt.Sprintf("__REPL_DELIM_%d__", time.Now().UnixNano())
    fmt.Fprintf(session.stdin, "%s\nconsole.log('%s')\n", code, delimiter)

    // 读取输出直到遇到分隔标记
    return session.readUntil(delimiter)
}
```

#### 安全考虑

- REPL 进程继承 bash 工具的沙盒策略
- 默认超时：单次 eval 30 秒
- 内存限制：通过 ulimit / Job Object 限制
- 网络限制：继承沙盒网络策略

#### 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/tool/builtin/repl.go` | 新增 | REPL 管理器 + js_eval / python_eval 工具 |
| `internal/tool/builtin/repl_js.go` | 新增 | JS REPL 会话管理 |
| `internal/tool/builtin/repl_python.go` | 新增 | Python REPL 会话管理 |
| `internal/tool/builtin/repl_test.go` | 新增 | REPL 测试 |
| `internal/agent/agent.go` | 修改 | Agent 退出时清理 REPL 会话 |

#### 配置扩展

```toml
[tools.repl]
enabled = true
js_path = "node"            # Node.js 可执行文件路径
python_path = "python3"     # Python 可执行文件路径
eval_timeout = "30s"        # 单次 eval 超时
max_memory_mb = 512         # 内存限制
```

#### 验收标准

- [ ] js_eval 支持变量跨调用保持
- [ ] python_eval 支持变量跨调用保持
- [ ] 重置功能清空 REPL 状态
- [ ] 执行超时自动终止
- [ ] REPL 进程受沙盒约束
- [ ] Agent 会话结束时清理所有 REPL 进程
- [ ] 语法错误和运行时错误正确返回

---

### P1-5: tool_search 工具

**目标**：当可用工具数量庞大时（MCP 服务器暴露数百工具），提供 BM25 语义搜索帮助模型发现合适的工具。

**当前状态**：工具注册在 Registry 中，模型看到所有工具的 schema，无搜索/发现机制。

#### 设计方案

```go
// internal/tool/search.go

type ToolSearch struct {
    registry *Registry
    index    *bm25.Index       // BM25 索引
    docs     []toolDoc          // 工具文档
}

type toolDoc struct {
    Name        string
    Description string
    Category    string          // "builtin" | "mcp" | "skill"
    Server      string          // MCP 服务器名（空为内置）
    Keywords    []string        // 提取的关键词
}
```

#### 工具定义

```json
{
  "name": "tool_search",
  "parameters": {
    "type": "object",
    "properties": {
      "query": {"type": "string", "description": "搜索关键词或功能描述"},
      "category": {"type": "string", "description": "可选：按类别过滤 (builtin/mcp/skill)"},
      "limit": {"type": "integer", "description": "返回结果数量，默认 10"}
    },
    "required": ["query"]
  }
}
```

#### 索引构建

```
触发时机：
1. Registry 发生变更时（工具添加/删除）
2. MCP 服务器连接/断开时
3. 首次搜索时懒构建

索引内容：
- 工具名称（加权 3.0）
- 工具描述（加权 1.0）
- 参数名（加权 1.5）
- MCP 服务器名（加权 0.5）

BM25 参数：
- k1 = 1.5（词频饱和参数）
- b = 0.75（文档长度归一化）
```

#### 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/tool/search.go` | 新增 | tool_search 工具 + BM25 索引 |
| `internal/tool/bm25.go` | 新增 | BM25 搜索引擎实现 |
| `internal/tool/registry.go` | 修改 | 变更时通知 ToolSearch 重建索引 |
| `go.mod` | 无需改动 | BM25 纯 Go 实现，无新依赖 |

#### 验收标准

- [ ] 搜索 "database" 返回数据库相关工具
- [ ] 搜索 "edit file" 返回 edit_file / apply_patch 等
- [ ] MCP 工具可被搜索发现
- [ ] 索引在工具变更时自动更新
- [ ] 搜索延迟 < 10ms（1000+ 工具）
- [ ] 结果包含工具名、描述、类别、匹配度分数

---

## 四、依赖关系与执行顺序

```
阶段 1（基础安全，可并行）:
  ├── P0-1: Linux 沙盒 ──────────────────┐
  ├── P0-2: Windows 沙盒 ────────────────┤ 依赖：P0-5 的接口重构
  └── P0-5: 三级沙盒模式 ────────────────┘ ← 先完成接口重构

  执行顺序：
  1. P0-5（接口重构 + 模式定义）→ 2. P0-1 + P0-2（并行实现各平台）

阶段 2（执行引擎）:
  └── P0-3: 无头执行模式增强（独立，无依赖）

阶段 3（工具增强）:
  └── P0-4: apply_patch 工具（独立，无依赖）

阶段 4（存储升级）:
  └── P1-1: SQLite 持久化（独立，但 P1-2 的子 Agent 可受益于 SQLite）

阶段 5（智能编排，依赖阶段 1+4）:
  └── P1-2: 多 Agent 并行编排
       依赖：P0-5（沙盒继承）、P1-1（子 Agent 独立 Session）

阶段 6（可并行）:
  ├── P1-3: MCP 服务端模式（独立）
  ├── P1-4: REPL 工具（独立）
  └── P1-5: tool_search 工具（独立）
```

```
时间线视图：

Week 1-2:  [P0-5 接口重构] → [P0-1 Linux 沙盒] + [P0-2 Windows 沙盒]
Week 2-3:  [P0-3 无头执行] + [P0-4 apply_patch]
Week 3-5:  [P1-1 SQLite 持久化]
Week 5-7:  [P1-2 多 Agent 编排]
Week 7-9:  [P1-3 MCP 服务端] + [P1-4 REPL] + [P1-5 tool_search]
```

---

## 五、通用改造原则

### 1. 接口先行
- 所有新功能先定义接口（Go interface），再实现
- 接口设计考虑跨平台、可测试、可扩展
- 现有接口变更保持向后兼容

### 2. 渐进式替换
- 新旧后端共存（如 JSONL + SQLite）
- 通过配置选择后端，默认新后端
- 提供迁移工具，不强制一次性迁移

### 3. 安全默认
- 所有新功能默认最安全选项
- 沙盒不可用时优雅降级 + 明确告警
- 权限检查在工具执行前完成

### 4. 事件驱动
- 所有新功能通过事件流（event.Sink）报告状态
- 前端无需修改即可支持新功能
- 子 Agent 事件嵌套在父 Agent 事件下

### 5. 可观测性
- 新增 Notice 事件用于沙盒状态、权限决策、Agent 生命周期
- 结构化日志记录关键操作
- 诊断命令（Rexion doctor）覆盖新功能健康检查

### 6. 测试策略
- 每个新功能包含单元测试 + 集成测试
- 沙盒测试：验证逃逸防护（尝试越界操作）
- 并发测试：验证多 Agent 并行的竞态安全
- 平台特定测试：使用构建标签隔离

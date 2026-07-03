# Reasonix 生产包构建指南

## 前置条件

| 依赖 | 安装方式 | 验证命令 |
|------|----------|----------|
| Go 1.25+ | https://go.dev/dl | `go version` |
| Node.js 22 | nvm 或官网安装 | `node -v` |
| pnpm 11+ | `npm i -g pnpm` | `pnpm -v` |
| Wails CLI v2.12.0 | `go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0` | `wails version` |
| NSIS | `winget install NSIS.NSIS` | `makensis /VERSION` |

NSIS 安装后需确认 `makensis` 在 PATH 中，默认安装路径为 `C:\Program Files (x86)\NSIS\`。

---

## 一键构建脚本（PowerShell）

将以下内容保存为 `build-release.ps1`，在项目根目录执行：

```powershell
# build-release.ps1 - Reasonix 生产包一键构建脚本
param(
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
if (-not $Root) { $Root = (Get-Location).Path }

# --- 版本号 ---
if (-not $Version) {
    $Version = git describe --tags --always 2>$null
    if (-not $Version) { $Version = "dev" }
}
$LDFLAGS = "-s -w -X main.version=$Version"
Write-Host "`n=== Reasonix 生产包构建 v$Version ===" -ForegroundColor Cyan

# --- 0. 确保 NSIS 在 PATH ---
$nsisDir = "C:\Program Files (x86)\NSIS"
if ((Get-Command makensis -ErrorAction SilentlyContinue) -eq $null) {
    if (Test-Path $nsisDir) {
        $env:PATH = "$nsisDir;$env:PATH"
        Write-Host "[OK] NSIS 已加入 PATH" -ForegroundColor Green
    } else {
        Write-Host "[WARN] NSIS 未安装，安装包构建将跳过" -ForegroundColor Yellow
    }
}

# --- 1. 构建 CLI 主程序 + 主模块插件 ---
Write-Host "`n[1/5] 构建 CLI 主程序和主模块插件..." -ForegroundColor Yellow
New-Item -ItemType Directory -Force -Path "$Root\bin" | Out-Null
$env:CGO_ENABLED = "0"
go build -ldflags $LDFLAGS -o "$Root\bin\reasonix.exe" ./cmd/reasonix
$mainPlugins = @(
    "reasonix-plugin-example",
    "reasonix-plugin-office",
    "reasonix-plugin-sheet",
    "reasonix-plugin-mail",
    "reasonix-plugin-im"
)
foreach ($p in $mainPlugins) {
    go build -ldflags $LDFLAGS -o "$Root\bin\$p.exe" "./cmd/$p"
}
Write-Host "  CLI 主程序 + 主模块插件构建完成" -ForegroundColor Green

# --- 2. 构建独立模块插件 ---
Write-Host "`n[2/5] 构建独立模块插件..." -ForegroundColor Yellow
$standalonePlugins = @(
    "reasonix-plugin-calendar",
    "reasonix-plugin-slides",
    "reasonix-plugin-search"
)
foreach ($p in $standalonePlugins) {
    Push-Location "$Root\cmd\$p"
    go build -o "$Root\bin\$p.exe" .
    Pop-Location
}
Write-Host "  独立模块插件构建完成" -ForegroundColor Green

# --- 3. 构建前端 ---
Write-Host "`n[3/5] 构建前端..." -ForegroundColor Yellow
Push-Location "$Root\desktop\frontend"
$nodeDir = "c:\Users\tianxb\AppData\Local\nvm\v22.16.0"
if (Test-Path $nodeDir) { $env:PATH = "$nodeDir;$env:PATH" }
pnpm install
pnpm build
Pop-Location
Write-Host "  前端构建完成" -ForegroundColor Green

# --- 4. 准备插件到 desktop/build/bin/plugins ---
Write-Host "`n[4/5] 准备插件到 NSIS 打包目录..." -ForegroundColor Yellow
$pluginsDir = "$Root\desktop\build\bin\plugins"
New-Item -ItemType Directory -Force -Path $pluginsDir | Out-Null
$allPlugins = @(
    "reasonix-plugin-office",
    "reasonix-plugin-sheet",
    "reasonix-plugin-search",
    "reasonix-plugin-calendar",
    "reasonix-plugin-slides",
    "reasonix-plugin-mail",
    "reasonix-plugin-im"
)
foreach ($p in $allPlugins) {
    Copy-Item "$Root\bin\$p.exe" $pluginsDir -Force
}
Write-Host "  插件已复制到 $pluginsDir" -ForegroundColor Green

# --- 5. 构建 Desktop + NSIS 安装包 ---
Write-Host "`n[5/5] 构建 Desktop 安装包..." -ForegroundColor Yellow
Push-Location "$Root\desktop"
# 注意：不要用 -clean，否则会清空 plugins 目录
wails build -platform windows/amd64 -nsis -ldflags "-X main.version=$Version"
Pop-Location

$installer = Get-ChildItem "$Root\desktop\build\bin\*installer*.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
if ($installer) {
    Write-Host "`n=== 构建成功 ===" -ForegroundColor Green
    Write-Host "  安装包: $($installer.FullName)" -ForegroundColor White
    Write-Host "  大小: $([math]::Round($installer.Length/1MB, 1)) MB" -ForegroundColor White
    Write-Host "  便携版: $Root\desktop\build\bin\reasonix-desktop.exe" -ForegroundColor White
} else {
    Write-Host "`n=== 构建完成（无安装包，NSIS 可能未安装）===" -ForegroundColor Yellow
    Write-Host "  便携版: $Root\desktop\build\bin\reasonix-desktop.exe" -ForegroundColor White
}

Write-Host "`n  CLI: $Root\bin\reasonix.exe" -ForegroundColor White
Write-Host "  插件: $Root\bin\reasonix-plugin-*.exe" -ForegroundColor White
```

---

## 手动分步构建

### 第 1 步：构建 CLI + 主模块插件

```powershell
$VERSION = git describe --tags --always 2>$null; if (-not $VERSION) { $VERSION = "dev" }
$LDFLAGS = "-s -w -X main.version=$VERSION"
$env:CGO_ENABLED = "0"
New-Item -ItemType Directory -Force -Path bin | Out-Null

# 主程序
go build -ldflags $LDFLAGS -o bin/reasonix.exe ./cmd/reasonix

# 主模块插件（go.mod 在根模块内）
foreach ($p in @("reasonix-plugin-example","reasonix-plugin-office","reasonix-plugin-sheet","reasonix-plugin-mail","reasonix-plugin-im")) {
    go build -ldflags $LDFLAGS -o "bin/$p.exe" "./cmd/$p"
}
```

### 第 2 步：构建独立模块插件

独立模块有各自的 `go.mod`，需在其目录下构建：

```powershell
foreach ($p in @("reasonix-plugin-calendar","reasonix-plugin-slides","reasonix-plugin-search")) {
    Push-Location "cmd/$p"
    go build -o "../../bin/$p.exe" .
    Pop-Location
}
```

### 第 3 步：构建前端

```powershell
cd desktop/frontend
$env:PATH = "c:\Users\tianxb\AppData\Local\nvm\v22.16.0;" + $env:PATH  # 按需
pnpm install
pnpm build   # CSS检查 + TypeScript检查 + Vite生产构建
```

### 第 4 步：准备插件到 NSIS 打包目录

```powershell
New-Item -ItemType Directory -Force -Path desktop/build/bin/plugins | Out-Null
Copy-Item bin/reasonix-plugin-*.exe desktop/build/bin/plugins/
```

> **关键**：此步骤必须在 `wails build` **之前**完成，因为 NSIS 的 `File` 指令在打包时读取这些文件。
> **不要**使用 `wails build -clean`，它会清空 `desktop/build/bin/` 目录。

### 第 5 步：构建 Desktop + NSIS 安装包

```powershell
cd desktop
$env:PATH = "C:\Program Files (x86)\NSIS;" + $env:PATH  # 确保 makensis 可用
wails build -platform windows/amd64 -nsis -ldflags "-X main.version=$VERSION"
```

产物：
- 安装包：`desktop/build/bin/reasonix-desktop-amd64-installer.exe` (~52 MB)
- 便携版：`desktop/build/bin/reasonix-desktop.exe` (~30 MB)

---

## 产物清单

| 产物 | 路径 | 说明 |
|------|------|------|
| CLI 主程序 | `bin/reasonix.exe` | 命令行版本 |
| 插件 x7 | `bin/reasonix-plugin-*.exe` | MCP 工具服务器 |
| Desktop 便携版 | `desktop/build/bin/reasonix-desktop.exe` | 免安装运行 |
| Desktop 安装包 | `desktop/build/bin/reasonix-desktop-amd64-installer.exe` | NSIS 安装程序 |

## 安装包内容

安装包会自动安装以下内容到 `%LOCALAPPDATA%\Programs\Reasonix\`：

```
Reasonix/
├── reasonix-desktop.exe          # 主程序
├── plugins/
│   ├── reasonix-plugin-office.exe
│   ├── reasonix-plugin-sheet.exe
│   ├── reasonix-plugin-search.exe
│   ├── reasonix-plugin-calendar.exe
│   ├── reasonix-plugin-slides.exe
│   ├── reasonix-plugin-mail.exe
│   └── reasonix-plugin-im.exe
└── uninstall.exe
```

安装完成后，用户需在 `reasonix.toml` 中配置插件路径：

```toml
[[plugins]]
name    = "office"
command = "plugins/reasonix-plugin-office.exe"

[[plugins]]
name    = "sheet"
command = "plugins/reasonix-plugin-sheet.exe"
# ... 其他插件类似
```

## 常见问题

### Q: `makensis not found`
安装 NSIS：`winget install NSIS.NSIS`，然后确保 `C:\Program Files (x86)\NSIS` 在 PATH 中。

### Q: `wails build -clean` 清空了插件目录
**不要用 `-clean`**。插件需在构建前手动复制到 `desktop/build/bin/plugins/`。

### Q: 独立模块插件报 `main module does not contain package`
calendar、slides、search 有独立的 `go.mod`，需在其目录下执行 `go build`。

### Q: 前端构建报 `pnpm not found`
确保 Node.js 22+ 在 PATH：`$env:PATH = "c:\Users\tianxb\AppData\Local\nvm\v22.16.0;" + $env:PATH`

### Q: 安装包触发 SmartScreen 警告
未做 Windows Authenticode 代码签名。如需消除警告，需购买代码签名证书并在 `project.nsi` 中取消注释 `signtool` 行。

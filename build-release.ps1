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

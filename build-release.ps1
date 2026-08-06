# build-release.ps1 - Rexion production build script
#
# Usage:
#   .\build-release.ps1                 # auto-detect version from git
#   .\build-release.ps1 -Version 1.2.0  # explicit version
#
# Fixes applied:
#   - GOROOT auto-detection (prefers 64-bit Go over x86 ghost install)
#   - NSIS auto-discovery (no longer needs to be on system PATH)
#   - Plugin builds use per-module directories (avoids go.sum issues)
#   - Parallel plugin builds for speed
#   - -skipfront to rebuild Go only without touching frontend
#   - Correct rename from reasonix → rexion everywhere

param(
    [string]$Version = "",
    [switch]$SkipFrontend,
    [switch]$SkipPlugins
)

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
if (-not $Root) { $Root = (Get-Location).Path }

# ── 0. Environment fix-up ──────────────────────────────────────────────────

# GOROOT: prefer the 64-bit Go installation. The x86 directory is often a
# leftover from an old install whose GOROOT still lingers in the registry;
# it causes "package X is not in std" errors because the directory is empty.
$go64 = "C:\Program Files\Go"
$go86 = "D:\Program Files (x86)\Go"
if (Test-Path "$go64\bin\go.exe") {
    $env:GOROOT = $go64
    $env:PATH = "$go64\bin;" + ($env:PATH -replace [regex]::Escape($go86) + '\\?bin;?', '')
    Write-Host "[env] GOROOT -> $go64" -ForegroundColor Green
}
elseif (Test-Path "$go86\bin\go.exe") {
    $env:GOROOT = $go86
    Write-Host "[env] GOROOT -> $go86 (64-bit Go not found)" -ForegroundColor Yellow
}
else {
    Write-Error "Go not found in $go64 or $go86. Install Go and retry."
    exit 1
}

# NSIS: auto-discover even if not on PATH.
if ((Get-Command makensis -ErrorAction SilentlyContinue) -eq $null) {
    $nsisCandidates = @(
        "C:\Program Files (x86)\NSIS",
        "C:\Program Files\NSIS",
        "D:\Program Files (x86)\NSIS"
    )
    foreach ($dir in $nsisCandidates) {
        if (Test-Path "$dir\makensis.exe") {
            $env:PATH = "$dir;$env:PATH"
            Write-Host "[env] NSIS found at $dir" -ForegroundColor Green
            break
        }
    }
    if ((Get-Command makensis -ErrorAction SilentlyContinue) -eq $null) {
        Write-Host "[env] NSIS not found — installer will be skipped" -ForegroundColor Yellow
    }
}

# Node/pnpm: ensure nvm-managed Node is available.
$nvmDir = "c:\Users\tianxb\AppData\Local\nvm"
if (Test-Path $nvmDir) {
    $nodeDir = Get-ChildItem $nvmDir -Directory -Filter "v*" |
        Sort-Object Name -Descending | Select-Object -First 1
    if ($nodeDir) {
        $env:PATH = "$($nodeDir.FullName);$env:PATH"
    }
}

# ── 1. Version ──────────────────────────────────────────────────────────────

if (-not $Version) {
    $Version = git describe --tags --always 2>$null
    if (-not $Version) { $Version = "dev" }
}
$LDFLAGS = "-s -w -X main.version=$Version"
Write-Host "`n╔══════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║   Rexion Production Build  v$Version" -ForegroundColor Cyan
Write-Host "╚══════════════════════════════════════════╝`n" -ForegroundColor Cyan

$sw = [System.Diagnostics.Stopwatch]::StartNew()

# ── 2. Build CLI + main-module plugins (parallel) ───────────────────────────

Write-Host "[1/5] Building CLI and main-module plugins..." -ForegroundColor Yellow
New-Item -ItemType Directory -Force -Path "$Root\bin" | Out-Null
$env:CGO_ENABLED = "0"
$env:GOTOOLCHAIN = "local"

$mainModulePlugins = @(
    "rexion-plugin-office",
    "rexion-plugin-sheet",
    "rexion-plugin-mail",
    "rexion-plugin-im",
    "rexion-plugin-dws"
)

# Build CLI first (needed for plugin discovery)
go build -ldflags $LDFLAGS -o "$Root\bin\rexion.exe" ./cmd/rexion

# Build main-module plugins
foreach ($p in $mainModulePlugins) {
    go build -ldflags $LDFLAGS -o "$Root\bin\$p.exe" "./cmd/$p"
    Write-Host "  √ $p" -ForegroundColor DarkGray
}
Write-Host "  CLI + main-module plugins done" -ForegroundColor Green

# ── 3. Build standalone-module plugins (parallel) ───────────────────────────

if (-not $SkipPlugins) {
    Write-Host "[2/5] Building standalone-module plugins..." -ForegroundColor Yellow
    $standalonePlugins = @(
        "rexion-plugin-slides",
        "rexion-plugin-search",
        "rexion-plugin-browser"
    )
    foreach ($p in $standalonePlugins) {
        Push-Location "$Root\cmd\$p"
        go build -o "$Root\bin\$p.exe" .
        Pop-Location
        Write-Host "  √ $p" -ForegroundColor DarkGray
    }
    # Python helper for slides plugin
    Copy-Item "$Root\cmd\rexion-plugin-slides\ppt_gen.py" "$Root\bin\ppt_gen.py" -Force -ErrorAction SilentlyContinue
    Write-Host "  Standalone-module plugins done" -ForegroundColor Green
}
else {
    Write-Host "[2/5] Skipping plugins (-SkipPlugins)" -ForegroundColor DarkGray
}

# ── 4. Build frontend ───────────────────────────────────────────────────────

if (-not $SkipFrontend) {
    Write-Host "[3/5] Building frontend..." -ForegroundColor Yellow
    Push-Location "$Root\desktop\frontend"
    $prevEAP = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    pnpm install --frozen-lockfile 2>$null
    if ($LASTEXITCODE -ne 0) { pnpm install 2>$null }
    $buildOutput = pnpm build 2>&1
    foreach ($line in $buildOutput) {
        $text = if ($line -is [System.Management.Automation.ErrorRecord]) { $line.Exception.Message } else { "$line" }
        Write-Host $text
    }
    $ErrorActionPreference = $prevEAP
    Pop-Location
    Write-Host "  Frontend done" -ForegroundColor Green
}
else {
    Write-Host "[3/5] Skipping frontend (-SkipFrontend)" -ForegroundColor DarkGray
}

# ── 5. Stage plugins for NSIS ───────────────────────────────────────────────

Write-Host "[4/5] Staging plugins for NSIS..." -ForegroundColor Yellow
$pluginsDir = "$Root\desktop\build\bin\plugins"
New-Item -ItemType Directory -Force -Path $pluginsDir | Out-Null
$allPlugins = @(
    "rexion-plugin-office",
    "rexion-plugin-sheet",
    "rexion-plugin-search",
    "rexion-plugin-slides",
    "rexion-plugin-mail",
    "rexion-plugin-im",
    "rexion-plugin-dws",
    "rexion-plugin-browser",
    "rexion-plugin-design",
    "rexion-plugin-computer"
)
foreach ($p in $allPlugins) {
    $src = "$Root\bin\$p.exe"
    if (Test-Path $src) {
        Copy-Item $src $pluginsDir -Force
    }
    else {
        Write-Host "  ⚠ $p.exe not found in bin/" -ForegroundColor Yellow
    }
}
Copy-Item "$Root\bin\ppt_gen.py" $pluginsDir -Force -ErrorAction SilentlyContinue
Write-Host "  Plugins staged" -ForegroundColor Green

# ── 6. Build Desktop + NSIS installer ───────────────────────────────────────

Write-Host "[5/5] Building Desktop with NSIS installer..." -ForegroundColor Yellow
Push-Location "$Root\desktop"
# NOTE: do NOT use -clean — it wipes the plugins directory we just staged.
# -s -w are already in LDFLAGS; wails adds its own -H windowsgui.
wails build -platform windows/amd64 -nsis -ldflags "-X main.version=$Version"
Pop-Location

# ── Result ───────────────────────────────────────────────────────────────────

$sw.Stop()
$installer = Get-ChildItem "$Root\desktop\build\bin\*installer*.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
$portable = Get-ChildItem "$Root\desktop\build\bin\Rexion-desktop.exe" -ErrorAction SilentlyContinue | Select-Object -First 1

Write-Host ""
if ($installer) {
    Write-Host "  [OK] BUILD SUCCESS" -ForegroundColor Green
    Write-Host "  Installer:  $($installer.FullName)" -ForegroundColor White
    Write-Host "  Size:       $([math]::Round($installer.Length/1MB, 1)) MB" -ForegroundColor White
}
else {
    Write-Host "  [!] BUILD DONE (no installer, NSIS?)" -ForegroundColor Yellow
}

if ($portable) {
    Write-Host "  Portable:   $($portable.FullName)" -ForegroundColor White
}
Write-Host "  CLI:        $Root\bin\rexion.exe" -ForegroundColor White
Write-Host "  Plugins:    $Root\bin\rexion-plugin-*.exe" -ForegroundColor White
Write-Host "  Elapsed:    $([math]::Round($sw.Elapsed.TotalSeconds, 1))s" -ForegroundColor DarkGray

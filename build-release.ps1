# build-release.ps1 - Reasonix production build script
param(
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"
$Root = $PSScriptRoot
if (-not $Root) { $Root = (Get-Location).Path }

# Version
if (-not $Version) {
    $Version = git describe --tags --always 2>$null
    if (-not $Version) { $Version = "dev" }
}
$LDFLAGS = "-s -w -X main.version=$Version"
Write-Host "`n=== Reasonix Production Build v$Version ===" -ForegroundColor Cyan

# 0. Ensure NSIS in PATH
$nsisDir = "C:\Program Files (x86)\NSIS"
if ((Get-Command makensis -ErrorAction SilentlyContinue) -eq $null) {
    if (Test-Path $nsisDir) {
        $env:PATH = "$nsisDir;$env:PATH"
        Write-Host "[OK] NSIS added to PATH" -ForegroundColor Green
    } else {
        Write-Host "[WARN] NSIS not installed, installer build will be skipped" -ForegroundColor Yellow
    }
}

# 1. Build CLI + main-module plugins
Write-Host "`n[1/5] Building CLI and main-module plugins..." -ForegroundColor Yellow
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
Write-Host "  CLI + main-module plugins done" -ForegroundColor Green

# 2. Build standalone-module plugins
Write-Host "`n[2/5] Building standalone-module plugins..." -ForegroundColor Yellow
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
# Copy python helper scripts (slides plugin requires ppt_gen.py)
Copy-Item "$Root\cmd\reasonix-plugin-slides\ppt_gen.py" "$Root\bin\ppt_gen.py" -Force
Write-Host "  Standalone-module plugins done" -ForegroundColor Green

# 3. Build frontend
Write-Host "`n[3/5] Building frontend..." -ForegroundColor Yellow
Push-Location "$Root\desktop\frontend"
$nodeDir = "c:\Users\tianxb\AppData\Local\nvm\v22.16.0"
if (Test-Path $nodeDir) { $env:PATH = "$nodeDir;$env:PATH" }
pnpm install
pnpm build
Pop-Location
Write-Host "  Frontend done" -ForegroundColor Green

# 4. Copy plugins to NSIS staging dir
Write-Host "`n[4/5] Staging plugins for NSIS..." -ForegroundColor Yellow
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
# Copy python helper scripts for slides plugin
Copy-Item "$Root\bin\ppt_gen.py" $pluginsDir -Force -ErrorAction SilentlyContinue
Write-Host "  Plugins staged to $pluginsDir" -ForegroundColor Green

# 5. Build Desktop + NSIS installer
Write-Host "`n[5/5] Building Desktop with NSIS installer..." -ForegroundColor Yellow
Push-Location "$Root\desktop"
# NOTE: do NOT use -clean, it wipes the plugins directory
wails build -platform windows/amd64 -nsis -ldflags "-X main.version=$Version"
Pop-Location

$installer = Get-ChildItem "$Root\desktop\build\bin\*installer*.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
if ($installer) {
    Write-Host "`n=== BUILD SUCCESS ===" -ForegroundColor Green
    Write-Host "  Installer: $($installer.FullName)" -ForegroundColor White
    Write-Host "  Size: $([math]::Round($installer.Length/1MB, 1)) MB" -ForegroundColor White
    Write-Host "  Portable: $Root\desktop\build\bin\reasonix-desktop.exe" -ForegroundColor White
} else {
    Write-Host "`n=== BUILD DONE (no installer, NSIS may not be installed) ===" -ForegroundColor Yellow
    Write-Host "  Portable: $Root\desktop\build\bin\reasonix-desktop.exe" -ForegroundColor White
}

Write-Host "`n  CLI: $Root\bin\reasonix.exe" -ForegroundColor White
Write-Host "  Plugins: $Root\bin\reasonix-plugin-*.exe" -ForegroundColor White

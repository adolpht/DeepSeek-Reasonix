# setup.ps1 — Environment setup for minimax-docx skill (Windows PowerShell)
# Checks for dotnet SDK 8+, Python 3, LibreOffice (optional)
# Installs DocumentFormat.OpenXml NuGet package if needed
# Usage: powershell -File setup.ps1 [-Minimal]

param(
    [switch]$Minimal
)

$ErrorActionPreference = "Stop"
$errors = 0
$warnings = 0

# ── Determine script directory ────────────────────────────────────────────────
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$SkillDir = Split-Path -Parent $ScriptDir
$NugetDir = Join-Path $SkillDir "scripts\dotnet\.nuget"

# ── 1. Check dotnet SDK 8+ ───────────────────────────────────────────────────
Write-Host "Checking dotnet SDK..."
$dotnetExe = Get-Command dotnet -ErrorAction SilentlyContinue
if ($dotnetExe) {
    try {
        $dotnetVersion = (dotnet --version 2>$null)
        $major = [int]($dotnetVersion.Split('.')[0])
        if ($major -ge 8) {
            Write-Host "  ✔ dotnet SDK $dotnetVersion found" -ForegroundColor Green
        } else {
            Write-Host "  ✘ dotnet SDK $dotnetVersion found, but version 8+ required" -ForegroundColor Red
            $errors++
        }
    } catch {
        Write-Host "  ✘ Could not determine dotnet version" -ForegroundColor Red
        $errors++
    }
} else {
    Write-Host "  ✘ dotnet SDK not found. Install from https://dot.net/download" -ForegroundColor Red
    $errors++
}

# ── 2. Check Python 3 ────────────────────────────────────────────────────────
Write-Host "Checking Python 3..."
$pythonExe = Get-Command python3 -ErrorAction SilentlyContinue
if (-not $pythonExe) {
    $pythonExe = Get-Command python -ErrorAction SilentlyContinue
}
if ($pythonExe) {
    try {
        $pyVersion = & $pythonExe.Name --version 2>&1
        if ($pyVersion -match "Python 3") {
            Write-Host "  ✔ $pyVersion found" -ForegroundColor Green
        } else {
            Write-Host "  ✘ python found but not Python 3 ($pyVersion)" -ForegroundColor Red
            $errors++
        }
    } catch {
        Write-Host "  ✘ Could not determine Python version" -ForegroundColor Red
        $errors++
    }
} else {
    Write-Host "  ✘ Python 3 not found. Install from https://python.org" -ForegroundColor Red
    $errors++
}

# ── 3. Check LibreOffice (optional) ──────────────────────────────────────────
if (-not $Minimal) {
    Write-Host "Checking LibreOffice (optional, for .doc -> .docx conversion)..."
    $loPaths = @(
        "C:\Program Files\LibreOffice\program\soffice.exe",
        "C:\Program Files (x86)\LibreOffice\program\soffice.exe"
    )
    $loFound = $false
    foreach ($p in $loPaths) {
        if (Test-Path $p) {
            Write-Host "  ✔ LibreOffice found at $p" -ForegroundColor Green
            $loFound = $true
            break
        }
    }
    if (-not $loFound) {
        $soffice = Get-Command soffice -ErrorAction SilentlyContinue
        if ($soffice) {
            Write-Host "  ✔ soffice found in PATH" -ForegroundColor Green
            $loFound = $true
        }
    }
    if (-not $loFound) {
        Write-Host "  ⚠ LibreOffice not found. .doc -> .docx conversion will be unavailable." -ForegroundColor Yellow
        Write-Host "    Install from https://libreoffice.org or rerun with -Minimal"
        $warnings++
    }
} else {
    Write-Host "Skipping LibreOffice check (-Minimal mode)"
}

# ── 4. Install DocumentFormat.OpenXml NuGet package ──────────────────────────
Write-Host "Checking DocumentFormat.OpenXml NuGet package..."
if ($dotnetExe) {
    try {
        $dotnetVersion = (dotnet --version 2>$null)
        $major = [int]($dotnetVersion.Split('.')[0])
        if ($major -ge 8) {
            New-Item -ItemType Directory -Path $NugetDir -Force | Out-Null
            $tempProj = Join-Path $NugetDir "temp_restore.proj"
            @"
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="DocumentFormat.OpenXml" Version="3.2.0" />
  </ItemGroup>
</Project>
"@ | Set-Content $tempProj
            $packagesDir = Join-Path $NugetDir "packages"
            try {
                dotnet restore $tempProj --packages $packagesDir 2>$null | Out-Null
                Write-Host "  ✔ DocumentFormat.OpenXml 3.2.0 restored" -ForegroundColor Green
            } catch {
                Write-Host "  ⚠ Could not restore DocumentFormat.OpenXml 3.2.0 (network issue?)" -ForegroundColor Yellow
                Write-Host "    The package will be restored on first build."
                $warnings++
            }
            Remove-Item $tempProj -Force -ErrorAction SilentlyContinue
        }
    } catch {
        # Skip if dotnet check already failed
    }
}

# ── Summary ──────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "────────────────────────────────────────"
if ($errors -eq 0) {
    Write-Host "READY" -ForegroundColor Green
    if ($warnings -gt 0) {
        Write-Host "  ($warnings warning(s), non-critical)"
    }
} else {
    Write-Host "NOT READY" -ForegroundColor Red
    Write-Host "  $errors critical error(s) must be resolved"
}
Write-Host "────────────────────────────────────────"
if ($errors -gt 0) { exit 1 }
exit 0

#!/usr/bin/env bash
# setup.sh — Environment setup for minimax-docx skill
# Checks for dotnet SDK 8+, Python 3, LibreOffice (optional)
# Installs DocumentFormat.OpenXml NuGet package if needed
# Usage: bash setup.sh [--minimal]

set -euo pipefail

MINIMAL=false
for arg in "$@"; do
    case "$arg" in
        --minimal) MINIMAL=true ;;
        -h|--help)
            echo "Usage: bash setup.sh [--minimal]"
            echo "  --minimal  Skip optional dependencies (LibreOffice)"
            exit 0
            ;;
    esac
done

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

errors=0
warnings=0

# ── Determine script directory ────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKILL_DIR="$(dirname "$SCRIPT_DIR")"
NUGET_DIR="$SKILL_DIR/scripts/dotnet/.nuget"

# ── 1. Check dotnet SDK 8+ ───────────────────────────────────────────────────
echo "Checking dotnet SDK..."
if command -v dotnet &>/dev/null; then
    DOTNET_VERSION="$(dotnet --version 2>/dev/null || echo "0")"
    DOTNET_MAJOR="${DOTNET_VERSION%%.*}"
    if [ "$DOTNET_MAJOR" -ge 8 ]; then
        echo -e "  ${GREEN}✔ dotnet SDK $DOTNET_VERSION found${NC}"
    else
        echo -e "  ${RED}✘ dotnet SDK $DOTNET_VERSION found, but version 8+ required${NC}"
        errors=$((errors + 1))
    fi
else
    echo -e "  ${RED}✘ dotnet SDK not found. Install from https://dot.net/download${NC}"
    errors=$((errors + 1))
fi

# ── 2. Check Python 3 ────────────────────────────────────────────────────────
echo "Checking Python 3..."
if command -v python3 &>/dev/null; then
    PY_VERSION="$(python3 --version 2>/dev/null || echo "unknown")"
    echo -e "  ${GREEN}✔ $PY_VERSION found${NC}"
elif command -v python &>/dev/null; then
    PY_VERSION="$(python --version 2>/dev/null || echo "unknown")"
    case "$PY_VERSION" in
        Python\ 3*) echo -e "  ${GREEN}✔ $PY_VERSION found (as 'python')${NC}" ;;
        *) echo -e "  ${RED}✘ python found but not Python 3${NC}"; errors=$((errors + 1)) ;;
    esac
else
    echo -e "  ${RED}✘ Python 3 not found. Install from https://python.org${NC}"
    errors=$((errors + 1))
fi

# ── 3. Check LibreOffice (optional) ──────────────────────────────────────────
if [ "$MINIMAL" = false ]; then
    echo "Checking LibreOffice (optional, for .doc → .docx conversion)..."
    if command -v libreoffice &>/dev/null; then
        LO_VERSION="$(libreoffice --version 2>/dev/null || echo "unknown")"
        echo -e "  ${GREEN}✔ $LO_VERSION found${NC}"
    elif command -v soffice &>/dev/null; then
        echo -e "  ${GREEN}✔ soffice found (LibreOffice headless mode available)${NC}"
    else
        echo -e "  ${YELLOW}⚠ LibreOffice not found. .doc → .docx conversion will be unavailable.${NC}"
        echo "    Install from https://libreoffice.org or rerun with --minimal"
        warnings=$((warnings + 1))
    fi
else
    echo "Skipping LibreOffice check (--minimal mode)"
fi

# ── 4. Install DocumentFormat.OpenXml NuGet package ──────────────────────────
echo "Checking DocumentFormat.OpenXml NuGet package..."
if command -v dotnet &>/dev/null; then
    DOTNET_MAJOR="${DOTNET_VERSION%%.*}"
    if [ "$DOTNET_MAJOR" -ge 8 ]; then
        mkdir -p "$NUGET_DIR"
        # Use a temporary project to resolve and cache the NuGet package
        TEMP_PROJ="$NUGET_DIR/temp_restore.proj"
        cat > "$TEMP_PROJ" <<'PROJ'
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="DocumentFormat.OpenXml" Version="3.2.0" />
  </ItemGroup>
</Project>
PROJ
        if dotnet restore "$TEMP_PROJ" --packages "$NUGET_DIR/packages" &>/dev/null; then
            echo -e "  ${GREEN}✔ DocumentFormat.OpenXml 3.2.0 restored${NC}"
        else
            echo -e "  ${YELLOW}⚠ Could not restore DocumentFormat.OpenXml 3.2.0 (network issue?)${NC}"
            echo "    The package will be restored on first build."
            warnings=$((warnings + 1))
        fi
        rm -f "$TEMP_PROJ"
    fi
fi

# ── Summary ──────────────────────────────────────────────────────────────────
echo ""
echo "────────────────────────────────────────"
if [ "$errors" -eq 0 ]; then
    echo -e "${GREEN}READY${NC}"
    [ "$warnings" -gt 0 ] && echo -e "  (${warnings} warning(s), non-critical)"
else
    echo -e "${RED}NOT READY${NC}"
    echo "  $errors critical error(s) must be resolved"
fi
echo "────────────────────────────────────────"
[ "$errors" -gt 0 ] && exit 1
exit 0

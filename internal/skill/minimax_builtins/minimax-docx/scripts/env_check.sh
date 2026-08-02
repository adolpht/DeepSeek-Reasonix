#!/usr/bin/env bash
# env_check.sh — Quick environment check for minimax-docx skill
# Checks dotnet and python3 availability (fast, no installation)
# Outputs "READY" or "NOT READY" with details
# Usage: bash env_check.sh

set -euo pipefail

errors=0
details=()

# ── 1. Check dotnet SDK 8+ ───────────────────────────────────────────────────
if command -v dotnet &>/dev/null; then
    DOTNET_VERSION="$(dotnet --version 2>/dev/null || echo "0")"
    DOTNET_MAJOR="${DOTNET_VERSION%%.*}"
    if [ "$DOTNET_MAJOR" -ge 8 ]; then
        details+=("dotnet=$DOTNET_VERSION")
    else
        details+=("dotnet=$DOTNET_VERSION (need 8+)")
        errors=$((errors + 1))
    fi
else
    details+=("dotnet=MISSING")
    errors=$((errors + 1))
fi

# ── 2. Check Python 3 ────────────────────────────────────────────────────────
if command -v python3 &>/dev/null; then
    PY_VERSION="$(python3 -c 'import sys; print(sys.version.split()[0])' 2>/dev/null || echo "unknown")"
    details+=("python3=$PY_VERSION")
elif command -v python &>/dev/null; then
    PY_VERSION="$(python -c 'import sys; print(sys.version.split()[0])' 2>/dev/null || echo "unknown")"
    case "$PY_VERSION" in
        3.*) details+=("python=$PY_VERSION") ;;
        *)   details+=("python=$PY_VERSION (need 3+)"); errors=$((errors + 1)) ;;
    esac
else
    details+=("python=MISSING")
    errors=$((errors + 1))
fi

# ── Output ───────────────────────────────────────────────────────────────────
DETAIL_STR="$(IFS=','; echo "${details[*]}")"

if [ "$errors" -eq 0 ]; then
    echo "READY ($DETAIL_STR)"
else
    echo "NOT READY ($DETAIL_STR)"
    exit 1
fi

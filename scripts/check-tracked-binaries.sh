#!/usr/bin/env bash
# Guard against committing compiled artifacts to the repo.
# Mirrors the .gitignore contract so CI and `make hooks` enforce the same rule.
# Usage: ./scripts/check-tracked-binaries.sh
set -euo pipefail
cd "$(dirname "$0")/.."

pattern='\.(exe|dll|so|dylib)$|\.test$'
bad="$(git ls-files | grep -E "$pattern" || true)"
if [ -n "$bad" ]; then
  echo "error: tracked binary artifacts detected; run 'git rm --cached <file>' and extend .gitignore:" >&2
  printf '%s\n' "$bad" | sed 's/^/  /' >&2
  exit 1
fi
echo "ok: no tracked binaries."
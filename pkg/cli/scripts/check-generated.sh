#!/usr/bin/env bash
set -euo pipefail

generated="docs/generated"
snapshot="$(mktemp -d)"
trap 'rm -rf "${snapshot}"' EXIT

cp -R "${generated}/." "${snapshot}/"
GOWORK=off go run ./cmd/generate-reference
diff -ru "${snapshot}" "${generated}"

python3 -m json.tool "${generated}/manifest.json" >/dev/null
bash -n "${generated}/tool.bash"
zsh -n "${generated}/_tool"

if rg -n '\beval\b|Invoke-Expression' \
  "${generated}/tool.bash" "${generated}/_tool" \
  "${generated}/tool.fish" "${generated}/tool.ps1"; then
  echo "generated completion evaluates untrusted shell input" >&2
  exit 1
fi

if command -v shellcheck >/dev/null 2>&1; then
  shellcheck -s bash "${generated}/tool.bash"
fi

if command -v fish >/dev/null 2>&1; then
  fish -n "${generated}/tool.fish"
fi

if command -v pwsh >/dev/null 2>&1; then
  pwsh -NoProfile -NonInteractive -Command \
    "[scriptblock]::Create((Get-Content -Raw '${generated}/tool.ps1')) | Out-Null"
fi

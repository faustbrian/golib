#!/usr/bin/env bash
set -euo pipefail

required=(
  README.md CHANGELOG.md SECURITY.md CONTRIBUTING.md CODE_OF_CONDUCT.md
  SUPPORT.md LICENSE docs/api.md docs/architecture.md docs/security.md
  docs/aws-kms.md docs/keyring.md docs/compatibility.md
)
for path in "${required[@]}"; do
  test -s "$path"
done

while IFS=: read -r source match; do
  link="$(sed -E 's/.*\(([^)]+)\)/\1/' <<<"$match")"
  link="${link%%#*}"
  [[ -z "$link" || "$link" == http://* || "$link" == https://* ]] && continue
  target="$(dirname "$source")/$link"
  test -e "$target" || {
    echo "broken local documentation link: $source -> $link" >&2
    exit 1
  }
done < <(rg -o --with-filename '\[[^]]+\]\([^)]+\)' README.md docs)

while IFS= read -r package; do
  GOWORK=off go doc "$package" >/dev/null
done < <(GOWORK=off go list ./...)

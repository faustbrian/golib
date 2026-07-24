#!/usr/bin/env bash
set -euo pipefail

required=(
  README.md CHANGELOG.md SECURITY.md CONTRIBUTING.md CODE_OF_CONDUCT.md
  docs/adoption.md docs/api.md docs/architecture.md docs/compatibility.md
  docs/cookbook.md docs/faq.md docs/hardening.md docs/matching.md
  docs/limits.md docs/matrices.md docs/migration.md docs/performance.md
  docs/quickstart.md docs/release.md docs/security.md docs/semantics.md
  docs/troubleshooting.md
)
for path in "${required[@]}"; do
  test -s "$path"
done

while IFS=: read -r source match; do
  link="$(sed -E 's/.*\(([^)]+)\)/\1/' <<<"$match")"
  link="${link%%#*}"
  [[ -z "$link" || "$link" == http://* || "$link" == https://* ]] && continue
  target="$(dirname "$source")/$link"
  test -e "$target" || { echo "broken local documentation link: $source -> $link" >&2; exit 1; }
done < <(rg -o --with-filename '\[[^]]+\]\([^)]+\)' README.md docs)

go test ./... -run '^Example' -count=1
go vet ./...

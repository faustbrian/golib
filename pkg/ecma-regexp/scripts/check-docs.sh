#!/usr/bin/env bash
set -euo pipefail

required=(
  README.md CHANGELOG.md
  docs/api.md docs/cookbook.md docs/faq.md docs/json-schema.md
  docs/migration.md docs/performance.md docs/replacement.md docs/security.md
  docs/support.md docs/syntax.md
  specification/README.md specification/manifest.json
  specification/conformance/decisions.tsv
  specification/conformance/differential.tsv
  specification/conformance/errata.tsv
  specification/conformance/features.tsv
  specification/conformance/requirements.tsv
  specification/conformance/test262.tsv
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

go test ./... -run '^Example' -count=1
go vet ./...

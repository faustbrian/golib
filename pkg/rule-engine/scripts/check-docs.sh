#!/usr/bin/env bash
set -euo pipefail

required=(
  README.md CHANGELOG.md SECURITY.md CONTRIBUTING.md CODE_OF_CONDUCT.md
  docs/quickstart.md docs/model.md docs/operators.md
  docs/types-and-coercion.md docs/compilation.md docs/evaluation.md
  docs/rule-sets.md docs/extensions.md docs/json-ast.md docs/limits.md
  docs/security.md docs/performance.md docs/migration.md
  docs/integration.md docs/cookbook.md docs/faq.md docs/compatibility.md
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

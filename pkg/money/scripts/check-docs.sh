#!/usr/bin/env bash
set -euo pipefail

required=(
  README.md SECURITY.md CHANGELOG.md CONTRIBUTING.md LICENSE
  docs/api.md docs/contexts-and-rounding.md docs/tax-and-discounts.md
  docs/allocations.md docs/formatting.md docs/persistence.md
  docs/php-migration.md docs/security.md docs/performance.md
  docs/cookbook.md docs/faq.md docs/compatibility.md docs/verification.md
)
for document in "${required[@]}"; do
  test -s "$document" || { echo "missing documentation: $document" >&2; exit 1; }
done
go list ./... | while IFS= read -r package; do
  go doc "$package" >/dev/null
done
if grep -R -n -E 'TODO|TBD|FIXME' README.md SECURITY.md docs; then
  echo 'unfinished documentation marker found' >&2
  exit 1
fi

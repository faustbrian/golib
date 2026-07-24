#!/usr/bin/env bash
set -euo pipefail

required=(
  README.md SECURITY.md CHANGELOG.md CONTRIBUTING.md
  docs/quickstart.md docs/reference.md docs/provenance.md
  docs/integrations.md docs/migration.md docs/operations.md
  docs/dataset-report.md docs/verification.md
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

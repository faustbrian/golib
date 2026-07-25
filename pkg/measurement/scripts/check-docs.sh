#!/usr/bin/env bash
set -euo pipefail

required=(
	README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md LICENSE
	docs/README.md docs/quickstart.md docs/api.md docs/exactness.md
	docs/dimensional-analysis.md docs/logistics-formulas.md docs/sources.md
	docs/serialization.md docs/cookbook.md docs/migration.md docs/security.md
	docs/performance.md docs/compatibility.md docs/faq.md
	docs/troubleshooting.md
)
for file in "${required[@]}"; do
	if [[ ! -s "$file" ]]; then
		printf 'required documentation is missing or empty: %s\n' "$file" >&2
		exit 1
	fi
done
if rg -n 'TODO|TBD|FIXME' README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md docs; then
	printf 'unfinished documentation marker found\n' >&2
	exit 1
fi
go test ./... -run '^Example'

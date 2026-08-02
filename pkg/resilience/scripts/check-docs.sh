#!/bin/sh
set -eu

required='README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md THIRD_PARTY_LICENSES.md docs/api.md docs/budgets.md docs/composition.md docs/design.md docs/errors.md docs/faq.md docs/hardening.md docs/kubernetes.md docs/migration.md docs/operations.md docs/performance.md docs/benchmarks/2026-08-02-darwin-arm64.md docs/benchmarks/raw/2026-08-02-darwin-arm64.txt docs/benchmarks/raw/2026-08-02-darwin-arm64-benchstat.txt'
for file in $required; do
    if [ ! -s "$file" ]; then
        echo "missing required documentation: $file" >&2
        exit 1
    fi
done
if grep -En 'TODO|TBD|FIXME' README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md docs/*.md; then
    echo 'unfinished documentation marker found' >&2
    exit 1
fi
GOWORK=off go test ./... -run '^Example' -count=1

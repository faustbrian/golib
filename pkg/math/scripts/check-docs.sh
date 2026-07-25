#!/bin/sh
set -eu

required='README.md CHANGELOG.md SECURITY.md docs/README.md docs/numeric-model.md docs/precision.md docs/conditions.md docs/serialization.md docs/security.md docs/performance.md docs/benchmark-baseline.md docs/hardening.md docs/migration.md docs/cookbook.md docs/compatibility.md docs/faq.md docs/troubleshooting.md docs/verification.md'
for file in $required; do
	test -s "$file" || { printf 'missing documentation: %s\n' "$file" >&2; exit 1; }
done
go test . -run '^Example' -count=1

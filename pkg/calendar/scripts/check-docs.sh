#!/usr/bin/env bash
set -euo pipefail

required=(
	README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md LICENSE
	docs/README.md docs/quickstart.md docs/api.md docs/arithmetic.md
	docs/gregorian-iso-vectors.md docs/timezone.md docs/timezone-corpus.md
	docs/business.md docs/exclusive-ranges.md docs/encoding.md
	docs/postgres.md docs/adapters.md docs/composition.md
	docs/holiday-datasets.md docs/versioning.md docs/carbon-migration.md
	docs/security.md docs/operations.md docs/performance.md
	docs/compatibility.md docs/troubleshooting.md docs/faq.md
	docs/hardening.md docs/release.md
)
for file in "${required[@]}"; do
	if [[ ! -s "$file" ]]; then
		printf 'required documentation is missing or empty: %s\n' "$file" >&2
		exit 1
	fi
done

go test ./scripts/check-markdown-links.go ./scripts/check-markdown-links_test.go
go run ./scripts/check-markdown-links.go

go test ./... -run '^Example'

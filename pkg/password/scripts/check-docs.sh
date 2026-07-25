#!/bin/sh
set -eu

required='README.md
SECURITY.md
CHANGELOG.md
LICENSE
docs/api.md
docs/parser-grammar.md
docs/laravel-migration.md
docs/database-upgrades.md
docs/concurrency.md
docs/kubernetes-sizing.md
docs/algorithm-selection.md
docs/secret-handling.md
docs/testing.md
docs/authentication-integration.md
docs/observability.md
docs/threat-model.md
docs/performance.md
docs/compatibility.md
docs/security-review.md
docs/faq.md
docs/troubleshooting.md'

printf '%s\n' "$required" | while IFS= read -r path; do
	if [ ! -s "$path" ]; then
		printf 'missing or empty documentation: %s\n' "$path" >&2
		exit 1
	fi
done

go run ./scripts/check-markdown-links.go
./scripts/check-api-docs.sh
go test -run '^Example' ./...

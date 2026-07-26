#!/usr/bin/env bash
set -euo pipefail

required=(
	README.md
	CHANGELOG.md
	CONTRIBUTING.md
	SECURITY.md
	LICENSE
	docs/README.md
	docs/api.md
	docs/architecture.md
	docs/guarantees.md
	docs/consumer.md
	docs/retry-dead-letter.md
	docs/transactions.md
	docs/replay.md
	docs/inspection.md
	docs/operations.md
	docs/security.md
)
for path in "${required[@]}"; do
	test -s "${path}" || {
		echo "missing required documentation: ${path}" >&2
		exit 1
	}
done

go doc .
go test ./... -run '^Example'
echo "required documentation and package docs are present"

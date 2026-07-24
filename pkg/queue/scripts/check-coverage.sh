#!/usr/bin/env bash
set -euo pipefail

profile=$(mktemp)
filtered=$(mktemp)
trap 'rm -f "$profile" "$filtered"' EXIT

go test -coverprofile="$profile" ./...
grep -Ev '/(cmd/semvercheck|examples|internal/testutil|mocks)/' "$profile" > "$filtered"

coverage=$(go tool cover -func="$filtered" | awk '/^total:/ {gsub("%", "", $3); print $3}')
printf 'production statement coverage: %s%%\n' "$coverage"

if [[ "$coverage" != "100.0" ]]; then
	printf 'meaningful production coverage must remain 100.0%%\n' >&2
	exit 1
fi

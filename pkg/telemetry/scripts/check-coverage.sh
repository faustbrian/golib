#!/bin/sh
set -eu

profile="${TMPDIR:-/tmp}/telemetry-coverage.out"
packages=$(go list ./... | grep -v '/examples/')

# shellcheck disable=SC2086
go test -coverprofile="$profile" $packages
coverage=$(go tool cover -func="$profile" | awk '/^total:/ { sub(/%$/, "", $3); print $3 }')

if [ "$coverage" != "100.0" ]; then
    printf 'statement coverage is %s%%; required 100.0%%\n' "$coverage" >&2
    exit 1
fi

printf 'statement coverage: %s%%\n' "$coverage"

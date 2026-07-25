#!/usr/bin/env bash
set -euo pipefail

: "${GREMLINS_VERSION:?GREMLINS_VERSION is required}"
output=$(go run \
	"github.com/go-gremlins/gremlins/cmd/gremlins@${GREMLINS_VERSION}" \
	unleash . \
	--integration \
	--exclude-files '.*(testdata|metaschema.go).*' \
	--workers 2 \
	--timeout-coefficient 60 \
	--threshold-efficacy 100 \
	--threshold-mcover 100 2>&1) || {
	printf '%s\n' "$output" >&2
	exit 1
}
printf '%s\n' "$output"
case "$output" in
	*"Lived: 0, Not covered: 0"*"Timed out: 0"*"Test efficacy: 100.00%"*"Mutator coverage: 100.00%"*) ;;
	*) echo 'mutation results do not satisfy the release gate' >&2; exit 1 ;;
esac

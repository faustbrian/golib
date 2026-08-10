#!/usr/bin/env bash
set -euo pipefail

: "${GREMLINS_VERSION:?GREMLINS_VERSION is required}"
cache=$(mktemp -d)
cleanup() {
	find "$cache" -depth -delete
}
trap cleanup EXIT
output=$(GOWORK=off GOCACHE="$cache" go run \
	"github.com/go-gremlins/gremlins/cmd/gremlins@${GREMLINS_VERSION}" \
	unleash . --integration --workers 2 --timeout-coefficient 10 \
	--exclude-files '^providers/' \
	--threshold-efficacy 100 --threshold-mcover 100 2>&1) || {
	printf '%s\n' "$output" >&2
	exit 1
}
printf '%s\n' "$output"
case "$output" in
	*"Lived: 0, Not covered: 0"*"Timed out: 0"*"Test efficacy: 100.00%"*"Mutator coverage: 100.00%"*) ;;
	*) echo 'mutation results do not satisfy the release gate' >&2; exit 1 ;;
esac

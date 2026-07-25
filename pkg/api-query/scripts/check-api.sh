#!/bin/sh
set -eu

: "${APIDIFF_VERSION:?APIDIFF_VERSION is required}"

run_apidiff() {
	if [ -n "${GOLIB_APIDIFF:-}" ]; then
		go exec-tool "$GOLIB_APIDIFF" "$@"
	else
		go run "golang.org/x/exp/cmd/apidiff@$APIDIFF_VERSION" "$@"
	fi
}

output=$(run_apidiff -m api/v1.txt github.com/faustbrian/golib/pkg/api-query)
if [ -n "$output" ]; then
	printf '%s\n' "$output"
	exit 1
fi

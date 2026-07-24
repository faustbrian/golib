#!/bin/sh
set -eu

: "${APIDIFF_VERSION:?APIDIFF_VERSION is required}"

output=$(go run "golang.org/x/exp/cmd/apidiff@$APIDIFF_VERSION" \
	-m api/v1.txt github.com/faustbrian/golib/pkg/api-query)
if [ -n "$output" ]; then
	printf '%s\n' "$output"
	exit 1
fi

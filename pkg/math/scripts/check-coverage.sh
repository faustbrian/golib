#!/bin/sh
set -eu

profile=$(mktemp)
trap 'rm -f "$profile"' EXIT
go test ./... -coverprofile="$profile" -count=1
coverage=$(go tool cover -func="$profile" | awk '/^total:/ {print $3}')
if [ "$coverage" != "100.0%" ]; then
	printf 'production statement coverage is %s; require 100.0%%\n' "$coverage" >&2
	exit 1
fi


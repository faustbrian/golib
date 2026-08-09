#!/bin/sh
set -eu

profile=$(mktemp "${TMPDIR:-/tmp}/search-opensearch-coverage.XXXXXX")
trap 'rm -f "$profile"' EXIT HUP INT TERM
go test -coverprofile="$profile" .
total=$(go tool cover -func="$profile" | awk '/^total:/ {print $3}')
if test "$total" != "100.0%"; then
	echo "coverage is $total; require 100.0%" >&2
	go tool cover -func="$profile" >&2
	exit 1
fi

#!/bin/sh
set -eu

profile=${TMPDIR:-/tmp}/webhook-coverage.out
go test ./... -coverprofile="$profile"
total=$(go tool cover -func="$profile" | awk '/^total:/ {print $3}')
if test "$total" != "100.0%"; then
    echo "production statement coverage is $total; require 100.0%" >&2
    go tool cover -func="$profile" >&2
    exit 1
fi
echo "production statement coverage: $total"

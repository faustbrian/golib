#!/bin/sh
set -eu

profile="$(mktemp)"
trap 'rm -f "$profile"' EXIT

go test ./... -coverprofile="$profile"
total="$(go tool cover -func="$profile" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')"

if [ "$total" != "100.0" ]; then
	printf 'meaningful statement coverage is %s%%; required 100.0%%\n' "$total" >&2
	exit 1
fi

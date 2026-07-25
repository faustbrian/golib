#!/bin/sh
set -eu

profile=$(mktemp "${TMPDIR:-/tmp}/breaker-cover.XXXXXX")
trap 'rm -f "$profile"' EXIT HUP INT TERM

go test -covermode=atomic -coverprofile="$profile" ./...
total=$(go tool cover -func="$profile" | awk '/^total:/ { sub(/%$/, "", $3); print $3 }')

if [ "$total" != "100.0" ]; then
	echo "statement coverage is ${total}%; want 100.0%" >&2
	exit 1
fi

echo "statement coverage: ${total}%"

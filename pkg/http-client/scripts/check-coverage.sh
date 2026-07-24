#!/usr/bin/env sh

set -eu

profile="$(mktemp)"
trap 'rm -f "$profile"' EXIT HUP INT TERM

go test -covermode=atomic -coverprofile="$profile" ./...
coverage="$(go tool cover -func="$profile" | awk '/^total:/ { print $3 }')"

if [ "$coverage" != "100.0%" ]; then
	echo "production coverage is $coverage; want 100.0%" >&2
	exit 1
fi

echo "production coverage: $coverage"

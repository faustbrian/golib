#!/bin/sh
set -eu

profile="$(mktemp)"
trap 'rm -f "$profile"' EXIT HUP INT TERM
GOWORK=off go test -covermode=atomic -coverprofile="$profile" ./...
coverage="$(GOWORK=off go tool cover -func="$profile" | awk '/^total:/ {gsub("%", "", $3); print $3}')"
if [ "$coverage" != "100.0" ]; then
    echo "production statement coverage is $coverage%, want 100.0%" >&2
    exit 1
fi
echo 'meaningful production statement coverage: 100.0%'

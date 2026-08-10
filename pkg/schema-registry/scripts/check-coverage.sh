#!/usr/bin/env bash
set -euo pipefail

cache=$(mktemp -d)
profile=$(mktemp)
cleanup() {
	find "$cache" -depth -delete
	find "$profile" -delete
}
trap cleanup EXIT
GOWORK=off GOCACHE="$cache" go test -covermode=atomic -coverprofile="$profile" ./...
coverage=$(go tool cover -func="$profile" | awk '/^total:/ {print $3}')
printf 'production statement coverage: %s\n' "$coverage"
test "$coverage" = "100.0%"

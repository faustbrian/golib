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
./scripts/assert-exact-coverage.sh "$profile"

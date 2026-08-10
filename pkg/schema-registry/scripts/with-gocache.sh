#!/usr/bin/env bash
set -euo pipefail

cache=$(mktemp -d)
cleanup() {
	find "$cache" -depth -delete
}
trap cleanup EXIT
GOWORK=off GOCACHE="$cache" "$@"

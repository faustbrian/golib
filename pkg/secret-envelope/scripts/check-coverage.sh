#!/usr/bin/env bash
set -euo pipefail

temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT

GOWORK=off go test ./... \
  -coverprofile="$temporary_directory/coverage.out" \
  -count=1
coverage="$(
  GOWORK=off go tool cover \
    -func="$temporary_directory/coverage.out" |
    awk '/^total:/ {print $3}'
)"
if [[ "$coverage" != "100.0%" ]]; then
  echo "production statement coverage is $coverage, want 100.0%" >&2
  exit 1
fi

#!/usr/bin/env bash
set -euo pipefail

profile="${TMPDIR:-/tmp}/postgres-coverage.out"
packages="$(go list ./... | grep -v '/examples/' | paste -sd, -)"
go test -tags=integration -covermode=atomic -coverpkg="$packages" \
  -coverprofile="$profile" ./...

coverage="$({ go tool cover -func="$profile"; } | awk '/^total:/ {print $3}')"
if [[ "$coverage" != "100.0%" ]]; then
  echo "production code coverage is $coverage; required: 100.0%" >&2
  exit 1
fi

echo "production code coverage: 100.0%"

#!/usr/bin/env bash
set -euo pipefail

profile="${TMPDIR:-/tmp}/jsonrpc-coverage.out"
go test -covermode=atomic -coverprofile="$profile" .

coverage="$({ go tool cover -func="$profile"; } | awk '/^total:/ {print $3}')"
if [[ "$coverage" != "100.0%" ]]; then
  echo "production code coverage is $coverage; required: 100.0%" >&2
  exit 1
fi

echo "production code coverage: 100.0%"

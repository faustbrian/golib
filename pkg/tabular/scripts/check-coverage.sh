#!/usr/bin/env bash
set -euo pipefail

profile="${1:-/tmp/tabular-coverage.out}"
go test -coverpkg=./... -coverprofile="${profile}" ./...
total="$(go tool cover -func="${profile}" | awk '/^total:/ { print $3 }')"

if [[ "${total}" != "100.0%" ]]; then
    echo "production statement coverage is ${total}; expected 100.0%" >&2
    exit 1
fi

echo "production statement coverage: ${total}"

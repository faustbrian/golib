#!/usr/bin/env bash
set -euo pipefail

matches="$(
  find . -type f -name '*.go' ! -name '*_test.go' \
    ! -path '*/.git/*' ! -path '*/vendor/*' \
    -exec grep -nHE '"unsafe"|//go:linkname|^import "C"$' {} + || true
)"

if [[ -n "$matches" ]]; then
  printf '%s\n' "forbidden low-level Go feature detected:" "$matches" >&2
  exit 1
fi

echo "production Go code does not use unsafe, cgo, or go:linkname"

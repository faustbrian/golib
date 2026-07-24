#!/usr/bin/env bash
set -euo pipefail

matches="$({
  find . -type f -name '*.go' ! -name '*_test.go' \
    ! -path '*/.git/*' ! -path '*/vendor/*' \
    -exec grep -nHE '"unsafe"|//go:linkname|^import "C"$' {} + || true
})"

if [[ -n "$matches" ]]; then
  printf '%s\n' "GO-SAFETY-1 violation:" "$matches" >&2
  exit 1
fi

echo "GO-SAFETY-1: production Go code has no unsafe, cgo, or go:linkname"

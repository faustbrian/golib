#!/usr/bin/env bash
set -euo pipefail

if rg -n --glob '*.go' --glob '!**/*_test.go' \
  '(^|[^[:alnum:]_])(unsafe|C)\.|go:linkname|func init\s*\(' .; then
  echo "forbidden production mechanism found" >&2
  exit 1
fi
if rg -n --glob '*.go' --glob '!**/*_test.go' \
  '^[[:space:]]*go[[:space:]]+' .; then
  echo "production background goroutine found" >&2
  exit 1
fi

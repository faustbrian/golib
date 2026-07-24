#!/usr/bin/env bash
set -euo pipefail

if rg -n --glob '*.go' '(^|[^[:alnum:]_])(unsafe|C)\.|go:linkname|func init\s*\(' .; then
  echo "forbidden production mechanism found" >&2
  exit 1
fi
if rg -n --glob '*.go' --glob '!**/*_test.go' \
  'reflect\.|runtime\.FuncForPC|plugin\.Open|os/exec|database/sql|net/http'; then
  echo "reflection, dynamic loading, hidden I/O, or model discovery found" >&2
  exit 1
fi
if rg -n --glob '*.go' --glob '!**/*_test.go' \
  '^[[:space:]]*go[[:space:]]+' .; then
  echo "production background goroutine found" >&2
  exit 1
fi

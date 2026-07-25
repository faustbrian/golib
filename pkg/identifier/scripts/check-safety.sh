#!/usr/bin/env bash
set -euo pipefail

if rg -n --glob '*.go' --glob '!**/*_test.go' \
  '(^|[^[:alnum:]_])(unsafe|C)\.|go:linkname|func init\s*\(' .; then
  echo "forbidden production mechanism found" >&2
  exit 1
fi
if rg -n --glob '*.go' --glob '!**/*_test.go' \
  '"math/rand|reflect\.|runtime\.' .; then
  echo "weak randomness or runtime discovery found" >&2
  exit 1
fi
if rg -n --glob '*.go' --glob '!**/*_test.go' \
  '^var [[:alnum:]_]+[[:space:]]*=.*Generator' .; then
  echo "ambient package-global generator found" >&2
  exit 1
fi

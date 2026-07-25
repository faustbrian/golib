#!/usr/bin/env bash
set -euo pipefail

if rg -n --glob '*.go' \
  '(^|[^[:alnum:]_])(unsafe|C)\.|go:linkname|func init\s*\(' .; then
  echo "forbidden production mechanism found" >&2
  exit 1
fi
if rg -n --glob '*.go' --glob '!**/*_test.go' \
  'reflect\.(TypeOf|VisibleFields)|runtime\.FuncForPC' .; then
  echo "reflection-driven discovery is forbidden" >&2
  exit 1
fi
if rg -n --glob '*.go' --glob '!**/*_test.go' \
  '^[[:space:]]*go[[:space:]]+' .; then
  echo "production background goroutines are forbidden" >&2
  exit 1
fi
if rg -n --glob '*.go' --glob '!**/*_test.go' \
  'http\.(DefaultServeMux|Handle|HandleFunc)[[:space:]]*\(' .; then
  echo "process-global HTTP registration is forbidden" >&2
  exit 1
fi

#!/usr/bin/env bash
set -euo pipefail

if rg -n --glob '*.go' --glob '!**/*_test.go' \
  '(^|[^[:alnum:]_])(unsafe|C)\.|go:linkname|func init\s*\(' .; then
  echo "forbidden production mechanism found" >&2
  exit 1
fi
if rg -n --glob '*.go' --glob '!**/*_test.go' \
  '"math/rand|runtime\.|os\.Setenv|credentials\.NewStaticCredentialsProvider' .; then
  echo "unsafe entropy, runtime, environment, or credential mechanism found" >&2
  exit 1
fi
if rg -n --glob '*.go' --glob '!**/*_test.go' \
  'fmt\.(Print|Printf|Println)|log\.(Print|Printf|Println)|slog\.(Debug|Info|Warn|Error|Log)\(' .; then
  echo "production diagnostic output requires explicit review" >&2
  exit 1
fi

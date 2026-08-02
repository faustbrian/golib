#!/usr/bin/env bash
set -euo pipefail

temporary="$(mktemp)"
report="$(mktemp)"
trap 'rm -f "$temporary" "$report"' EXIT
version='v0.0.0-20260709172345-9ea1abe57597'

run_apidiff() {
  if [ -n "${GOLIB_APIDIFF:-}" ]; then
    go exec-tool "$GOLIB_APIDIFF" "$@"
  else
    go run "golang.org/x/exp/cmd/apidiff@$version" "$@"
  fi
}

run_apidiff -m -w "$temporary" \
  github.com/faustbrian/golib/pkg/money
run_apidiff -m -incompatible \
  api/v1.txt "$temporary" > "$report"
test ! -s "$report" || {
  cat "$report" >&2
  exit 1
}

#!/usr/bin/env bash
set -euo pipefail

temporary="$(mktemp)"
report="$(mktemp)"
trap 'rm -f "$temporary" "$report"' EXIT
version='v0.0.0-20260709172345-9ea1abe57597'
go run "golang.org/x/exp/cmd/apidiff@$version" -m -w "$temporary" \
  github.com/faustbrian/golib/pkg/international
go run "golang.org/x/exp/cmd/apidiff@$version" -m -incompatible \
  api/v1.txt "$temporary" > "$report"
test ! -s "$report" || {
  cat "$report" >&2
  exit 1
}

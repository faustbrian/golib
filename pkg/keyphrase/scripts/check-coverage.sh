#!/bin/sh
set -eu

profile=$(mktemp "${TMPDIR:-/tmp}/keyphrase-coverage.XXXXXX")
report=$(mktemp "${TMPDIR:-/tmp}/keyphrase-coverage-report.XXXXXX")
trap 'rm -f "$profile" "$report"' EXIT HUP INT TERM

go test -covermode=atomic -coverpkg=./... -coverprofile="$profile" ./... | tee "$report"
go tool cover -func="$profile" | tee -a "$report"

awk '$1 == "total:" { gsub(/%/, "", $3); found=1; if (($3 + 0) != 100) exit 1 } END { if (!found) exit 1 }' "$report" || {
    printf '%s\n' 'production statement coverage must be exactly 100.0%' >&2
    exit 1
}

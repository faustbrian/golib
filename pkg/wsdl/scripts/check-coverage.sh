#!/usr/bin/env bash
set -euo pipefail

minimum="${COVERAGE_MIN:-100.0}"
output="${COVERAGE_OUTPUT:-coverage.out}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
raw="$work/raw.cover"

go test -count=1 -coverpkg=./... -coverprofile="$raw" ./...
awk '
  NR == 1 { mode = $0; next }
  {
    statements[$1] = $2
    counts[$1] += $3
  }
  END {
    print mode
    for (block in statements) {
      print block, statements[block], counts[block]
    }
  }
' "$raw" >"$output"
actual="$(go tool cover -func="$output" | awk '/^total:/ {gsub("%", "", $3); print $3}')"
uncovered="$(awk 'NR > 1 && $3 == 0 { statements += $2 } END { print statements + 0 }' "$output")"
if [[ "$uncovered" -ne 0 ]]; then
  printf '%s uncovered statements remain (reported coverage %s%%)\n' \
    "$uncovered" "$actual" >&2
  exit 1
fi
awk -v actual="$actual" -v minimum="$minimum" 'BEGIN {
  if (actual + 0 < minimum + 0) {
    printf "coverage %.1f%% is below %.1f%%\n", actual, minimum > "/dev/stderr"
    exit 1
  }
}'

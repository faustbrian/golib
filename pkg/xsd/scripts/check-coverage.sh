#!/usr/bin/env bash
set -euo pipefail

output="${COVERAGE_OUTPUT:-coverage.out}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
root="$(./scripts/prepare-xsts.sh "$work")"
raw="$work/raw.cover"

XSTS_ROOT="$root" go test -count=1 -coverpkg=./... -coverprofile="$raw" ./...
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
awk '
  BEGIN {
    expected["github.com/faustbrian/golib/pkg/xsd"] = 1
    expected["github.com/faustbrian/golib/pkg/xsd/builder"] = 1
    expected["github.com/faustbrian/golib/pkg/xsd/compile"] = 1
    expected["github.com/faustbrian/golib/pkg/xsd/datatype"] = 1
    expected["github.com/faustbrian/golib/pkg/xsd/resolve"] = 1
    expected["github.com/faustbrian/golib/pkg/xsd/validate"] = 1
  }
  FNR == 1 { next }
  {
    split($1, location, ":")
    package = location[1]
    sub("/[^/]+$", "", package)
    total[package] += $2
    if (($3 + 0) > 0) covered[package] += $2
  }
  END {
    failed = 0
    for (package in expected) {
      printf "%s %d/%d statements\n", package, covered[package], total[package]
      if (total[package] == 0 || covered[package] != total[package]) failed = 1
    }
    exit failed
  }
' "$output" | sort

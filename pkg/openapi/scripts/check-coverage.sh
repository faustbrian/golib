#!/bin/sh
set -eu

minimum_package=${MIN_PACKAGE_COVERAGE:-100}
minimum_total=${MIN_TOTAL_COVERAGE:-100}

awk -v package="$minimum_package" -v total="$minimum_total" 'BEGIN {
  if ((package + 0) < 100) {
    print "MIN_PACKAGE_COVERAGE must be at least 100" > "/dev/stderr"
    exit 1
  }
  if ((total + 0) < 100) {
    print "MIN_TOTAL_COVERAGE must be at least 100" > "/dev/stderr"
    exit 1
  }
}'

profile=$(mktemp "${TMPDIR:-/tmp}/openapi-coverage.XXXXXX")
output=$(mktemp "${TMPDIR:-/tmp}/openapi-coverage-output.XXXXXX")
trap 'rm -f "$profile" "$output"' EXIT HUP INT TERM

go test -coverprofile="$profile" ./... | tee "$output"

awk -v minimum="$minimum_package" '
  /coverage:/ {
    for (field = 1; field <= NF; field++) {
      if ($field == "coverage:") {
        value = $(field + 1)
        sub(/%$/, "", value)
        if ((value + 0) < (minimum + 0)) {
          printf "package coverage %.1f%% is below %.1f%%: %s\n", value, minimum, $0 > "/dev/stderr"
          failed = 1
        }
      }
    }
  }
  END { exit failed }
' "$output"

total=$(go tool cover -func="$profile" | awk '/^total:/ { sub(/%$/, "", $3); print $3 }')
awk -v value="$total" -v minimum="$minimum_total" 'BEGIN {
  if ((value + 0) < (minimum + 0)) {
    printf "total coverage %.1f%% is below %.1f%%\n", value, minimum > "/dev/stderr"
    exit 1
  }
  printf "total coverage: %.1f%% (minimum %.1f%%)\n", value, minimum
}'

#!/bin/sh
set -eu

profile="${TMPDIR:-/tmp}/knapsack-coverage.out"
packages="$(go list ./... | grep -v '/knapsacktest$' | paste -sd, -)"
go test ./... -coverpkg="$packages" -coverprofile="$profile" -count=1
total="$(go tool cover -func="$profile" | awk '/^total:/ {gsub("%", "", $3); print $3}')"
test "$total" = "100.0" || {
	printf 'meaningful production statement coverage is %s%%, want 100.0%%\n' "$total" >&2
	exit 1
}

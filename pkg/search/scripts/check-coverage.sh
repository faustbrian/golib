#!/bin/sh
set -eu

for package in . ./searchtest
do
	profile=$(mktemp "${TMPDIR:-/tmp}/search-coverage.XXXXXX")
	go test -coverprofile="$profile" "$package"
	total=$(go tool cover -func="$profile" | awk '/^total:/ {print $3}')
	if test "$total" != "100.0%"; then
		echo "coverage for $package is $total; require 100.0%" >&2
		go tool cover -func="$profile" >&2
		rm -f "$profile"
		exit 1
	fi
	rm -f "$profile"
done

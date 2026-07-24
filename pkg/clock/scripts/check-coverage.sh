#!/bin/sh
set -eu

rm -f coverage.out
for package in . ./manual; do
    profile=$(mktemp)
    go test -covermode=atomic -coverprofile="$profile" "$package"
    coverage=$(go tool cover -func="$profile" | awk '/^total:/ {gsub("%", "", $3); print $3}')
    if [ "$coverage" != "100.0" ]; then
        echo "production statement coverage for $package is $coverage%, want 100.0%" >&2
        rm -f "$profile"
        exit 1
    fi
    if [ ! -f coverage.out ]; then
        cp "$profile" coverage.out
    else
        tail -n +2 "$profile" >> coverage.out
    fi
    rm -f "$profile"
done

go test ./clocktest
echo "meaningful production statement coverage: 100.0%"

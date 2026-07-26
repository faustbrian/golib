#!/usr/bin/env bash
set -euo pipefail

profile="${1:-coverage.out}"
filtered="${profile}.filtered"
trap 'rm -f "$profile" "$filtered"' EXIT

go test -coverprofile="$profile" ./...
grep -v '/localizedtest/' "$profile" > "$filtered"
coverage="$({ go tool cover -func="$filtered"; } | awk '$1 == "total:" {
    sub(/%$/, "", $3)
    print $3
}')"

if [[ "$coverage" != "100.0" ]]; then
    echo "meaningful production statement coverage is ${coverage}%, want 100.0%" >&2
    exit 1
fi

echo "meaningful production statement coverage: 100.0%"

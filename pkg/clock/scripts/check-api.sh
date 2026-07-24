#!/bin/sh
set -eu

snapshot=$(mktemp)
trap 'rm -f "$snapshot"' EXIT
{
    go doc -all .
    go doc -all ./manual
    go doc -all ./clocktest
} | sed -e '${/^$/d;}' > "$snapshot"

if [ "${UPDATE_API:-0}" = "1" ]; then
    mkdir -p api
    cp "$snapshot" api/v1.txt
    exit 0
fi

if ! cmp -s api/v1.txt "$snapshot"; then
    echo "exported API differs from api/v1.txt; review and regenerate intentionally" >&2
    diff -u api/v1.txt "$snapshot" || true
    exit 1
fi

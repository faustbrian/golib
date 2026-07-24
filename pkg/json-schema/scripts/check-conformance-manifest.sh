#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
expected="$root/specification/official-suite-results.tsv"
actual=$(mktemp "${TMPDIR:-/tmp}/json-schema-results.XXXXXX")
trap 'rm -f "$actual"' EXIT HUP INT TERM

"$root/scripts/generate-conformance-manifest.sh" "$actual"
cmp "$expected" "$actual"

files=$(($(wc -l <"$expected") - 1))
cases=$(awk -F '\t' 'NR > 1 { total += $5 } END { print total + 0 }' "$expected")
printf '%s\n' "official conformance manifest verified ($files files, $cases cases)"

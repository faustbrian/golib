#!/usr/bin/env bash
set -euo pipefail

budget="${1:-10000x}"
targets="$(
	sed -nE \
		's/^func (Fuzz[A-Za-z0-9_]+)\([A-Za-z_][A-Za-z0-9_]* \*testing\.F\).*/\1/p' \
		./*_test.go
)"
count=0
while IFS= read -r target; do
	[[ -n "${target}" ]] || continue
	go test . -run '^$' -fuzz "^${target}$" -fuzztime="${budget}" -parallel=2
	count=$((count + 1))
done <<<"${targets}"

if [[ "${count}" -eq 0 ]]; then
	echo "no fuzz targets executed" >&2
	exit 1
fi
echo "executed ${count} fuzz targets"

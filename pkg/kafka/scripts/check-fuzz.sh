#!/usr/bin/env bash
set -euo pipefail

budget="${1:-10000x}"
count=0
while IFS=$'\t' read -r package directory; do
	[[ -n "${package}" && -n "${directory}" ]] || continue
	targets="$(
		sed -nE \
			's/^func (Fuzz[A-Za-z0-9_]+)\([A-Za-z_][A-Za-z0-9_]* \*testing\.F\).*/\1/p' \
			"${directory}"/*_test.go
	)"
	while IFS= read -r target; do
		[[ -n "${target}" ]] || continue
		go test "${package}" -run '^$' -fuzz "^${target}$" \
			-fuzztime="${budget}" -parallel=2
		count=$((count + 1))
	done <<<"${targets}"
done < <(go list -f '{{.ImportPath}}{{"\t"}}{{.Dir}}' ./...)

if [[ "${count}" -eq 0 ]]; then
	echo "no fuzz targets executed" >&2
	exit 1
fi
echo "executed ${count} fuzz targets"

#!/usr/bin/env bash
set -euo pipefail

duration=${1:-2s}
targets=$(go test . -list '^Fuzz' | awk '/^Fuzz/ {print $1}')
if [[ -z "$targets" ]]; then
	printf 'no fuzz targets found\n' >&2
	exit 1
fi
while IFS= read -r target; do
	go test . -run '^$' -fuzz "^${target}$" -fuzztime="$duration"
done <<< "$targets"

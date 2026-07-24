#!/usr/bin/env bash
set -euo pipefail

duration=${1:-2s}
targets=$(go test ./... -list '^Fuzz' | awk '/^Fuzz/ {print $1}')
if [[ -z "$targets" ]]; then
	printf 'no fuzz targets found\n' >&2
	exit 1
fi

while IFS= read -r target; do
	file=
	while IFS= read -r candidate; do
		if grep -q -m1 "func ${target}(" "$candidate"; then
			file=$candidate
			break
		fi
	done < <(find . -type f -name '*_test.go' -print)
	if [[ -z "$file" ]]; then
		printf 'fuzz target source not found: %s\n' "$target" >&2
		exit 1
	fi
	package=$(dirname "$file")
	if [[ "$package" == "." ]]; then
		package=.
	else
		package=./$package
	fi
	go test "$package" -run '^$' -fuzz "^${target}$" -fuzztime="$duration"
done <<< "$targets"

#!/usr/bin/env bash
set -euo pipefail

duration=${1:-2s}
targets=$(go test ./... -list '^Fuzz' | awk '/^Fuzz/ {print $1}')
if [[ -z "$targets" ]]; then
	echo 'no fuzz targets found' >&2
	exit 1
fi

while IFS= read -r target; do
	if command -v rg >/dev/null 2>&1; then
		file=$(rg -l -m1 "func ${target}\\(" --glob '*_test.go' .)
	else
		file=$(grep -R -l -m1 --include='*_test.go' "func ${target}(" . | sed -n '1p')
	fi
	package=$(dirname "$file")
	if [[ "$package" == "." ]]; then
		package=.
	else
		package=./$package
	fi
	go test "$package" -run '^$' -fuzz "^${target}$" -fuzztime="$duration"
done <<< "$targets"

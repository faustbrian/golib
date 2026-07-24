#!/usr/bin/env bash
set -euo pipefail

duration=${1:-2s}
while IFS= read -r package; do
	while IFS= read -r target; do
		[[ -z "$target" ]] && continue
		go test "$package" -run '^$' -fuzz="^${target}$" -fuzztime="$duration"
	done < <(go test "$package" -list '^Fuzz' | awk '/^Fuzz/ {print $1}')
done < <(go list ./...)

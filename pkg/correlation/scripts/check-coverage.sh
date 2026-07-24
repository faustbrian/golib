#!/usr/bin/env bash
set -euo pipefail

workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT

while IFS= read -r package; do
	name=${package//\//_}
	profile="$workspace/$name.out"
	go test -covermode=atomic -coverprofile="$profile" "$package" >/dev/null
	coverage=$(go tool cover -func="$profile" | awk '/^total:/ {print $3}')
	printf '%s production statement coverage: %s\n' "$package" "$coverage"
	if [[ "$coverage" != "100.0%" ]]; then
		echo "production coverage must remain 100.0% for $package" >&2
		exit 1
	fi
done < <(go list ./...)

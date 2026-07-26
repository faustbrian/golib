#!/bin/sh
set -eu

generator="${SYFT:?SYFT is required}"
directory="$(mktemp -d)"
trap 'rm -rf "$directory"' EXIT

generate() {
	# The command is a pinned repository-controlled value from Makefile.
	# shellcheck disable=SC2086
	SYFT_CHECK_FOR_APP_UPDATE=false $generator dir:. -o cyclonedx-json="$1" \
		--source-name knapsack --quiet
	jq 'del(.serialNumber, .metadata.timestamp) |
		walk(if type == "string" then sub("^.*/knapsack/"; "/") else . end)' \
		"$1" > "$1.normalized"
}

generate "$directory/first.json"
generate "$directory/second.json"
cmp "$directory/first.json.normalized" "$directory/second.json.normalized"

grep -q 'github.com/faustbrian/golib/pkg/knapsack' "$directory/first.json.normalized"
grep -q 'github.com/faustbrian/golib/pkg/math' "$directory/first.json.normalized"
grep -q 'github.com/faustbrian/golib/pkg/measurement' "$directory/first.json.normalized"
if grep -q '"timestamp"\|"serialNumber"' "$directory/first.json.normalized"; then
	printf '%s\n' 'SBOM contains nondeterministic timestamp or serial number' >&2
	exit 1
fi

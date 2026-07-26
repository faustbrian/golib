#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
checksums="$root/specification/official-suite.sha256"
symlinks="$root/specification/official-suite.symlinks"

if [ ! -s "$checksums" ]; then
	printf '%s\n' "missing official suite checksums" >&2
	exit 1
fi

cd "$root"
shasum -a 256 -c -s "$checksums"

actual_symlinks=$(mktemp "${TMPDIR:-/tmp}/json-schema-symlinks.XXXXXX")
trap 'rm -f "$actual_symlinks"' EXIT HUP INT TERM
find testdata/official/JSON-Schema-Test-Suite -type l -print \
	| LC_ALL=C sort \
	| while IFS= read -r path; do
		printf '%s\t%s\n' "$path" "$(readlink "$path")"
	done >"$actual_symlinks"
cmp -s "$symlinks" "$actual_symlinks"

expected_files=$(wc -l <"$checksums" | tr -d ' ')
expected_links=$(wc -l <"$symlinks" | tr -d ' ')
expected=$((expected_files + expected_links))
actual=$(find testdata/official/JSON-Schema-Test-Suite \
	\( -type f -o -type l \) | wc -l | tr -d ' ')

if [ "$actual" != "$expected" ]; then
	printf '%s\n' "official suite file count mismatch: expected $expected, got $actual" >&2
	exit 1
fi

printf '%s\n' "official suite provenance verified ($actual files)"

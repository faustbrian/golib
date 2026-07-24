#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
checksums="$root/specification/official-meta-schemas.sha256"
sources="$root/specification/official-meta-schemas.sources.tsv"

if [ ! -s "$checksums" ] || [ ! -s "$sources" ]; then
	printf '%s\n' "missing official meta-schema provenance" >&2
	exit 1
fi

cd "$root"
shasum -a 256 -c -s "$checksums"

expected=$(wc -l <"$checksums" | tr -d ' ')
source_count=$(wc -l <"$sources" | tr -d ' ')
actual=$(find testdata/official/meta-schemas -type f -name '*.json' \
	| wc -l | tr -d ' ')

if [ "$source_count" != "$expected" ] || [ "$actual" != "$expected" ]; then
	printf '%s\n' \
		"official meta-schema count mismatch: expected $expected, got $actual" >&2
	exit 1
fi

while IFS="$(printf '\t')" read -r uri path; do
	if [ -z "$uri" ] || [ ! -f "$path" ]; then
		printf '%s\n' "invalid official meta-schema source: $uri $path" >&2
		exit 1
	fi
done <"$sources"

printf '%s\n' "official meta-schema provenance verified ($actual files)"

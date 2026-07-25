#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$root/specification/official-suite.env"

target="$root/testdata/official/JSON-Schema-Test-Suite"
checksums="$root/specification/official-suite.sha256"
symlinks="$root/specification/official-suite.symlinks"

if [ -e "$target" ]; then
	printf '%s\n' "official suite already exists: $target" >&2
	exit 1
fi

staging=$(mktemp -d "${TMPDIR:-/tmp}/json-schema-suite.XXXXXX")
trap 'rm -rf "$staging"' EXIT HUP INT TERM

archive="$staging/suite.tar.gz"
url="https://github.com/json-schema-org/JSON-Schema-Test-Suite/archive/$SUITE_REVISION.tar.gz"

curl --fail --location --silent --show-error --output "$archive" "$url"
actual=$(shasum -a 256 "$archive" | awk '{print $1}')

if [ "$actual" != "$SUITE_ARCHIVE_SHA256" ]; then
	printf '%s\n' "archive checksum mismatch: got $actual" >&2
	exit 1
fi

mkdir -p "$(dirname -- "$target")"
tar -xzf "$archive" -C "$staging"
mv "$staging/JSON-Schema-Test-Suite-$SUITE_REVISION" "$target"

(
	cd "$root"
	find testdata/official/JSON-Schema-Test-Suite -type f -print \
		| LC_ALL=C sort \
		| xargs shasum -a 256
) >"$checksums"

(
	cd "$root"
	find testdata/official/JSON-Schema-Test-Suite -type l -print \
		| LC_ALL=C sort \
		| while IFS= read -r path; do
			printf '%s\t%s\n' "$path" "$(readlink "$path")"
		done
) >"$symlinks"

printf '%s\n' "imported JSON Schema Test Suite $SUITE_REVISION"

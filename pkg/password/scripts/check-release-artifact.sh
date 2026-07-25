#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
	printf 'usage: %s vMAJOR.MINOR.PATCH GIT_REF\n' "$0" >&2
	exit 2
fi

version=$1
ref=$2
temporary=$(mktemp "${TMPDIR:-/tmp}/password-release.XXXXXX")
trap 'rm -f "$temporary"' EXIT HUP INT TERM

./scripts/build-release.sh "$version" "$ref"
archive="dist/password-${version}.tar.gz"
cp "$archive" "$temporary"
./scripts/build-release.sh "$version" "$ref"
cmp "$temporary" "$archive"
shasum -a 256 -c "${archive}.sha256"

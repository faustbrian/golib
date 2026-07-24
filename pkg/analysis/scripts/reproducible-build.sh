#!/bin/sh

set -eu

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-build.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

cd "$root"
CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "$temporary/first" ./cmd/golib-analysis
CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "$temporary/second" ./cmd/golib-analysis

if ! cmp -s "$temporary/first" "$temporary/second"; then
	printf 'repeated release builds produced different binaries\n' >&2
	exit 1
fi

checksum=$(shasum -a 256 "$temporary/first" | awk '{print $1}')
printf '%s  golib-analysis\n' "$checksum"

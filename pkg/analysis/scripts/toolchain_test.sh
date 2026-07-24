#!/bin/sh

set -eu

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
subject="$root/scripts/toolchain.sh"
temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-toolchain-test.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

version_file="$temporary/.go-version"
printf '1.26.5\n' > "$version_file"
go_stub="$temporary/go"
printf '%s\n' '#!/bin/sh' 'printf "%s\n" "$STUB_GO_VERSION"' > "$go_stub"
chmod +x "$go_stub"

STUB_GO_VERSION=go1.26.5 \
TOOLCHAIN_VERSION_FILE="$version_file" \
TOOLCHAIN_GO="$go_stub" \
	"$subject" > "$temporary/stdout"
grep -q '^toolchain verified: go1.26.5$' "$temporary/stdout"

if STUB_GO_VERSION=go1.26.4 \
	TOOLCHAIN_VERSION_FILE="$version_file" \
	TOOLCHAIN_GO="$go_stub" \
	"$subject" > /dev/null 2> "$temporary/mismatch-stderr"; then
	printf 'toolchain gate accepted a mismatched Go version\n' >&2
	exit 1
fi
grep -q '^Go toolchain mismatch: have go1.26.4, require go1.26.5$' \
	"$temporary/mismatch-stderr"

printf 'latest\n' > "$version_file"
if STUB_GO_VERSION=golatest \
	TOOLCHAIN_VERSION_FILE="$version_file" \
	TOOLCHAIN_GO="$go_stub" \
	"$subject" > /dev/null 2>&1; then
	printf 'toolchain gate accepted an invalid version contract\n' >&2
	exit 1
fi

printf 'toolchain gate tests passed\n'

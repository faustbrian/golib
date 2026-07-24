#!/bin/sh

set -eu

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
version_file=${TOOLCHAIN_VERSION_FILE:-"$root/.go-version"}
go_command=${TOOLCHAIN_GO:-go}

if [ ! -f "$version_file" ]; then
	printf 'Go toolchain version file not found: %s\n' "$version_file" >&2
	exit 1
fi
required=$(cat "$version_file")
if ! printf '%s\n' "$required" | grep -E '^1\.[0-9]+\.[0-9]+$' >/dev/null ||
	[ "$(wc -l < "$version_file" | tr -d ' ')" -ne 1 ]; then
	printf 'Go toolchain version must be one stable patch release\n' >&2
	exit 1
fi
actual=$("$go_command" env GOVERSION)
if [ "$actual" != "go$required" ]; then
	printf 'Go toolchain mismatch: have %s, require go%s\n' \
		"$actual" "$required" >&2
	exit 1
fi

printf 'toolchain verified: %s\n' "$actual"

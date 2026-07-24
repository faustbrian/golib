#!/bin/sh
set -eu
# shellcheck disable=SC1091 # Repository-local pinned version manifest.
. ./tools/versions.env

expected=${GOLANGCI_LINT_VERSION#v}
actual=$(golangci-lint version | sed -n 's/.* has version \([^ ]*\) .*/\1/p')
if [ "$actual" != "$expected" ]; then
	printf 'golangci-lint version mismatch: expected %s, got %s\n' "$expected" "${actual:-unknown}" >&2
	exit 1
fi
golangci-lint run ./...

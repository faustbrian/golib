#!/usr/bin/env bash
set -euo pipefail

: "${APIDIFF_VERSION:?APIDIFF_VERSION is required}"

baseline=api/v1.export
current=$(mktemp)
trap 'rm -f "$current"' EXIT
tool="golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}"

go run "$tool" -m -w "$current" github.com/faustbrian/golib/pkg/openrpc

if [[ ! -s "$baseline" ]]; then
    echo "API compatibility baseline is missing: $baseline" >&2
    exit 1
fi

go run "$tool" -m -incompatible "$baseline" "$current"

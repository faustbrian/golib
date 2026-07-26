#!/usr/bin/env bash
set -euo pipefail

: "${APIDIFF_VERSION:?APIDIFF_VERSION is required}"

baseline=api/v1.export
current=$(mktemp)
trap 'rm -f "$current"' EXIT
tool="golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}"

run_apidiff() {
    if [[ -n "${GOLIB_APIDIFF:-}" ]]; then
        go exec-tool "${GOLIB_APIDIFF}" "$@"
    else
        go run "${tool}" "$@"
    fi
}

run_apidiff -m -w "$current" github.com/faustbrian/golib/pkg/openrpc

if [[ ! -s "$baseline" ]]; then
    echo "API compatibility baseline is missing: $baseline" >&2
    exit 1
fi

run_apidiff -m -incompatible "$baseline" "$current"

#!/usr/bin/env bash
set -euo pipefail

: "${APIDIFF_VERSION:?APIDIFF_VERSION is required}"

module="github.com/faustbrian/golib/pkg/authorization"
baseline="api/authorization.txt"
current="$(mktemp)"
report="$(mktemp)"
trap 'rm -f "${current}" "${report}"' EXIT

if [[ ! -f "${baseline}" ]]; then
    echo "missing API baseline: ${baseline}" >&2
    exit 1
fi

go run "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" \
    -m -w "${current}" "${module}"
go run "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" \
    -m -incompatible "${baseline}" "${current}" >"${report}"

if [[ -s "${report}" ]]; then
    echo "incompatible exported API changes:" >&2
    cat "${report}" >&2
    exit 1
fi

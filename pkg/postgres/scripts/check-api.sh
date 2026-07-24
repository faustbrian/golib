#!/usr/bin/env bash
set -euo pipefail

: "${APIDIFF_VERSION:?APIDIFF_VERSION is required}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
baseline="${root}/api/v1.export"
current="$(mktemp)"
trap 'rm -f "${current}"' EXIT
tool="golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}"

if [[ ! -s "${baseline}" ]]; then
	printf 'API compatibility baseline is missing: %s\n' "${baseline}" >&2
	exit 1
fi

cd "${root}"
GOWORK=off go run "${tool}" -m -w "${current}" \
	"github.com/faustbrian/golib/pkg/postgres"
GOWORK=off go run "${tool}" -m -incompatible "${baseline}" "${current}"

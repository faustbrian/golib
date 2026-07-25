#!/usr/bin/env bash
set -euo pipefail

: "${APIDIFF_VERSION:?APIDIFF_VERSION is required}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
baseline="${root}/api/v1.export"
current="$(mktemp)"
trap 'rm -f "${current}"' EXIT
tool="golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}"

run_apidiff() {
	if [[ -n "${GOLIB_APIDIFF:-}" ]]; then
		go exec-tool "${GOLIB_APIDIFF}" "$@"
	else
		go run "${tool}" "$@"
	fi
}

if [[ ! -s "${baseline}" ]]; then
	printf 'API compatibility baseline is missing: %s\n' "${baseline}" >&2
	exit 1
fi

cd "${root}"
GOWORK=off run_apidiff -m -w "${current}" \
	"github.com/faustbrian/golib/pkg/postgres"
GOWORK=off run_apidiff -m -incompatible "${baseline}" "${current}"

#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"

tool_dir="$(mktemp -d)"
current="$(mktemp)"
report="$(mktemp)"
trap 'rm -rf "${tool_dir}" "${current}" "${report}"' EXIT

version="v0.0.0-20260709172345-9ea1abe57597"
module="github.com/faustbrian/golib/pkg/queue-control-plane"
baseline="api/control-plane.api"

GOBIN="${tool_dir}" go install "golang.org/x/exp/cmd/apidiff@${version}"
"${tool_dir}/apidiff" -m -w "${current}" "${module}"

if [[ "${UPDATE_API_BASELINE:-0}" == "1" ]]; then
    cp "${current}" "${baseline}"
    exit 0
fi

if [[ ! -f "${baseline}" ]]; then
    printf 'API baseline is missing: %s\n' "${baseline}" >&2
    exit 1
fi

"${tool_dir}/apidiff" -m "${baseline}" "${current}" >"${report}"
if [[ -s "${report}" ]]; then
    printf 'Public Go API differs from the reviewed baseline:\n' >&2
    sed 's/^/  /' "${report}" >&2
    printf '%s\n' \
        'Review the report, then run UPDATE_API_BASELINE=1 make api-compatibility.' \
        >&2
    exit 1
fi

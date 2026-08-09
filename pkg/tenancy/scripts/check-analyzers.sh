#!/usr/bin/env bash
set -euo pipefail

module_directory="$(cd "$(dirname "$0")/.." && pwd)"
repository_root="$(git -C "${module_directory}" rev-parse --show-toplevel)"
temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/tenancy-analyzers.XXXXXX")"
cleanup() {
    find "${temporary_directory}" -depth -delete
}
trap cleanup EXIT HUP INT TERM

analyzer="${temporary_directory}/golib-analysis"
policy="${module_directory}/analysis.yml"
consumer_report="${temporary_directory}/consumer.json"
adapter_report="${temporary_directory}/adapter.json"

(
    cd "${repository_root}/pkg/analysis"
    GOWORK="${repository_root}/go.work" go build -o "${analyzer}" ./cmd/golib-analysis
)

"${analyzer}" validate-config "${policy}" >/dev/null

set +e
(
    cd "${repository_root}"
    GOWORK="${repository_root}/go.work" "${analyzer}" check \
        -config "${policy}" -root "${repository_root}" -format json \
        ./pkg/tenancy/testdata/analyzer/consumer
) >"${consumer_report}"
consumer_status=$?
set -e
if [[ ${consumer_status} -ne 1 ]]; then
    printf 'tenancy analyzer consumer exit status: got %d, want 1\n' "${consumer_status}" >&2
    exit 1
fi

assert_diagnostic_count() {
    local rule="$1"
    local expected="$2"
    local actual
    actual="$(rg -o "\\\"rule\\\":\\\"${rule}\\\"" "${consumer_report}" | wc -l | tr -d ' ')"
    if [[ "${actual}" != "${expected}" ]]; then
        printf '%s diagnostics: got %s, want %s\n' "${rule}" "${actual}" "${expected}" >&2
        exit 1
    fi
}

assert_diagnostic_count 'api/forbidden-call' 5
assert_diagnostic_count 'context/no-background' 1
assert_diagnostic_count 'observability/high-cardinality-label' 1

(
    cd "${repository_root}"
    GOWORK="${repository_root}/go.work" "${analyzer}" check \
        -config "${policy}" -root "${repository_root}" -format json \
        ./pkg/tenancy/testdata/analyzer/adapter
) >"${adapter_report}"
if ! rg -q '"diagnostics":\[\]' "${adapter_report}"; then
    printf 'reviewed tenancy adapter emitted analyzer diagnostics\n' >&2
    exit 1
fi

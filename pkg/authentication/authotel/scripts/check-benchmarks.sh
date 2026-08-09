#!/usr/bin/env bash
set -euo pipefail

bench_time="${BENCH_TIME:-1000x}"
bench_count="${BENCH_COUNT:-5}"
result="$(mktemp "${TMPDIR:-/tmp}/authotel-benchmark.XXXXXX")"
trap 'rm -f "${result}"' EXIT

go test ./... \
    -run '^$' \
    -bench '^BenchmarkAuthenticationInstrumentation$' \
    -benchmem \
    -benchtime="${bench_time}" \
    -count="${bench_count}" | tee "${result}"

median_field() {
    local mode="$1"
    local field="$2"
    awk -v mode="/${mode}-" -v field="${field}" \
        'index($1, mode) > 0 { print $field }' "${result}" |
        sort -n |
        awk '
        END {
            if (NR == 0) exit 1
            if (NR % 2 == 1) printf "%.6f\n", values[(NR + 1) / 2]
            else printf "%.6f\n", (values[NR / 2] + values[NR / 2 + 1]) / 2
        }
        { values[NR] = $1 }
    '
}

direct_latency="$(median_field direct 3)"

check_budget() {
    local mode="$1"
    local latency_multiple="$2"
    local allocation_limit="$3"
    local latency allocations

    latency="$(median_field "${mode}" 3)"
    allocations="$(median_field "${mode}" 7)"
    awk -v actual="${latency}" -v direct="${direct_latency}" \
        -v multiple="${latency_multiple}" \
        'BEGIN { if (actual > direct * multiple) exit 1 }' || {
        printf '%s exceeds the %.0fx direct latency budget\n' \
            "${mode}" "${latency_multiple}" >&2
        exit 1
    }
    awk -v actual="${allocations}" -v limit="${allocation_limit}" \
        'BEGIN { if (actual > limit) exit 1 }' || {
        printf '%s exceeds the %.0f allocation budget\n' \
            "${mode}" "${allocation_limit}" >&2
        exit 1
    }
    printf '%s median_ns=%s direct_multiple=%.2f median_allocs=%s\n' \
        "${mode}" "${latency}" \
        "$(awk -v actual="${latency}" -v direct="${direct_latency}" 'BEGIN { print actual / direct }')" \
        "${allocations}"
}

check_budget opentelemetry_noop 100 20
check_budget opentelemetry_sampled_out 150 22
check_budget opentelemetry_enabled 200 24

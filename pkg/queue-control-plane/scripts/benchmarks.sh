#!/usr/bin/env bash
set -euo pipefail

export GOMAXPROCS="${GOMAXPROCS:-1}"
count="${BENCH_COUNT:-5}"

run_benchmark() {
    local package="$1"
    local benchmark="$2"
    local max_bytes="$3"
    local max_allocs="$4"
    local output

    output="$(go test \
        -run '^$' \
        -bench "^${benchmark}$" \
        -benchmem \
        -count "${count}" \
        "${package}")"
    printf '%s\n' "${output}"
    awk \
        -v benchmark="${benchmark}" \
        -v max_bytes="${max_bytes}" \
        -v max_allocs="${max_allocs}" \
        '
        $1 ~ ("^" benchmark "(-[0-9]+)?$") {
            found = 1
            bytes = -1
            allocs = -1
            for (field = 2; field <= NF; field++) {
                if ($field == "B/op") {
                    bytes = $(field - 1)
                }
                if ($field == "allocs/op") {
                    allocs = $(field - 1)
                }
            }
            if (bytes < 0 || allocs < 0 || bytes > max_bytes || allocs > max_allocs) {
                printf "%s exceeds budget: %d B/op, %d allocs/op; want <= %d, <= %d\n", \
                    benchmark, bytes, allocs, max_bytes, max_allocs > "/dev/stderr"
                failed = 1
            }
        }
        END {
            if (!found) {
                printf "%s did not produce benchmark metrics\n", benchmark > "/dev/stderr"
                failed = 1
            }
            exit failed
        }
        ' <<<"${output}"
}

run_benchmark ./fleet BenchmarkRegistrySnapshotTenThousandWorkers 16000000 21000
run_benchmark ./fleet BenchmarkRegistryReconnectStormTenThousandWorkers 200000 11000
run_benchmark ./history BenchmarkVerifyOneHundredThousandAuditEvents 0 0
run_benchmark ./apihttp BenchmarkQueuePageTwoHundredMeasurements 65536 2
run_benchmark ./apihttp BenchmarkFailurePageTwoHundredRecords 150000 2
run_benchmark ./apihttp BenchmarkPrivilegedPayloadOneMebibyte 3000000 8
run_benchmark ./apihttp BenchmarkWorkerAPIThousandWorkerMaximumPage 10000000 7000
run_benchmark ./dataplane BenchmarkControllerDispatchBackendOutage 1024 2

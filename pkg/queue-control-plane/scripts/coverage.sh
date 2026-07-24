#!/usr/bin/env bash
set -euo pipefail

profile="$(mktemp)"
trap 'rm -f "${profile}"' EXIT

go test -count=1 -coverprofile="${profile}" ./...

awk '
    NR == 1 { next }
    {
        package = $1
        sub(/:.*/, "", package)
        sub("/[^/]+$", "", package)
        statements[package] += $2
        if ($3 > 0) {
            covered[package] += $2
        }
        total += $2
        if ($3 > 0) {
            total_covered += $2
        }
    }
    END {
        failed = 0
        for (package in statements) {
            if (covered[package] != statements[package]) {
                printf "%s statement coverage is %d/%d, want all statements\n", \
                    package, covered[package], statements[package] > "/dev/stderr"
                failed = 1
            }
        }
        if (total_covered != total) {
            printf "aggregate statement coverage is %d/%d, want all statements\n", \
                total_covered, total > "/dev/stderr"
            failed = 1
        }
        exit failed
    }
' "${profile}"

printf 'every package and aggregate statement coverage: 100.0%%\n'

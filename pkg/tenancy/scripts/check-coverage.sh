#!/usr/bin/env bash
set -euo pipefail

for package in . ./http ./jsonrpc ./postgres; do
    profile="$(mktemp "${TMPDIR:-/tmp}/tenancy-coverage.XXXXXX")"
    trap 'rm -f "${profile}"' EXIT
    GOWORK=off go test -coverprofile="${profile}" "${package}"
    total="$(GOWORK=off go tool cover -func="${profile}" | awk '/^total:/ { print $3 }')"
    if [[ "${total}" != "100.0%" ]]; then
        printf 'coverage for %s is %s, want 100.0%%\n' "${package}" "${total}" >&2
        exit 1
    fi
    rm -f "${profile}"
    trap - EXIT
done

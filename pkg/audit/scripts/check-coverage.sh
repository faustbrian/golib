#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/golib-audit-coverage.XXXXXX")"
cleanup() {
    case "${temporary}" in
        "${TMPDIR:-/tmp}"/golib-audit-coverage.*)
            find "${temporary}" -depth -delete
            ;;
    esac
}
trap cleanup EXIT HUP INT TERM

check() {
    local directory="$1"
    local tags="$2"
    local profile="$3"
    local covered
    local statements
    local uncovered

    cd "${directory}"
    if [[ -n "${tags}" ]]; then
        "${root}/scripts/with-gocache.sh" go test -tags="${tags}" -coverprofile="${profile}" ./...
    else
        "${root}/scripts/with-gocache.sh" go test -coverprofile="${profile}" ./...
    fi
    statements="$(awk 'NR > 1 { total += $2 } END { print total + 0 }' "${profile}")"
    covered="$(awk 'NR > 1 && $3 > 0 { total += $2 } END { print total + 0 }' "${profile}")"
    uncovered="$(awk 'NR > 1 && $2 > 0 && $3 == 0 { print; exit }' "${profile}")"
    if [[ "${covered}" != "${statements}" || -n "${uncovered}" ]]; then
        printf 'production coverage is %s/%s statements in %s; first uncovered block: %s\n' \
            "${covered}" "${statements}" "${directory}" "${uncovered}" >&2
        exit 1
    fi
    printf 'production coverage: %s=%s/%s statements\n' "${directory}" "${covered}" "${statements}"
}

check "${root}" "" "${temporary}/core.out"
check "${root}/postgres" "integration" "${temporary}/postgres.out"

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
    local coverage

    cd "${directory}"
    if [[ -n "${tags}" ]]; then
        "${root}/scripts/with-gocache.sh" go test -tags="${tags}" -coverprofile="${profile}" ./...
    else
        "${root}/scripts/with-gocache.sh" go test -coverprofile="${profile}" ./...
    fi
    coverage="$("${root}/scripts/with-gocache.sh" go tool cover -func="${profile}" | awk '/^total:/ {print $3}')"
    if [[ "${coverage}" != "100.0%" ]]; then
        printf 'production coverage is %s in %s, want 100.0%%\n' "${coverage}" "${directory}" >&2
        exit 1
    fi
    printf 'production coverage: %s=%s\n' "${directory}" "${coverage}"
}

check "${root}" "" "${temporary}/core.out"
check "${root}/postgres" "integration" "${temporary}/postgres.out"

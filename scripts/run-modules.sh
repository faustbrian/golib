#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
    printf 'usage: %s <gate> <--all|--changed REV|--modules LIST>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
gate="$1"
shift
jobs="${GOLIB_JOBS:-1}"
selection_arguments=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        --jobs)
            [[ $# -ge 2 ]] || {
                printf '%s\n' '--jobs requires a positive integer' >&2
                exit 2
            }
            jobs="$2"
            shift 2
            ;;
        *)
            selection_arguments+=("$1")
            shift
            ;;
    esac
done
case "${gate}" in
    release-dry-run|release-public)
        selection_arguments+=(--dependencies)
        ;;
esac
if [[ ! "${jobs}" =~ ^[1-9][0-9]*$ ]]; then
    printf 'invalid parallel job count: %s\n' "${jobs}" >&2
    exit 2
fi

selection="$(
    go run ./cmd/golib select \
        "${selection_arguments[@]}" --order dependency
)"
case "${gate}" in
    release-dry-run|release-public)
        selection="$(
            printf '%s\n' "${selection}" |
                "${root}/scripts/filter-releasable-modules.sh"
        )"
        ;;
esac
if [[ -z "${selection}" ]]; then
    printf 'module selection is empty\n' >&2
    exit 1
fi

case "${gate}" in
    format|tidy|api-update)
        mutating_gate=1
        ;;
    *)
        mutating_gate=0
        ;;
esac
if [[ "${mutating_gate}" -eq 1 && "${jobs}" -ne 1 ]]; then
    printf 'mutating gate %s does not support parallel jobs\n' "${gate}" >&2
    exit 2
fi

if [[ "${mutating_gate}" -eq 0 &&
    "${GOLIB_VERIFICATION_SNAPSHOT:-0}" != "1" ]]; then
    module_count="$(
        printf '%s\n' "${selection}" | sed '/^$/d' | wc -l | tr -d ' '
    )"
    if [[ "${jobs}" -gt "${module_count}" ]]; then
        jobs="${module_count}"
    fi
    exec "${root}/scripts/internal/run-verification-snapshots.sh" \
        "${root}" "${gate}" "${jobs}" "${selection}"
fi

local_proxy="$(mktemp -d "${TMPDIR:-/tmp}/golib-proxy.XXXXXX")"
local_modcache="$(mktemp -d "${TMPDIR:-/tmp}/golib-modcache.XXXXXX")"
cleanup() {
    rm -rf "${local_proxy}"
    chmod -R u+w "${local_modcache}"
    rm -rf "${local_modcache}"
}
trap cleanup EXIT HUP INT TERM
case "${gate}" in
    format|format-check|workspace-test|safety|benchmark|tidy|release-public)
        needs_proxy=0
        ;;
    *)
        needs_proxy=1
        ;;
esac
if [[ "${needs_proxy}" -eq 1 ]]; then
    selected_modules="$(printf '%s\n' "${selection}" | paste -sd, -)"
    "${root}/scripts/build-local-proxy.sh" \
        "${local_proxy}" v0.0.0 "${selected_modules}"
    export GOLIB_LOCAL_PROXY="${local_proxy}"
    upstream="${GOLIB_UPSTREAM_GOPROXY:-$(go env GOPROXY)}"
    no_sum_db="$(go env GONOSUMDB)"
    export GOPROXY="file://${local_proxy},${upstream}"
    export GONOSUMDB="github.com/faustbrian/golib/*${no_sum_db:+,${no_sum_db}}"
    export GOLIB_LOCAL_MODCACHE="${local_modcache}"
    export GOMODCACHE="${GOLIB_LOCAL_MODCACHE}"
fi

count="$(printf '%s\n' "${selection}" | sed '/^$/d' | wc -l | tr -d ' ')"
printf 'gate=%s modules=%s\n' "${gate}" "${count}"
printf '%s\n' "${selection}" | sed 's/^/  - /'

if [[ "${gate}" == "check" ]]; then
    gates=()
    while IFS= read -r canonical_gate; do
        [[ -n "${canonical_gate}" ]] && gates+=("${canonical_gate}")
    done <"${root}/scripts/check-gates.txt"
else
    gates=("${gate}")
fi

while IFS= read -r module; do
    [[ -n "${module}" ]] || continue
    environment_file="$(mktemp)"
    state_file="$(mktemp)"
    "${root}/scripts/start-services.sh" \
        "${module}" "${environment_file}" "${state_file}"
    set -a
    # shellcheck disable=SC1090 # Generated from pinned local service values.
    source "${environment_file}"
    set +a
    status=0
    for selected_gate in "${gates[@]}"; do
        set +e
        case "${selected_gate}" in
            format|tidy|api-update)
                "${root}/scripts/check-module.sh" \
                    "${module}" "${selected_gate}"
                ;;
            *)
                "${root}/scripts/run-gate-with-evidence.sh" \
                    "${module}" "${selected_gate}"
                ;;
        esac
        status=$?
        set -e
        [[ "${status}" -eq 0 ]] || break
    done
    if [[ "${status}" -eq 0 && "${gate}" == "check" ]]; then
        set +e
        "${root}/scripts/audit-goals.sh" "${module}"
        status=$?
        set -e
    fi
    "${root}/scripts/stop-services.sh" "${state_file}"
    rm -f "${environment_file}" "${state_file}"
    [[ "${status}" -eq 0 ]] || exit "${status}"
done <<<"${selection}"

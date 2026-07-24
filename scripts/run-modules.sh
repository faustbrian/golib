#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
    printf 'usage: %s <gate> <--all|--changed REV|--modules LIST>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
gate="$1"
shift
selection="$(go run ./cmd/golib select "$@" --order dependency)"
if [[ -z "${selection}" ]]; then
    printf 'module selection is empty\n' >&2
    exit 1
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
        "${local_proxy}" v0.1.0 "${selected_modules}"
    export GOLIB_LOCAL_PROXY="${local_proxy}"
    upstream="${GOLIB_UPSTREAM_GOPROXY:-$(go env GOPROXY)}"
    upstream_modcache="${GOLIB_UPSTREAM_GOMODCACHE:-$(go env GOMODCACHE)}"
    no_sum_db="$(go env GONOSUMDB)"
    export GOLIB_UPSTREAM_GOMODCACHE="${upstream_modcache}"
    export GOPROXY="file://${local_proxy},file://${upstream_modcache}/cache/download,${upstream}"
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

#!/usr/bin/env bash
set -euo pipefail

dry_run=0
public=0
if [[ "${1:-}" == "--dry-run" ]]; then
    dry_run=1
    shift
fi
if [[ "${1:-}" == "--public" ]]; then
    public=1
    shift
fi
if [[ $# -ne 1 ]]; then
    printf 'usage: %s [--dry-run] [--public] <module-directory>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
module="$1"
entry="$(jq -c --arg directory "${module}" '.modules[] | select(.directory == $directory)' "${root}/modules.json")"
[[ -n "${entry}" ]] || { printf 'unknown module: %s\n' "${module}" >&2; exit 1; }
[[ "$(jq -r '.releasable' <<<"${entry}")" == true ]] || {
    printf 'module is not releasable: %s\n' "${module}" >&2
    exit 1
}

tag_prefix="$(jq -r '.tag_prefix' <<<"${entry}")"
[[ "${tag_prefix}" == "${module}/v" ]] || {
    printf 'invalid tag prefix %s for %s\n' "${tag_prefix}" "${module}" >&2
    exit 1
}

"${root}/scripts/check-module.sh" "${module}" tidy-check
"${root}/scripts/check-module.sh" "${module}" test
"${root}/scripts/check-module.sh" "${module}" api

consumer="$(mktemp -d)"
trap 'rm -rf "${consumer}"' EXIT
module_path="$(jq -r '.module_path' <<<"${entry}")"
package_path="$(jq -r '.packages[0].import_path // empty' <<<"${entry}")"
[[ -n "${package_path}" ]] || { printf 'module has no consumer package\n' >&2; exit 1; }
(
    cd "${consumer}"
    GOWORK=off go mod init example.com/golib-consumer
    if [[ "${public}" -eq 1 ]]; then
        env -u GOLIB_LOCAL_PROXY \
            GOPROXY="${GOLIB_UPSTREAM_GOPROXY:-https://proxy.golang.org,direct}" \
            GONOSUMDB= \
            GOWORK=off go get "${module_path}@v0.1.0"
        env -u GOLIB_LOCAL_PROXY \
            GOPROXY="${GOLIB_UPSTREAM_GOPROXY:-https://proxy.golang.org,direct}" \
            GONOSUMDB= \
            GOWORK=off go list "${package_path}"
    else
        GOWORK=off go get "${module_path}@v0.1.0"
        GOWORK=off go list "${package_path}"
    fi
)

if [[ "${dry_run}" -eq 1 ]]; then
    if [[ "${public}" -eq 1 ]]; then
        printf 'public release verification passed for %s at %s0.1.0\n' \
            "${module}" "${tag_prefix}"
    else
        printf 'release dry-run passed for %s from the local source proxy; proposed tag %s0.1.0\n' \
            "${module}" "${tag_prefix}"
    fi
    exit 0
fi

printf 'release creation is intentionally delegated to reviewed automation\n' >&2
exit 1

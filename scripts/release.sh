#!/usr/bin/env bash
set -euo pipefail

dry_run=0
plan_only=0
public=0
release_version=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run)
            dry_run=1
            shift
            ;;
        --plan)
            plan_only=1
            shift
            ;;
        --public)
            public=1
            shift
            ;;
        --version)
            [[ $# -ge 2 ]] || {
                printf '%s\n' '--version requires a semantic version' >&2
                exit 2
            }
            release_version="$2"
            shift 2
            ;;
        *)
            break
            ;;
    esac
done
if [[ $# -ne 1 ]]; then
    printf 'usage: %s [--dry-run|--plan] [--public] [--version VERSION] <module-directory>\n' "$0" >&2
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
current_version="$(jq -r '.version' <<<"${entry}")"
initial_version="v1.0.0"
if [[ -z "${release_version}" ]]; then
    if [[ "${current_version}" != "unreleased" ]]; then
        printf 'release version is required after the initial release of %s\n' \
            "${module}" >&2
        exit 1
    fi
    release_version="${initial_version}"
fi
if [[ ! "${release_version}" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    printf 'release version must be canonical semantic version: %s\n' \
        "${release_version}" >&2
    exit 2
fi
if [[ "${current_version}" == "unreleased" && "${release_version}" != "${initial_version}" ]]; then
    printf 'initial release must be %s for %s, got %s\n' \
        "${initial_version}" "${module}" "${release_version}" >&2
    exit 1
fi
tag="${tag_prefix}${release_version#v}"
dependency_release_order="$(
    cd "${root}"
    go run ./cmd/golib select \
        --modules "${module}" --dependencies --order dependency --format json
)"
operational_assurance="$(
    cd "${root}"
    go run ./cmd/golib assurance --format json
)"
plan="$(jq -n \
        --arg module "${module}" \
        --arg module_path "$(jq -r '.module_path' <<<"${entry}")" \
        --arg current_version "${current_version}" \
        --arg proposed_version "${release_version}" \
        --arg tag "${tag}" \
        --argjson dependency_release_order "${dependency_release_order}" \
        --argjson owned_dependencies "$(jq '.owned_dependencies' <<<"${entry}")" \
        --argjson operational_assurance "${operational_assurance}" \
        '{
            module: $module,
            module_path: $module_path,
            current_version: $current_version,
            proposed_version: $proposed_version,
            tag: $tag,
            operational_assurance: $operational_assurance,
            dependency_release_order: $dependency_release_order,
            owned_dependencies: $owned_dependencies,
            commands: [
                "make check MODULES=" + $module,
                "make release-dry-run MODULES=" + $module,
                "make release-public MODULES=" + $module
            ]
        }')"
if [[ "${plan_only}" -eq 1 ]]; then
    printf '%s\n' "${plan}"
    exit 0
fi
printf '%s\n' "${plan}"

if [[ "${dry_run}" -eq 0 ]]; then
    (
        cd "${root}"
        go run ./cmd/golib assurance --require-ready >/dev/null
    )
fi

"${root}/scripts/check-module.sh" "${module}" tidy-check
"${root}/scripts/check-module.sh" "${module}" test
"${root}/scripts/check-module.sh" "${module}" api

consumer="$(mktemp -d)"
release_proxy=""
# shellcheck disable=SC2329 # Invoked by the signal and exit trap.
cleanup() {
    rm -rf "${consumer}"
    if [[ -n "${release_proxy}" ]]; then
        rm -rf "${release_proxy}"
    fi
}
trap cleanup EXIT HUP INT TERM
module_path="$(jq -r '.module_path' <<<"${entry}")"
package_path="$(jq -r '.packages[0].import_path // empty' <<<"${entry}")"
[[ -n "${package_path}" ]] || { printf 'module has no consumer package\n' >&2; exit 1; }
if [[ "${public}" -eq 0 ]]; then
    release_proxy="$(mktemp -d "${TMPDIR:-/tmp}/golib-release-proxy.XXXXXX")"
    "${root}/scripts/build-local-proxy.sh" \
        "${release_proxy}" "${release_version}" "${module}"
fi
(
    cd "${consumer}"
    GOWORK=off go mod init example.com/golib-consumer
    if [[ "${public}" -eq 1 ]]; then
        env -u GOLIB_LOCAL_PROXY \
            GOPROXY="${GOLIB_UPSTREAM_GOPROXY:-https://proxy.golang.org,direct}" \
            GONOSUMDB= \
            GOWORK=off go get "${module_path}@${release_version}"
        env -u GOLIB_LOCAL_PROXY \
            GOPROXY="${GOLIB_UPSTREAM_GOPROXY:-https://proxy.golang.org,direct}" \
            GONOSUMDB= \
            GOWORK=off go list "${package_path}"
    else
        env -u GOLIB_LOCAL_PROXY \
            GOPROXY="file://${release_proxy},${GOLIB_UPSTREAM_GOPROXY:-https://proxy.golang.org,direct}" \
            GONOSUMDB="github.com/faustbrian/golib/*" \
            GOWORK=off go get "${module_path}@${release_version}"
        env -u GOLIB_LOCAL_PROXY \
            GOPROXY="file://${release_proxy},${GOLIB_UPSTREAM_GOPROXY:-https://proxy.golang.org,direct}" \
            GONOSUMDB="github.com/faustbrian/golib/*" \
            GOWORK=off go list "${package_path}"
    fi
)

if [[ "${dry_run}" -eq 1 ]]; then
    if [[ "${public}" -eq 1 ]]; then
        printf 'public release verification passed for %s at %s\n' \
            "${module}" "${tag}"
    else
        printf 'release dry-run passed for %s from the local source proxy; proposed tag %s\n' \
            "${module}" "${tag}"
    fi
    exit 0
fi

printf 'release creation is intentionally delegated to reviewed automation\n' >&2
exit 1

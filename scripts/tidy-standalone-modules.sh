#!/usr/bin/env bash

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
manifest="${GOLIB_STANDALONE_MANIFEST:-${root}/migration/standalone/repositories.json}"
destination_root="${GOLIB_STANDALONE_ROOT:-/Users/brian/Developer/golib}"
task_root="$(mktemp -d /tmp/golib-standalone-tidy.XXXXXX)"

cleanup() {
    chmod -R u+w "${task_root}" 2>/dev/null || true
    find "${task_root}" -depth -delete 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

export GOCACHE="${task_root}/gocache"
export GOMODCACHE="${task_root}/gomodcache"
export GONOSUMDB="github.com/faustbrian/go-*"
mkdir -p "${GOCACHE}" "${GOMODCACHE}"

tool="${task_root}/golib"
proxy="${task_root}/proxy"
go build -o "${tool}" ./cmd/golib

module_root() {
    local repository="$1"
    local directory="$2"
    local result="${destination_root}/${repository}"

    if [[ "${directory}" != "." ]]; then
        result="${result}/${directory}"
    fi
    printf '%s\n' "${result}"
}

rebuild_proxy() {
    local through_wave="$1"

    if [[ -d "${proxy}" ]]; then
        find "${proxy}" -depth -delete
    fi
    mkdir -p "${proxy}"
    "${tool}" standalone-proxy \
        --destination-root "${destination_root}" \
        --output "${proxy}" \
        --through-wave "${through_wave}"
}

assert_owned_dependencies_available() {
    local module_path="$1"

    while IFS=$'\t' read -r dependency version; do
        [[ -z "${dependency}" ]] && continue
        if [[ -z "${version}" || ! -f "${proxy}/${dependency}/@v/${version}.mod" ]]; then
            printf '%s requires unavailable owned dependency %s\n' \
                "${module_path}" "${dependency}" >&2
            exit 1
        fi
    done < <(jq -r --arg module_path "${module_path}" '
        . as $manifest
        | .modules[]
        | select(.module_path == $module_path)
        | .owned_dependencies[] as $dependency
        | [
            $dependency,
            ($manifest.modules[] | select(.module_path == $dependency) | .release_version)
        ]
        | @tsv
    ' "${manifest}")
}

tidy_manifest_selection() {
    local selection="$1"
    local count=0

    while IFS=$'\t' read -r repository directory module_path; do
        [[ -z "${module_path}" ]] && continue
        assert_owned_dependencies_available "${module_path}"
        (
            cd "$(module_root "${repository}" "${directory}")"
            GOWORK=off \
                GOPROXY="file://${proxy},https://proxy.golang.org,direct" \
                go mod tidy
        )
        count=$((count + 1))
    done <<< "${selection}"
    printf 'tidied %d modules\n' "${count}"
}

standalone_manifest_digest() {
    while IFS=$'\t' read -r repository directory module_path; do
        [[ -z "${module_path}" ]] && continue
        module_directory="$(module_root "${repository}" "${directory}")"
        printf 'module %s\n' "${module_path}"
        shasum -a 256 "${module_directory}/go.mod"
        if [[ -f "${module_directory}/go.sum" ]]; then
            shasum -a 256 "${module_directory}/go.sum"
        else
            printf 'go.sum absent\n'
        fi
    done < <(jq -r '
        .modules[]
        | [.repository, .directory, .module_path]
        | @tsv
    ' "${manifest}") | shasum -a 256 | awk '{print $1}'
}

tidy_all_modules() {
    local wave selection

    for ((wave = 1; wave <= wave_count; wave++)); do
        rebuild_proxy "$((wave - 1))"
        selection="$(jq -r --argjson wave "$((wave - 1))" '
            .release_waves[$wave][] as $module_path
            | .modules[]
            | select(.module_path == $module_path)
            | [.repository, .directory, .module_path]
            | @tsv
        ' "${manifest}")"
        printf 'tidying release wave %d/%d\n' "${wave}" "${wave_count}"
        tidy_manifest_selection "${selection}"
    done

    rebuild_proxy "${wave_count}"
    selection="$(jq -r '
        .modules[]
        | select(.releasable == false)
        | [.repository, .directory, .module_path]
        | @tsv
    ' "${manifest}")"
    printf 'tidying non-releasable harness modules\n'
    tidy_manifest_selection "${selection}"
    rebuild_proxy "${wave_count}"
}

wave_count="$(jq '.release_waves | length' "${manifest}")"
maximum_passes="$((wave_count + 2))"
converged=0
for ((pass = 1; pass <= maximum_passes; pass++)); do
    before="$(standalone_manifest_digest)"
    "${tool}" standalone-clean-sums --destination-root "${destination_root}"
    printf 'standalone checksum convergence pass %d/%d\n' \
        "${pass}" "${maximum_passes}"
    tidy_all_modules
    after="$(standalone_manifest_digest)"
    if [[ "${before}" == "${after}" ]]; then
        converged=1
        break
    fi
done
[[ "${converged}" -eq 1 ]] || {
    printf 'standalone module checksums did not converge after %d passes\n' \
        "${maximum_passes}" >&2
    exit 1
}

selection="$(jq -r '
    .modules[]
    | [.repository, .directory, .module_path]
    | @tsv
' "${manifest}")"
verified=0
while IFS=$'\t' read -r repository directory module_path; do
    [[ -z "${module_path}" ]] && continue
    (
        cd "$(module_root "${repository}" "${directory}")"
        GOWORK=off \
            GOPROXY="file://${proxy},https://proxy.golang.org,direct" \
            go mod tidy -diff
    )
    verified=$((verified + 1))
done <<< "${selection}"
printf 'tidy verification passed for %d modules\n' "${verified}"

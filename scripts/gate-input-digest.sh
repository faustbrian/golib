#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 || $# -gt 3 ]]; then
    printf 'usage: %s <gate> <module-directory> [package-directory]\n' "$0" >&2
    exit 2
fi

root="${GOLIB_ROOT:-$(git rev-parse --show-toplevel)}"
gate="$1"
module="$2"
package="${3:-}"
if ! jq -e --arg directory "${module}" \
    '.modules[] | select(.directory == $directory)' \
    "${root}/modules.json" >/dev/null; then
    printf 'module is absent from modules.json: %s\n' "${module}" >&2
    exit 2
fi

manifest="$(mktemp "${TMPDIR:-/tmp}/golib-gate-inputs.XXXXXX")"
directories="$(mktemp "${TMPDIR:-/tmp}/golib-gate-directories.XXXXXX")"
input_files="$(mktemp "${TMPDIR:-/tmp}/golib-gate-files.XXXXXX")"
package_data="${manifest}.packages"
existing_files="${manifest}.existing"
file_hashes="${manifest}.hashes"
cleanup() {
    rm -f \
        "${manifest}" "${directories}" "${input_files}" "${package_data}" \
        "${existing_files}" "${file_hashes}"
}
trap cleanup EXIT HUP INT TERM

append_value() {
    printf 'value  %s=%s\n' "$1" "$2" >>"${manifest}"
}

append_file() {
    local file="$1"
    local relative digest
    [[ -f "${file}" ]] || {
        printf 'gate input is missing: %s\n' "${file}" >&2
        exit 1
    }
    relative="${file#"${root}/"}"
    digest="$(shasum -a 256 "${file}" | awk '{print $1}')"
    printf 'file   %s  %s\n' "${digest}" "${relative}" >>"${manifest}"
}

append_repository_files() {
    local file
    : >"${existing_files}"
    while IFS= read -r file; do
        [[ -n "${file}" ]] || continue
        if [[ -f "${root}/${file}" ]]; then
            printf '%s\n' "${file}" >>"${existing_files}"
        else
            append_value missing-file "${file}"
        fi
    done
    [[ -s "${existing_files}" ]] || return
    git -C "${root}" hash-object --stdin-paths \
        <"${existing_files}" >"${file_hashes}"
    paste "${file_hashes}" "${existing_files}" |
        awk -F '\t' '{ printf "file   %s  %s\n", $1, $2 }' >>"${manifest}"
}

append_tool_inputs() {
    append_file "${root}/.golib/versions.env"
    append_file "${root}/scripts/internal/mutation-command.sh"
    append_file "${root}/scripts/patches/gremlins-run-all-mutants.patch"
    append_file "${root}/scripts/start-services.sh"
}

append_environment() {
    append_value go-version "$(go env GOVERSION)"
    append_value goos "$(go env GOOS)"
    append_value goarch "$(go env GOARCH)"
    append_value cgo-enabled "$(go env CGO_ENABLED)"
}

append_verification_environment() {
    append_environment
    append_value kernel "$(uname -srm)"
    if command -v docker >/dev/null 2>&1; then
        append_value docker "$(
            docker version --format '{{.Server.Version}}' 2>/dev/null ||
                printf unavailable
        )"
    else
        append_value docker missing
    fi
    if command -v node >/dev/null 2>&1; then
        append_value node "$(node --version)"
    else
        append_value node missing
    fi
}

verification_digest() {
    local directory file
    append_value gate "${gate}"
    append_value module "${module}"
    append_verification_environment
    append_value module-policy "$(
        jq -S -c --arg directory "${module}" \
            '.modules[] | select(.directory == $directory)' \
            "${root}/modules.json"
    )"
    append_value package-policy "$(
        jq -S -c --arg directory "${module}" \
            '[.packages[] | select(.module_directory == $directory)]' \
            "${root}/packages.json"
    )"

    printf '%s\n' "${module}" >"${directories}"
    jq -r --arg directory "${module}" '
        . as $catalog
        | def closure($seen):
            ([
                $catalog.modules[]
                | select(.module_path as $path | $seen | index($path))
                | .owned_dependencies[]
            ] | unique) as $dependencies
            | ($seen + $dependencies | unique) as $next
            | if $next == $seen then $next else closure($next) end;
        (.modules[] | select(.directory == $directory).owned_dependencies) as $owned
        | closure($owned) as $paths
        | .modules[]
        | select(.module_path as $path | $paths | index($path))
        | .directory
    ' "${root}/modules.json" >>"${directories}"

    while IFS= read -r directory; do
        [[ -n "${directory}" ]] || continue
        git -C "${root}" ls-files -co --exclude-standard -- "${directory}" \
            >>"${input_files}"
    done < <(LC_ALL=C sort -u "${directories}")
    git -C "${root}" ls-files -co --exclude-standard -- \
        .github/workflows/ci.yml \
        .go-version \
        .golib \
        .gitleaks.toml \
        AGENTS.md \
        Makefile \
        go.mod \
        go.sum \
        go.work \
        modules.json \
        packages.json \
        scripts >>"${input_files}"

    LC_ALL=C sort -u "${input_files}" | append_repository_files
}

mutation_digest() {
    local package_directory package_input_digest
    append_value gate mutation
    append_value module "${module}"
    append_file "${root}/scripts/check-mutation.sh"
    append_file "${root}/scripts/internal/run-mutation.sh"
    while IFS= read -r package_directory; do
        [[ -n "${package_directory}" ]] || continue
        package_input_digest="$(
            "${root}/scripts/gate-input-digest.sh" \
                mutation "${module}" "${package_directory}"
        )"
        append_value "package:${package_directory}" "${package_input_digest}"
    done < <(
        jq -r --arg directory "${module}" '
            .modules[]
            | select(.directory == $directory)
            | .packages[]
            | select(.coverage_required == true)
            | .directory
        ' "${root}/modules.json" | LC_ALL=C sort
    )
}

legacy_digest() {
    local directory
    append_value gate mutation
    append_value module "${module}"
    append_environment
    append_value module-policy "$(
        jq -S -c --arg directory "${module}" \
            '.modules[] | select(.directory == $directory)' \
            "${root}/modules.json"
    )"
    append_value package-policy "$(
        jq -S -c --arg directory "${module}" \
            '[.packages[] | select(.module_directory == $directory)]' \
            "${root}/packages.json"
    )"
    append_value zero-mutant-policy "$(
        jq -S -c --arg directory "${module}" \
            '[.packages[] | select(.module_directory == $directory)]' \
            "${root}/.golib/mutation-zero-inventory.json"
    )"

    append_file "${root}/.golib/versions.env"
    append_file "${root}/scripts/build-golib-gremlins.sh"
    append_file "${root}/scripts/internal/mutation-command.sh"
    append_file "${root}/scripts/patches/gremlins-run-all-mutants.patch"
    append_file "${root}/scripts/start-services.sh"

    printf '%s\n' "${module}" >"${directories}"
    jq -r --arg directory "${module}" '
        . as $catalog
        | def closure($seen):
            ([
                $catalog.modules[]
                | select(.module_path as $path | $seen | index($path))
                | .owned_dependencies[]
            ] | unique) as $dependencies
            | ($seen + $dependencies | unique) as $next
            | if $next == $seen then $next else closure($next) end;
        (.modules[] | select(.directory == $directory).owned_dependencies) as $owned
        | closure($owned) as $paths
        | .modules[]
        | select(.module_path as $path | $paths | index($path))
        | .directory
    ' "${root}/modules.json" >>"${directories}"

    while IFS= read -r directory; do
        [[ -n "${directory}" ]] || continue
        while IFS= read -r -d '' file; do
            append_file "${file}"
        done < <(
            find "${root}/${directory}" -type f \
                ! -path '*/.git/*' \
                ! -path '*/.artifacts/*' \
                ! -path '*/.tools/*' \
                ! -name '*.coverprofile' \
                ! -name 'coverage.out' \
                -print0 | LC_ALL=C sort -z
        )
    done < <(LC_ALL=C sort -u "${directories}")
}

package_digest() {
    local data_name digest_go digest_go_flags digest_workspace flag
    local module_path module_root package_directory resolution tags
    module_root="${root}/${module}"
    if ! jq -e --arg directory "${module}" --arg package "${package}" '
        .modules[]
        | select(.directory == $directory)
        | .packages[]
        | select(.directory == $package and .coverage_required == true)
    ' "${root}/modules.json" >/dev/null; then
        printf 'mutation package is absent from catalog: %s %s\n' \
            "${module}" "${package}" >&2
        exit 2
    fi

    append_value gate mutation
    append_value module "${module}"
    append_value package "${package}"
    append_environment
    append_value module-policy "$(
        jq -S -c --arg directory "${module}" '
            .modules[]
            | select(.directory == $directory)
            | {
                directory,
                module_path,
                go_version,
                owned_dependencies,
                required_services,
                test_tags,
                mutation: .gates.mutation
            }
        ' "${root}/modules.json"
    )"
    append_value package-policy "$(
        jq -S -c --arg directory "${module}" --arg package "${package}" '
            .modules[]
            | select(.directory == $directory)
            | .packages[]
            | select(.directory == $package)
        ' "${root}/modules.json"
    )"
    append_value zero-mutant-policy "$(
        jq -S -c --arg directory "${module}" --arg package "${package}" '
            [.packages[] | select(
                .module_directory == $directory and
                .package_directory == $package
            )]
        ' "${root}/.golib/mutation-zero-inventory.json"
    )"
    # Evidence orchestration does not affect which mutants execute or which
    # tests observe them. Campaign semantics are captured by append_tool_inputs.
    append_tool_inputs

    module_path="$(jq -r --arg directory "${module}" '
        .modules[]
        | select(.directory == $directory)
        | .module_path
    ' "${root}/modules.json")"
    tags="$(jq -r --arg directory "${module}" '
        .modules[]
        | select(.directory == $directory)
        | .test_tags
        | join(",")
    ' "${root}/modules.json")"
    resolution="${GOLIB_MUTATION_DIGEST_RESOLUTION:-stable}"
    if [[ "${resolution}" == "caller" ]]; then
        (
            cd "${module_root}"
            if [[ -n "${tags}" ]]; then
                go list -deps -test -json -tags="${tags}" ./...
            else
                go list -deps -test -json ./...
            fi
        ) >"${package_data}"
    elif [[ "${resolution}" != "stable" ]]; then
        printf 'unknown mutation digest resolution: %s\n' \
            "${resolution}" >&2
        exit 2
    else
    digest_go="${GOLIB_REAL_GO:-$(command -v go)}"
    digest_workspace=off
    if [[ -f "${root}/go.work" ]]; then
        digest_workspace="${root}/go.work"
    fi
    digest_go_flags=""
    for flag in ${GOLIB_UPSTREAM_GOFLAGS:-${GOFLAGS:-}}; do
        case "${flag}" in
            -mod=*|-modfile=*) ;;
            *)
                digest_go_flags="$(
                    printf '%s%s' \
                        "${digest_go_flags:+${digest_go_flags} }" "${flag}"
                )"
                ;;
        esac
    done
    (
        cd "${module_root}"
        if [[ -n "${tags}" ]]; then
            GOWORK="${digest_workspace}" GOFLAGS="${digest_go_flags}" \
                "${digest_go}" list -deps -test -json \
                -tags="${tags}" ./...
        else
            GOWORK="${digest_workspace}" GOFLAGS="${digest_go_flags}" \
                "${digest_go}" list -deps -test -json ./...
        fi
    ) >"${package_data}"
    fi

    jq -r --arg root "${root}/" --arg module_path "${module_path}" '
        select(.Dir | startswith($root))
        | .Dir as $directory
        | (
            [
                .GoFiles[]?,
                .CgoFiles[]?,
                .CFiles[]?,
                .CXXFiles[]?,
                .MFiles[]?,
                .HFiles[]?,
                .FFiles[]?,
                .SFiles[]?,
                .SwigFiles[]?,
                .SwigCXXFiles[]?,
                .SysoFiles[]?,
                .EmbedFiles[]?
            ] +
            (
                if (.Module.Path // "") == $module_path
                then [
                    .TestGoFiles[]?,
                    .XTestGoFiles[]?,
                    .TestEmbedFiles[]?,
                    .XTestEmbedFiles[]?
                ]
                else []
                end
            )
        )[]
        | if startswith("/") then . else "\($directory)/\(.)" end
        | select(startswith($root))
    ' "${package_data}" >>"${input_files}"

    jq -r --arg root "${root}/" '
        select(
            (.Module.GoMod // "") == $root or
            ((.Module.GoMod // "") | startswith($root))
        )
        | .Module.GoMod
    ' "${package_data}" | LC_ALL=C sort -u >>"${input_files}"

    while IFS= read -r package_directory; do
        [[ -n "${package_directory}" ]] || continue
        for data_name in corpus fixtures testdata; do
            if [[ -d "${package_directory}/${data_name}" ]]; then
                find "${package_directory}/${data_name}" -type f \
                    -print >>"${input_files}"
            fi
        done
    done < <(
        jq -r --arg root "${root}/" --arg module_path "${module_path}" '
            select(
                (.Dir | startswith($root)) and
                ((.Module.Path // "") == $module_path)
            )
            | .Dir
        ' "${package_data}" | LC_ALL=C sort -u
    )

    while IFS= read -r file; do
        [[ -n "${file}" ]] || continue
        append_file "${file}"
    done < <(LC_ALL=C sort -u "${input_files}")
}

if [[ "${gate}" == "mutation" && -z "${package}" ]]; then
    mutation_digest
elif [[ "${gate}" == "mutation-legacy" ]]; then
    legacy_digest
elif [[ "${gate}" == "mutation" ]]; then
    package_digest
else
    verification_digest
fi

LC_ALL=C sort "${manifest}" | shasum -a 256 | awk '{print $1}'

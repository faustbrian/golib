#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 3 ]]; then
    printf 'usage: %s <output-directory> [version] [module-directories]\n' \
        "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
output="$1"
version="${2:-v0.1.0}"
selected="${3:-}"

if [[ ! "${version}" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    printf 'local proxy version must be canonical semantic version: %s\n' \
        "${version}" >&2
    exit 2
fi
if [[ -e "${output}" && ! -d "${output}" ]]; then
    printf 'local proxy output is not a directory: %s\n' "${output}" >&2
    exit 2
fi

mkdir -p "${output}"
if find "${output}" -mindepth 1 -print -quit | grep -q .; then
    printf 'local proxy output must be empty: %s\n' "${output}" >&2
    exit 2
fi

temporary="$(mktemp -d "${TMPDIR:-/tmp}/golib-local-proxy.XXXXXX")"
mkdir -p "${temporary}/proxy"
cleanup() {
    rm -rf "${temporary}"
}
trap cleanup EXIT HUP INT TERM

while IFS=$'\t' read -r module_path module_directory; do
    [[ -n "${module_path}" && -n "${module_directory}" ]] || continue
    if [[ "${module_path}" =~ [A-Z] ]]; then
        printf 'local proxy does not support unescaped uppercase module paths: %s\n' \
            "${module_path}" >&2
        exit 1
    fi

    proxy_directory="${temporary}/proxy/${module_path}/@v"
    archive_directory="${temporary}/archive"
    archive_root="${archive_directory}/${module_path}@${version}"
    zip_file="${proxy_directory}/${version}.zip"
    nested_modules="${temporary}/nested-modules"
    mkdir -p "${proxy_directory}" "${archive_root}"
    jq -r --arg current "${module_directory}" '
        .modules[]
        | .directory
        | select(startswith($current + "/"))
    ' "${root}/modules.json" >"${nested_modules}"

    cp "${root}/${module_directory}/go.mod" \
        "${proxy_directory}/${version}.mod"
    printf '{"Version":"%s","Time":"2000-01-01T00:00:00Z"}\n' \
        "${version}" >"${proxy_directory}/${version}.info"
    printf '%s\n' "${version}" >"${proxy_directory}/list"

    while IFS= read -r -d '' source; do
        relative="${source#"${module_directory}/"}"
        nested=0
        while IFS= read -r nested_module; do
            [[ -n "${nested_module}" ]] || continue
            if [[ "${source}" == "${nested_module}" ||
                "${source}" == "${nested_module}/"* ]]; then
                nested=1
                break
            fi
        done <"${nested_modules}"
        [[ "${nested}" -eq 0 ]] || continue
        if [[ -L "${root}/${source}" ]]; then
            continue
        fi

        destination="${archive_root}/${relative}"
        mkdir -p "$(dirname "${destination}")"
        cp -p "${root}/${source}" "${destination}"
    done < <(
        git -C "${root}" ls-files -z --cached --others --exclude-standard \
            -- "${module_directory}"
    )

    find "${archive_directory}" -exec touch -t 200001010000 {} +
    (
        cd "${archive_directory}"
        LC_ALL=C find "${module_path}@${version}" -type f -print |
            LC_ALL=C sort |
            zip -q -X "${zip_file}" -@
    )
    rm -rf "${archive_directory}"
done < <(
    jq -r --arg selected "${selected}" '
        . as $catalog
        | def closure($directories):
            ([
                $catalog.modules[]
                | select(.directory as $directory | $directories | index($directory))
                | .owned_dependencies[]
            ] | unique) as $dependencies
            | ([
                $catalog.modules[]
                | select(.module_path as $path | $dependencies | index($path))
                | .directory
            ] + $directories | unique) as $next
            | if $next == $directories then $next else closure($next) end;
        (
            if $selected == ""
            then [.modules[] | select(.releasable == true) | .directory]
            else closure($selected | split(","))
            end
        ) as $directories
        | .modules[]
        | select(
            .releasable == true
            and (.directory as $directory | $directories | index($directory))
        )
        | [.module_path, .directory]
        | @tsv
    ' "${root}/modules.json"
)

cp -R "${temporary}/proxy/." "${output}/"

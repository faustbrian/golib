#!/usr/bin/env bash

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
manifest="${GOLIB_STANDALONE_MANIFEST:-${root}/migration/standalone/repositories.json}"
destination_root="${GOLIB_STANDALONE_ROOT:-/Users/brian/Developer/golib}"

if [[ "$#" -eq 0 ]]; then
    printf 'usage: %s <family|--all>\n' "$0" >&2
    exit 1
fi

source_commit="$(jq -r '.source.commit' "${manifest}")"
source_ref="refs/remotes/origin/main"
if [[ "$(git rev-parse "${source_ref}")" != "${source_commit}" ]]; then
    printf '%s does not resolve to the verified source commit %s\n' \
        "${source_ref}" "${source_commit}" >&2
    exit 1
fi

if [[ "$1" == "--all" ]]; then
    families=()
    while IFS= read -r family; do
        families+=("${family}")
    done < <(jq -r '.repositories[].family' "${manifest}")
else
    families=("$1")
fi

for family in "${families[@]}"; do
    repository="$(jq -r --arg family "${family}" '
        .repositories[] | select(.family == $family) | .name
    ' "${manifest}")"
    source_directory="$(jq -r --arg family "${family}" '
        .repositories[] | select(.family == $family) | .source_directory
    ' "${manifest}")"
    if [[ -z "${repository}" || "${repository}" == "null" ||
          -z "${source_directory}" || "${source_directory}" == "null" ]]; then
        printf 'family is absent from the standalone manifest: %s\n' "${family}" >&2
        exit 1
    fi

    destination="${destination_root}/${repository}"
    if [[ ! -d "${destination}/.git" ]]; then
        printf 'destination is not an initialized Git repository: %s\n' \
            "${destination}" >&2
        exit 1
    fi
    if [[ -n "$(git -C "${destination}" status --porcelain)" ]]; then
        printf 'destination worktree is not clean: %s\n' "${destination}" >&2
        exit 1
    fi
    if git -C "${destination}" show-ref --quiet; then
        printf 'destination already contains refs: %s\n' "${destination}" >&2
        exit 1
    fi
    expected_origin="git@github.com:faustbrian/${repository}.git"
    if [[ "$(git -C "${destination}" remote get-url origin)" != "${expected_origin}" ]]; then
        printf 'destination origin does not match %s: %s\n' \
            "${expected_origin}" "${destination}" >&2
        exit 1
    fi
    if git ls-tree -r --name-only "${source_commit}" -- "${source_directory}" |
        grep -Eq '[[:space:]]'; then
        printf 'source paths containing whitespace require explicit migration support: %s\n' \
            "${source_directory}" >&2
        exit 1
    fi

    GOLIB_PREFIX="${source_directory}/" \
        git fast-export \
            --signed-tags=strip \
            --tag-of-filtered-object=drop \
            "${source_ref}" -- "${source_directory}" |
        GOLIB_PREFIX="${source_directory}/" perl -pe '
            BEGIN { $prefix = quotemeta($ENV{"GOLIB_PREFIX"}) }
            s/^(M [0-9]+ [^ ]+|D) $prefix/$1 /
        ' |
        git -C "${destination}" fast-import --quiet

    imported_ref="refs/remotes/origin/main"
    if ! git -C "${destination}" show-ref --verify --quiet "${imported_ref}"; then
        printf 'history import did not produce %s for %s\n' \
            "${imported_ref}" "${family}" >&2
        exit 1
    fi
    git -C "${destination}" checkout --quiet -B main "${imported_ref}"

    if git -C "${destination}" ls-tree -r --name-only main |
        grep -Eq "^${source_directory}/"; then
        printf 'source prefix remains after extraction: %s\n' "${family}" >&2
        exit 1
    fi
    if [[ -n "$(git -C "${destination}" status --porcelain)" ]]; then
        printf 'history extraction left a dirty worktree: %s\n' "${destination}" >&2
        exit 1
    fi

    printf '%s\t%s\t%s\n' \
        "${family}" \
        "$(git -C "${destination}" rev-parse main)" \
        "$(git -C "${destination}" rev-list --count main)"
done

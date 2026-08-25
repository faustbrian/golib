#!/usr/bin/env bash

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
manifest="${1:-${root}/migration/standalone/repositories.json}"
destination_root="${2:-/Users/brian/Developer/golib}"
output="${3:-${destination_root}/.migration-audit.json}"

for command in gh git jq curl; do
    command -v "${command}" >/dev/null || {
        printf 'required command is unavailable: %s\n' "${command}" >&2
        exit 1
    }
done

jq -e '
    .counts.repositories == 82 and
    .counts.releasable_modules == 110 and
    (.repositories | length) == 82
' "${manifest}" >/dev/null || {
    printf 'standalone migration manifest has an unexpected scope\n' >&2
    exit 1
}

scratch="$(mktemp -d -t golib-standalone-audit.XXXXXX)"
temporary_output=""
cleanup() {
    if [[ -n "${temporary_output}" && -e "${temporary_output}" ]]; then
        unlink "${temporary_output}"
    fi
    chmod -R u+w "${scratch}" 2>/dev/null || true
    find "${scratch}" -depth -delete
}
trap cleanup EXIT HUP INT TERM

repositories_file="${scratch}/repositories.ndjson"
versions_file="${scratch}/versions.ndjson"
: >"${repositories_file}"
: >"${versions_file}"

while IFS=$'\t' read -r family target legacy; do
    metadata_file="${scratch}/${legacy}.metadata.json"
    refs_file="${scratch}/${legacy}.refs"
    releases_file="${scratch}/${legacy}.releases.json"
    rulesets_file="${scratch}/${legacy}.rulesets.json"
    protection_file="${scratch}/${legacy}.protection.json"

    env -u GH_TOKEN gh api "repos/faustbrian/${legacy}" >"${metadata_file}"
    git ls-remote --refs "git@github.com:faustbrian/${legacy}.git" >"${refs_file}"
    env -u GH_TOKEN gh api --paginate "repos/faustbrian/${legacy}/releases?per_page=100" \
        --slurp >"${releases_file}"
    env -u GH_TOKEN gh api \
        "repos/faustbrian/${legacy}/rulesets?includes_parents=false" \
        >"${rulesets_file}" 2>/dev/null || printf '[]\n' >"${rulesets_file}"
    default_branch="$(jq -r '.default_branch' "${metadata_file}")"
    env -u GH_TOKEN gh api \
        "repos/faustbrian/${legacy}/branches/${default_branch}/protection" \
        >"${protection_file}" 2>/dev/null || printf 'null\n' >"${protection_file}"

    jq -cn \
        --arg family "${family}" \
        --arg target "${target}" \
        --arg legacy "${legacy}" \
        --slurpfile metadata "${metadata_file}" \
        --rawfile refs "${refs_file}" \
        --slurpfile release_pages "${releases_file}" \
        --slurpfile rulesets "${rulesets_file}" \
        --slurpfile protection "${protection_file}" '
        {
            family: $family,
            target_repository: $target,
            legacy_repository: $legacy,
            metadata: {
                id: $metadata[0].id,
                node_id: $metadata[0].node_id,
                default_branch: $metadata[0].default_branch,
                visibility: $metadata[0].visibility,
                archived: $metadata[0].archived,
                description: $metadata[0].description,
                topics: $metadata[0].topics
            },
            refs: (
                $refs
                | split("\n")
                | map(select(length > 0) | split("\t") | {
                    object: .[0],
                    ref: .[1]
                })
            ),
            releases: (
                $release_pages
                | flatten
                | map({
                    id,
                    tag_name,
                    target_commitish,
                    draft,
                    prerelease,
                    created_at,
                    published_at
                })
            ),
            rulesets: $rulesets[0],
            default_branch_protection: $protection[0]
        }
    ' >>"${repositories_file}"
done < <(
    jq -r '.repositories[] | [.family, .name, .legacy_repository] | @tsv' "${manifest}"
)

while IFS=$'\t' read -r module_path legacy_path; do
    versions="$(curl --fail --silent --show-error --max-time 15 \
        "https://proxy.golang.org/${legacy_path}/@v/list" 2>/dev/null || true)"
    jq -cn \
        --arg module_path "${module_path}" \
        --arg legacy_path "${legacy_path}" \
        --arg versions "${versions}" '
        {
            target_module: $module_path,
            legacy_module: $legacy_path,
            public_versions: ($versions | split("\n") | map(select(length > 0)))
        }
    ' >>"${versions_file}"
done < <(
    jq -r '
        . as $manifest
        | .modules[]
        | select(.releasable)
        | . as $module
        | ($manifest.repositories[] | select(.name == $module.repository)) as $repository
        | [
            .module_path,
            (
                "github.com/faustbrian/" + $repository.legacy_repository +
                (if .directory == "." then "" else "/" + .directory end)
            )
        ]
        | @tsv
    ' "${manifest}"
)

temporary_output="${output}.tmp.$$"
jq -n \
    --arg captured_at "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
    --arg destination_root "${destination_root}" \
    --slurpfile manifest "${manifest}" \
    --slurpfile repositories "${repositories_file}" \
    --slurpfile public_versions "${versions_file}" '
    {
        schema_version: 1,
        captured_at: $captured_at,
        source: $manifest[0].source,
        destination_root: $destination_root,
        repositories: $repositories,
        public_versions: $public_versions
    }
' >"${temporary_output}"
mv "${temporary_output}" "${output}"
temporary_output=""

printf 'captured %s repositories and %s module version records in %s\n' \
    "$(jq '.repositories | length' "${output}")" \
    "$(jq '.public_versions | length' "${output}")" \
    "${output}"

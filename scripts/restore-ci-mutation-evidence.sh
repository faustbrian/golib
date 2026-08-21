#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
    printf 'usage: %s <module-directory> [artifact-archive]\n' "$0" >&2
    exit 2
fi

root="${GOLIB_ROOT:-$(git rev-parse --show-toplevel)}"
module="$1"
archive="${2:-}"

if ! jq -e --arg directory "${module}" \
    '.modules[] | select(.directory == $directory)' \
    "${root}/modules.json" >/dev/null; then
    printf 'cannot restore mutation evidence for unknown module: %s\n' \
        "${module}" >&2
    exit 2
fi

task="$(mktemp -d "${TMPDIR:-/tmp}/golib-ci-evidence.XXXXXX")"
cleanup() {
    find "${task}" -depth -delete 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

if [[ -z "${archive}" ]]; then
    repository="${GITHUB_REPOSITORY:-}"
    repository_id="${GITHUB_REPOSITORY_ID:-}"
    run_id="${GITHUB_RUN_ID:-}"
    if [[ -z "${GH_TOKEN:-}" || -z "${repository}" ||
        ! "${repository_id}" =~ ^[0-9]+$ || ! "${run_id}" =~ ^[0-9]+$ ]]; then
        printf '[%s] prior CI mutation evidence is unavailable\n' "${module}"
        exit 0
    fi
    command -v gh >/dev/null || {
        printf 'GitHub CLI is required to restore CI mutation evidence\n' >&2
        exit 1
    }

    artifact_slug="${module//\//-}"
    [[ "${artifact_slug}" != "." ]] || artifact_slug=root
    artifact_name="evidence-${artifact_slug}"
    if ! artifacts="$(
        gh api --method GET \
            "repos/${repository}/actions/artifacts" \
            -f "name=${artifact_name}" \
            -f per_page=100
    )"; then
        printf '[%s] prior CI mutation evidence lookup failed; executing fresh\n' \
            "${module}"
        exit 0
    fi
    artifact_id="$(
        jq -r \
            --argjson repository_id "${repository_id}" \
            --argjson run_id "${run_id}" '
                [
                    .artifacts[]
                    | select(
                        .expired == false and
                        .workflow_run.head_repository_id == $repository_id and
                        .workflow_run.head_branch == "main" and
                        .workflow_run.id != $run_id
                    )
                ]
                | sort_by(.created_at)
                | reverse
                | (.[0].id // "")
            ' <<<"${artifacts}"
    )"
    if [[ ! "${artifact_id}" =~ ^[0-9]+$ ]]; then
        printf '[%s] no trusted prior CI mutation evidence found\n' "${module}"
        exit 0
    fi
    archive="${task}/evidence.zip"
    if ! gh api \
        "repos/${repository}/actions/artifacts/${artifact_id}/zip" \
        >"${archive}"; then
        printf '[%s] prior CI mutation evidence download failed; executing fresh\n' \
            "${module}"
        exit 0
    fi
fi

command -v unzip >/dev/null || {
    printf 'unzip is required to restore CI mutation evidence\n' >&2
    exit 1
}
if [[ ! -s "${archive}" ]] || ! unzip -tqq "${archive}" >/dev/null; then
    printf '[%s] prior CI mutation evidence archive is invalid; executing fresh\n' \
        "${module}"
    exit 0
fi

checkpoint_directory="${root}/.artifacts/${module}/mutation-checkpoints"
mkdir -p "${checkpoint_directory}"
entries="${task}/entries"
unzip -Z1 "${archive}" >"${entries}"
restored=0
while IFS= read -r package_directory; do
    [[ -n "${package_directory}" ]] || continue
    slug=root
    if [[ "${package_directory}" != "." ]]; then
        slug="${package_directory//\//-}"
    fi
    entry="mutation-checkpoints/${slug}.json"
    if [[ "$(grep -Fxc -- "${entry}" "${entries}")" -ne 1 ]]; then
        continue
    fi
    candidate="$(mktemp "${task}/checkpoint.XXXXXX")"
    if ! unzip -p "${archive}" "${entry}" >"${candidate}" ||
        ! jq -e \
            --arg module "${module}" \
            --arg package "${package_directory}" '
                .schema_version == 3 and
                .module == $module and
                .package == $package and
                (.execution_revision | type == "string") and
                (.execution_revision | test("^[0-9a-f]{40}$")) and
                (.gate_input_digest | type == "string") and
                (.gate_input_digest | test("^[0-9a-f]{64}$")) and
                (.gremlins_version | type == "string") and
                (.gremlins_version | length > 0) and
                (
                    (.gremlins_verifier_sha256 // null) == null or
                    (
                        (.gremlins_verifier_sha256 | type == "string") and
                        (.gremlins_verifier_sha256 | test("^[0-9a-f]{64}$"))
                    )
                ) and
                (.report.files | type == "array") and
                ([.report.files[].mutations[]? | select(.status != "KILLED")] | length == 0)
            ' "${candidate}" >/dev/null; then
        find "${candidate}" -delete 2>/dev/null || true
        continue
    fi
    mv "${candidate}" "${checkpoint_directory}/${slug}.json"
    restored=$((restored + 1))
done < <(
    jq -r --arg directory "${module}" '
        .modules[]
        | select(.directory == $directory)
        | .packages[]
        | select(.coverage_required == true)
        | .directory
    ' "${root}/modules.json" | LC_ALL=C sort
)

printf '[%s] restored %d prior content-addressed mutation checkpoints\n' \
    "${module}" "${restored}"

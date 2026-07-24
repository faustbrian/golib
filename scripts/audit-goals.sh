#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
    printf 'usage: %s <module-directory>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
module="$1"
artifact="${root}/.artifacts/${module}"
output="${artifact}/goal-traceability.json"
temporary="${output}.tmp.$$"
gate_records="$(mktemp "${TMPDIR:-/tmp}/golib-goal-gates.XXXXXX")"
cleanup() {
    rm -f "${temporary}" "${gate_records}"
}
trap cleanup EXIT HUP INT TERM

module_record="$(
    jq -e -c --arg directory "${module}" \
        '.modules[] | select(.directory == $directory)' \
        "${root}/modules.json"
)" || {
    printf 'module is absent from modules.json: %s\n' "${module}" >&2
    exit 2
}
goal_count="$(jq '.goal_evidence | length' <<<"${module_record}")"
mkdir -p "${artifact}"

if [[ "${goal_count}" -gt 0 ]]; then
    while IFS= read -r gate; do
        [[ -n "${gate}" ]] || continue
        "${root}/scripts/verify-gate-evidence.sh" "${module}" "${gate}"
        evidence="${artifact}/evidence/${gate}.json"
        jq -c '{
            gate,
            result,
            input_digest,
            execution_revision,
            completed_at,
            revalidated_revision,
            revalidated_at
        }' "${evidence}" >>"${gate_records}"
    done <"${root}/scripts/check-gates.txt"
fi

verified_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
revision="$(git rev-parse HEAD)"
manifest_sha256="$(shasum -a 256 "${root}/modules.json" | awk '{print $1}')"
jq -n \
    --argjson module "${module_record}" \
    --slurpfile gates "${gate_records}" \
    --arg verified_at "${verified_at}" \
    --arg revision "${revision}" \
    --arg manifest_sha256 "${manifest_sha256}" \
    '{
        schema_version: 1,
        module: $module.directory,
        module_path: $module.module_path,
        requirements_manifest_sha256: $manifest_sha256,
        implementation_status: $module.goal_status,
        verification_status: (
            if ($module.goal_evidence | length) == 0
            then "not-applicable"
            else "verified"
            end
        ),
        verified_revision: $revision,
        verified_at: $verified_at,
        goals: [
            $module.goal_evidence[] |
            . + {verification_status: "verified"}
        ],
        gate_evidence: $gates
    }' >"${temporary}"
mv "${temporary}" "${output}"
printf '[%s] goal traceability: %s (%s goals)\n' \
    "${module}" \
    "$(jq -r '.verification_status' "${output}")" \
    "${goal_count}"

#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
    printf 'usage: %s <module-directory> <gate>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
module="$1"
gate="$2"
artifact="${root}/.artifacts/${module}/evidence"
evidence="${artifact}/${gate}.json"
log="${artifact}/${gate}.log"
temporary_evidence="${evidence}.tmp.$$"
temporary_log="${log}.tmp.$$"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
execution_revision="$(git rev-parse HEAD)"
input_digest="$("${root}/scripts/gate-input-digest.sh" "${gate}" "${module}")"

# shellcheck disable=SC2329 # Invoked by the signal and exit trap.
cleanup() {
    rm -f "${temporary_evidence}" "${temporary_log}"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM
mkdir -p "${artifact}"
rm -f "${temporary_evidence}" "${temporary_log}"

if [[ -f "${evidence}" && -f "${log}" ]]; then
    recorded_log_sha256="$(jq -er '.log_sha256' "${evidence}" 2>/dev/null || true)"
    current_log_sha256="$(shasum -a 256 "${log}" | awk '{print $1}')"
    if jq -e \
        --arg module "${module}" \
        --arg gate "${gate}" \
        --arg input_digest "${input_digest}" \
        --arg log_sha256 "${current_log_sha256}" \
        '
            .schema_version == 1 and
            .module == $module and
            .gate == $gate and
            .result == "passed" and
            .exit_code == 0 and
            .input_digest == $input_digest and
            .completed_input_digest == $input_digest and
            .log_sha256 == $log_sha256
        ' "${evidence}" >/dev/null 2>&1 &&
        [[ "${recorded_log_sha256}" == "${current_log_sha256}" ]]; then
        revalidated_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        jq \
            --arg revalidated_revision "${execution_revision}" \
            --arg revalidated_at "${revalidated_at}" \
            '
                .revalidated_revision = $revalidated_revision |
                .revalidated_at = $revalidated_at |
                .reuse_count = ((.reuse_count // 0) + 1)
            ' "${evidence}" >"${temporary_evidence}"
        mv "${temporary_evidence}" "${evidence}"
        printf '[%s] %s evidence: reused\n' "${module}" "${gate}"
        exit 0
    fi
fi

set +e
"${root}/scripts/check-module.sh" "${module}" "${gate}" 2>&1 |
    tee "${temporary_log}"
command_status=${PIPESTATUS[0]}
set -e

completed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
completed_revision="$(git rev-parse HEAD)"
completed_digest="$("${root}/scripts/gate-input-digest.sh" "${gate}" "${module}")"
log_sha256="$(shasum -a 256 "${temporary_log}" | awk '{print $1}')"
result=passed
if [[ "${command_status}" -ne 0 ]]; then
    result=failed
elif [[ "${completed_digest}" != "${input_digest}" ]]; then
    result=invalidated
    command_status=1
fi

jq -n \
    --arg module "${module}" \
    --arg gate "${gate}" \
    --arg result "${result}" \
    --argjson exit_code "${command_status}" \
    --arg execution_revision "${execution_revision}" \
    --arg completed_revision "${completed_revision}" \
    --arg input_digest "${input_digest}" \
    --arg completed_digest "${completed_digest}" \
    --arg log_sha256 "${log_sha256}" \
    --arg started_at "${started_at}" \
    --arg completed_at "${completed_at}" \
    --arg go_version "$(go env GOVERSION)" \
    --arg goos "$(go env GOOS)" \
    --arg goarch "$(go env GOARCH)" \
    '{
        schema_version: 1,
        module: $module,
        gate: $gate,
        result: $result,
        exit_code: $exit_code,
        execution_revision: $execution_revision,
        completed_revision: $completed_revision,
        input_digest: $input_digest,
        completed_input_digest: $completed_digest,
        log_sha256: $log_sha256,
        started_at: $started_at,
        completed_at: $completed_at,
        environment: {
            go_version: $go_version,
            goos: $goos,
            goarch: $goarch
        }
    }' >"${temporary_evidence}"

mv "${temporary_log}" "${log}"
mv "${temporary_evidence}" "${evidence}"
printf '[%s] %s evidence: %s\n' "${module}" "${gate}" "${result}"
exit "${command_status}"

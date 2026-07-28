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
input_digest="$("${root}/scripts/gate-input-digest.sh" "${gate}" "${module}")"
digest_artifact="${artifact}/by-input/${gate}"
evidence="${digest_artifact}/${input_digest}.json"
log="${digest_artifact}/${input_digest}.log"
legacy_evidence="${artifact}/${gate}.json"
legacy_log="${artifact}/${gate}.log"

if [[ ! -f "${evidence}" || ! -f "${log}" ]]; then
    evidence="${legacy_evidence}"
    log="${legacy_log}"
fi

[[ -f "${evidence}" ]] || {
    printf '[%s] missing %s evidence\n' "${module}" "${gate}" >&2
    exit 1
}
[[ -f "${log}" ]] || {
    printf '[%s] missing %s evidence log\n' "${module}" "${gate}" >&2
    exit 1
}

log_sha256="$(shasum -a 256 "${log}" | awk '{print $1}')"
jq -e \
    --arg module "${module}" \
    --arg gate "${gate}" \
    --arg input_digest "${input_digest}" \
    --arg log_sha256 "${log_sha256}" \
    '
        .schema_version == 1 and
        .module == $module and
        .gate == $gate and
        .result == "passed" and
        .exit_code == 0 and
        .input_digest == $input_digest and
        .completed_input_digest == $input_digest and
        .log_sha256 == $log_sha256
    ' "${evidence}" >/dev/null || {
    printf '[%s] stale or invalid %s evidence\n' "${module}" "${gate}" >&2
    exit 1
}

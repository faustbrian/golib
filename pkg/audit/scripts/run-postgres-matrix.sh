#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
module="pkg/audit/postgres"
matrix="${root}/${module}/testdata/postgres-images.tsv"
artifact="${root}/.artifacts/${module}/postgres-matrix"
execution_revision="$(git -C "${root}" rev-parse HEAD)"
go_version="$("${root}/pkg/audit/scripts/with-gocache.sh" go env GOVERSION)"
environment_os="$(uname -s)"
environment_arch="$(uname -m)"
temporary_evidence=""
temporary_log=""
revalidated=""
temporary_aggregate=""
expected_version=14
cleanup() {
    [[ -z "${temporary_evidence}" ]] || rm -f "${temporary_evidence}"
    [[ -z "${temporary_log}" ]] || rm -f "${temporary_log}"
    [[ -z "${revalidated}" ]] || rm -f "${revalidated}"
    [[ -z "${temporary_aggregate}" ]] || rm -f "${temporary_aggregate}"
    rm -f "${artifact}"/postgres-*.json.tmp.$$ "${artifact}"/postgres-*.log.tmp.$$
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM
mkdir -p "${artifact}/by-input"

while read -r version image extra; do
    [[ -n "${version}" && -n "${image}" && -z "${extra:-}" ]] || {
        printf 'invalid PostgreSQL matrix row for version %s\n' "${version:-unknown}" >&2
        exit 1
    }
    [[ "${image}" == postgres:"${version}".*-alpine@sha256:* ]] || {
        printf 'PostgreSQL %s image is not digest-pinned\n' "${version}" >&2
        exit 1
    }
    [[ "${version}" == "${expected_version}" ]] || {
        printf 'expected PostgreSQL matrix version %s, found %s\n' \
            "${expected_version}" "${version}" >&2
        exit 1
    }
    expected_version=$((expected_version + 1))
    base_digest="$("${root}/scripts/gate-input-digest.sh" test "${module}")"
    input_digest="$(printf '%s\n%s\n%s\n' "${base_digest}" "${version}" "${image}" | shasum -a 256 | awk '{print $1}')"
    directory="${artifact}/by-input/${input_digest}"
    evidence="${directory}/postgres-${version}.json"
    log="${directory}/postgres-${version}.log"
    temporary_evidence="${evidence}.tmp.$$"
    temporary_log="${log}.tmp.$$"
    mkdir -p "${directory}"
    rm -f "${temporary_evidence}" "${temporary_log}"

    current_log_sha256=""
    if [[ -s "${log}" ]]; then
        current_log_sha256="$(shasum -a 256 "${log}" | awk '{print $1}')"
    fi
    if [[ -s "${evidence}" && -s "${log}" ]] && jq -e \
        --arg version "${version}" --arg image "${image}" --arg input_digest "${input_digest}" \
        --arg log_sha256 "${current_log_sha256}" \
        '.schema_version == 1 and .result == "passed" and .exit_code == 0 and
         .postgres_version == $version and .image == $image and
         .input_digest == $input_digest and .completed_input_digest == $input_digest and
         .log_sha256 == $log_sha256' \
        "${evidence}" >/dev/null; then
        revalidated="${evidence}.revalidated.$$"
        jq --arg revision "${execution_revision}" --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
            '.revalidated_revision=$revision | .revalidated_at=$at |
             .reuse_count=((.reuse_count // 0) + 1)' "${evidence}" >"${revalidated}"
        mv "${revalidated}" "${evidence}"
        revalidated=""
        cp "${log}" "${artifact}/postgres-${version}.log.tmp.$$"
        cp "${evidence}" "${artifact}/postgres-${version}.json.tmp.$$"
        mv "${artifact}/postgres-${version}.log.tmp.$$" "${artifact}/postgres-${version}.log"
        mv "${artifact}/postgres-${version}.json.tmp.$$" "${artifact}/postgres-${version}.json"
        printf '[%s] PostgreSQL %s matrix evidence: reused\n' "${module}" "${version}"
        continue
    fi

    started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    set +e
    POSTGRES_VERSION="${version}" "${root}/pkg/audit/scripts/with-gocache.sh" \
        "${root}/scripts/check-module.sh" "${module}" test 2>&1 | tee "${temporary_log}"
    status=${PIPESTATUS[0]}
    set -e
    completed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    completed_revision="$(git -C "${root}" rev-parse HEAD)"
    completed_base_digest="$("${root}/scripts/gate-input-digest.sh" test "${module}")"
    completed_digest="$(printf '%s\n%s\n%s\n' "${completed_base_digest}" "${version}" "${image}" | shasum -a 256 | awk '{print $1}')"
    result=passed
    if [[ "${status}" -ne 0 ]]; then
        result=failed
    elif [[ "${completed_digest}" != "${input_digest}" ]]; then
        result=invalidated
        status=1
    fi
    log_sha256="$(shasum -a 256 "${temporary_log}" | awk '{print $1}')"
    jq -n \
        --arg module "${module}" --arg result "${result}" --argjson exit_code "${status}" \
        --arg postgres_version "${version}" --arg image "${image}" \
        --arg execution_revision "${execution_revision}" --arg completed_revision "${completed_revision}" \
        --arg input_digest "${input_digest}" --arg completed_input_digest "${completed_digest}" \
        --arg log_sha256 "${log_sha256}" --arg started_at "${started_at}" --arg completed_at "${completed_at}" \
        --arg go_version "${go_version}" --arg os "${environment_os}" --arg arch "${environment_arch}" \
        '{schema_version:1,module:$module,gate:"postgres-matrix",result:$result,exit_code:$exit_code,
          postgres_version:$postgres_version,image:$image,execution_revision:$execution_revision,
          completed_revision:$completed_revision,input_digest:$input_digest,
          completed_input_digest:$completed_input_digest,log_sha256:$log_sha256,
          started_at:$started_at,completed_at:$completed_at,
          environment:{go_version:$go_version,os:$os,arch:$arch}}' \
        >"${temporary_evidence}"
    mv "${temporary_log}" "${log}"
    temporary_log=""
    mv "${temporary_evidence}" "${evidence}"
    temporary_evidence=""
    cp "${log}" "${artifact}/postgres-${version}.log.tmp.$$"
    cp "${evidence}" "${artifact}/postgres-${version}.json.tmp.$$"
    mv "${artifact}/postgres-${version}.log.tmp.$$" "${artifact}/postgres-${version}.log"
    mv "${artifact}/postgres-${version}.json.tmp.$$" "${artifact}/postgres-${version}.json"
    printf '[%s] PostgreSQL %s matrix evidence: %s\n' "${module}" "${version}" "${result}"
    [[ "${status}" -eq 0 ]] || exit "${status}"
done <"${matrix}"

[[ "${expected_version}" == 19 ]] || {
    printf 'PostgreSQL matrix must contain versions 14 through 18 exactly once\n' >&2
    exit 1
}

temporary_aggregate="${artifact}/matrix.json.tmp.$$"
jq -s '{schema_version:1,gate:"postgres-matrix",result:(if all(.result == "passed") then "passed" else "failed" end),versions:.}' \
    "${artifact}"/postgres-{14,15,16,17,18}.json >"${temporary_aggregate}"
mv "${temporary_aggregate}" "${artifact}/matrix.json"
temporary_aggregate=""

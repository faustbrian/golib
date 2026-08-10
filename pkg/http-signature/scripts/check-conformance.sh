#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
inventory="${module_root}/spec/normative-requirements.json"
sources="${module_root}/spec/sources.lock.json"
work="$(mktemp -d "${TMPDIR:-/tmp}/http-signature-conformance.XXXXXX")"
cleanup() {
    find "${work}" -type f -delete
    rmdir "${work}"
}
trap cleanup EXIT HUP INT TERM

jq -e '
    .schema_version == 1 and
    (.rfcs | length == 2) and
    (.mappings | type == "object") and
    all(.mappings[];
        (.implementation | type == "array" and length > 0) and
        (.tests | type == "array" and length > 0) and
        (.documentation | type == "array" and length > 0)
    ) and
    all(.rfcs[];
        (.groups | type == "object") and
        all(.groups | keys[]; . as $group | $group != "")
    )
' "${inventory}" >/dev/null

while IFS= read -r referenced_path; do
    [[ -e "${module_root}/${referenced_path}" ]] || {
        printf 'missing conformance mapping target: %s\n' "${referenced_path}" >&2
        exit 1
    }
done < <(jq -r '.mappings[] | .implementation[], .tests[], .documentation[]' "${inventory}" | sort -u)

for rfc in 9421 9530; do
    source_name="RFC ${rfc} text"
    url="$(jq -er --arg name "${source_name}" '.sources[] | select(.name == $name) | .url' "${sources}")"
    locked_sha256="$(jq -er --arg name "${source_name}" '.sources[] | select(.name == $name) | .sha256' "${sources}")"
    inventory_sha256="$(jq -er --argjson rfc "${rfc}" '.rfcs[] | select(.rfc == $rfc) | .source_sha256' "${inventory}")"
    [[ "${locked_sha256}" == "${inventory_sha256}" ]] || {
        printf 'RFC %s conformance source digest does not match source lock\n' "${rfc}" >&2
        exit 1
    }

    source_file="${work}/rfc${rfc}.txt"
    actual_file="${work}/rfc${rfc}.actual"
    expected_file="${work}/rfc${rfc}.expected"
    curl --fail --silent --show-error --location "${url}" --output "${source_file}"
    actual_sha256="$(shasum -a 256 "${source_file}" | awk '{print $1}')"
    [[ "${actual_sha256}" == "${locked_sha256}" ]] || {
        printf 'RFC %s source checksum mismatch\n' "${rfc}" >&2
        exit 1
    }

    if [[ "${rfc}" == "9421" ]]; then
        perl -ne '
            next if $. >= 264 && $. <= 268;
            while (/\b(MUST NOT|MUST|SHALL NOT|SHALL|REQUIRED|SHOULD NOT|SHOULD|NOT RECOMMENDED|RECOMMENDED|MAY|OPTIONAL)\b/g) {
                print "$.:$1\n";
            }
        ' "${source_file}" | sort -V >"${actual_file}"
    else
        perl -ne '
            next if $. >= 241 && $. <= 245;
            while (/\b(MUST NOT|MUST|SHALL NOT|SHALL|REQUIRED|SHOULD NOT|SHOULD|NOT RECOMMENDED|RECOMMENDED|MAY|OPTIONAL)\b/g) {
                print "$.:$1\n";
            }
        ' "${source_file}" | sort -V >"${actual_file}"
    fi
    jq -r --argjson rfc "${rfc}" '.rfcs[] | select(.rfc == $rfc) | .groups[][]' "${inventory}" | sort -V >"${expected_file}"
    diff -u "${expected_file}" "${actual_file}"

    while IFS= read -r group; do
        jq -e --arg group "${group}" '.mappings[$group] != null' "${inventory}" >/dev/null || {
            printf 'RFC %s references an unmapped conformance group: %s\n' "${rfc}" "${group}" >&2
            exit 1
        }
    done < <(jq -r --argjson rfc "${rfc}" '.rfcs[] | select(.rfc == $rfc) | .groups | keys[]' "${inventory}")

    printf 'RFC %s: %s normative keyword occurrences mapped\n' "${rfc}" "$(wc -l <"${actual_file}" | tr -d ' ')"
done

#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
inventory="${module_root}/spec/normative-requirements.json"
sources="${module_root}/spec/sources.lock.json"
work="$(mktemp -d "${TMPDIR:-/tmp}/http-signature-conformance.XXXXXX")"
cleanup() {
    find "${work}" -type f -delete
    find "${work}" -depth -mindepth 1 -type d -exec rmdir {} +
    rmdir "${work}"
}
trap cleanup EXIT HUP INT TERM

jq -e '
    .schema_version == 2 and
    (.rfcs | length == 3) and
    ([.rfcs[].rfc] | sort == [8941, 9421, 9530]) and
    (.mappings | type == "object") and
    all(.mappings[];
        (.implementation | type == "array" and length > 0) and
        (.tests | type == "array" and length > 0) and
        (.documentation | type == "array" and length > 0)
    ) and
    all(.rfcs[];
        (.groups | type == "object" and length > 0) and
        (.abnf_rules | type == "object") and
        (.prose_sections | type == "object" and length > 0) and
        ([.abnf_rules[][]] | length == (unique | length)) and
        ([.prose_sections[][]] | length == (unique | length)) and
        all((.groups, .abnf_rules, .prose_sections) | keys[]; . != "")
    ) and
    ([.rfcs[] | (.groups, .abnf_rules, .prose_sections) | keys[]] | unique) == (.mappings | keys)
' "${inventory}" >/dev/null

while IFS= read -r referenced_path; do
    [[ -e "${module_root}/${referenced_path}" ]] || {
        printf 'missing conformance mapping target: %s\n' "${referenced_path}" >&2
        exit 1
    }
done < <(jq -r '.mappings[] | .implementation[], .tests[], .documentation[]' "${inventory}" | sort -u)

for rfc in 8941 9421 9530; do
    case "${rfc}" in
        8941)
            conventions_start=162
            conventions_end=167
            prose_start=100
            prose_top_level_max=6
            ;;
        9421)
            conventions_start=264
            conventions_end=268
            prose_start=180
            prose_top_level_max=8
            ;;
        9530)
            conventions_start=241
            conventions_end=245
            prose_start=108
            prose_top_level_max=7
            ;;
    esac

    source_name="RFC ${rfc} text"
    url="$(jq -er --arg name "${source_name}" '.sources[] | select(.name == $name) | .url' "${sources}")"
    locked_sha256="$(jq -er --arg name "${source_name}" '.sources[] | select(.name == $name) | .sha256' "${sources}")"
    inventory_sha256="$(jq -er --argjson rfc "${rfc}" '.rfcs[] | select(.rfc == $rfc) | .source_sha256' "${inventory}")"
    [[ "${locked_sha256}" == "${inventory_sha256}" ]] || {
        printf 'RFC %s conformance source digest does not match source lock\n' "${rfc}" >&2
        exit 1
    }

    source_file="${work}/rfc${rfc}.txt"
    curl --fail --silent --show-error --location "${url}" --output "${source_file}"
    actual_sha256="$(shasum -a 256 "${source_file}" | awk '{print $1}')"
    [[ "${actual_sha256}" == "${locked_sha256}" ]] || {
        printf 'RFC %s source checksum mismatch\n' "${rfc}" >&2
        exit 1
    }

    actual_keywords="${work}/rfc${rfc}.keywords.actual"
    expected_keywords="${work}/rfc${rfc}.keywords.expected"
    CONVENTIONS_START="${conventions_start}" CONVENTIONS_END="${conventions_end}" perl -ne '
        next if $. >= $ENV{CONVENTIONS_START} && $. <= $ENV{CONVENTIONS_END};
        while (/\b(MUST NOT|MUST|SHALL NOT|SHALL|REQUIRED|SHOULD NOT|SHOULD|NOT RECOMMENDED|RECOMMENDED|MAY|OPTIONAL)\b/g) {
            print "$.:$1\n";
        }
    ' "${source_file}" | sort -V >"${actual_keywords}"
    jq -r --argjson rfc "${rfc}" '.rfcs[] | select(.rfc == $rfc) | .groups[][]' "${inventory}" | sort -V >"${expected_keywords}"
    diff -u "${expected_keywords}" "${actual_keywords}"

    actual_abnf="${work}/rfc${rfc}.abnf.actual"
    expected_abnf="${work}/rfc${rfc}.abnf.expected"
    perl -ne '
        if (/^   ([a-z][a-z0-9-]*)\s+=/) {
            print "$.:$1\n";
        }
    ' "${source_file}" | sort -V >"${actual_abnf}"
    jq -r --argjson rfc "${rfc}" '.rfcs[] | select(.rfc == $rfc) | .abnf_rules[][]' "${inventory}" | sort -V >"${expected_abnf}"
    diff -u "${expected_abnf}" "${actual_abnf}"

    actual_prose="${work}/rfc${rfc}.prose.actual"
    expected_prose="${work}/rfc${rfc}.prose.expected"
    awk -v start="${prose_start}" -v top_max="${prose_top_level_max}" '
        NR > start && /^([0-9]+\.)+  / {
            section = $1
            sub(/\.$/, "", section)
            top = section
            sub(/\..*/, "", top)
            if (top <= top_max) {
                print section
            }
        }
    ' "${source_file}" | sort -V >"${actual_prose}"
    jq -r --argjson rfc "${rfc}" '.rfcs[] | select(.rfc == $rfc) | .prose_sections[][]' "${inventory}" | sort -V >"${expected_prose}"
    diff -u "${expected_prose}" "${actual_prose}"

    while IFS= read -r group; do
        jq -e --arg group "${group}" '.mappings[$group] != null' "${inventory}" >/dev/null || {
            printf 'RFC %s references an unmapped conformance group: %s\n' "${rfc}" "${group}" >&2
            exit 1
        }
    done < <(jq -r --argjson rfc "${rfc}" '.rfcs[] | select(.rfc == $rfc) | (.groups, .abnf_rules, .prose_sections) | keys[]' "${inventory}" | sort -u)

    printf 'RFC %s: %s BCP 14 occurrences, %s ABNF rules, and %s prose sections mapped\n' \
        "${rfc}" \
        "$(wc -l <"${actual_keywords}" | tr -d ' ')" \
        "$(wc -l <"${actual_abnf}" | tr -d ' ')" \
        "$(wc -l <"${actual_prose}" | tr -d ' ')"
done

corpus_name="HTTPWG RFC 8941 Structured Fields corpus"
corpus_url="$(jq -er --arg name "${corpus_name}" '.sources[] | select(.name == $name) | .url' "${sources}")"
corpus_revision="$(jq -er --arg name "${corpus_name}" '.sources[] | select(.name == $name) | .revision' "${sources}")"
corpus_sha256="$(jq -er --arg name "${corpus_name}" '.sources[] | select(.name == $name) | .sha256' "${sources}")"
corpus_archive="${work}/rfc8941-corpus.tar.gz"
corpus_entries="${work}/rfc8941-corpus.entries"
corpus_extract="${work}/rfc8941-corpus"
corpus_directory="structured-field-tests-${corpus_revision}"

curl --fail --silent --show-error --location "${corpus_url}" --output "${corpus_archive}"
actual_corpus_sha256="$(shasum -a 256 "${corpus_archive}" | awk '{print $1}')"
if [[ "${actual_corpus_sha256}" != "${corpus_sha256}" ]]; then
    printf 'HTTPWG RFC 8941 corpus checksum mismatch\n' >&2
    exit 1
fi

tar -tzf "${corpus_archive}" >"${corpus_entries}"
if [[ ! -s "${corpus_entries}" ]]; then
    printf 'HTTPWG RFC 8941 corpus archive is empty\n' >&2
    exit 1
fi
while IFS= read -r entry; do
    case "${entry}" in
        "${corpus_directory}" | "${corpus_directory}/" | "${corpus_directory}/"*) ;;
        *)
            printf 'HTTPWG RFC 8941 corpus archive has an unexpected path: %s\n' "${entry}" >&2
            exit 1
            ;;
    esac
    case "/${entry}/" in
        *"/../"*)
            printf 'HTTPWG RFC 8941 corpus archive has an unsafe path: %s\n' "${entry}" >&2
            exit 1
            ;;
    esac
done <"${corpus_entries}"
if ! tar -tvzf "${corpus_archive}" | awk '
    substr($1, 1, 1) != "-" && substr($1, 1, 1) != "d" { exit 1 }
'; then
    printf 'HTTPWG RFC 8941 corpus archive contains a non-file entry\n' >&2
    exit 1
fi

mkdir "${corpus_extract}"
tar -xzf "${corpus_archive}" -C "${corpus_extract}"
corpus_root="${corpus_extract}/${corpus_directory}"
if [[ ! -d "${corpus_root}" ]]; then
    printf 'HTTPWG RFC 8941 corpus root is absent after extraction\n' >&2
    exit 1
fi

(
    cd "${module_root}"
    HTTPWG_RFC8941_CORPUS="${corpus_root}" ./scripts/with-go-cache.sh \
        env GOWORK=off go test -mod=readonly . -run '^TestHTTPWGRFC8941Corpus$' -count=1
)
printf 'HTTPWG RFC 8941 corpus: 1526 parsing and 544 serialization cases passed\n'

#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${module_root}/specification/manifest.json"
requirements="${module_root}/specification/normative-requirements.json"
work="$(mktemp -d "${TMPDIR:-/tmp}/cloudevents-specification.XXXXXX")"
cleanup() {
    find "${work}" -type f -delete
    rmdir "${work}"
}
trap cleanup EXIT HUP INT TERM

command -v curl >/dev/null
command -v jq >/dev/null
command -v shasum >/dev/null

jq -e '
    . as $root |
    ($root.sources[] | select(.name == "CloudEvents specification")) as $spec |
    ($root.sources[] | select(.name == "CloudEvents conformance")) as $conformance |
    ($root.sources[] | select(.name == "CloudEvents JavaScript SDK")) as $javascript |
    ($root.sources[] | select(.name == "Node.js npm runtime")) as $node |
    .schema == 2 and
    (.reviewed_at | type == "string" and test("^[0-9]{4}-[0-9]{2}-[0-9]{2}$")) and
    (.sources | type == "array" and length == 7) and
    ([.sources[].name] | length == (unique | length)) and
    ([.sources[] | select(.name == "CloudEvents Go SDK core" and .version == "v2.16.2" and .revision == "af3e8599b3316ab6b4b73ff69aa8ec0efddbb5bb")] | length == 1) and
    ([.sources[] | select(.name == "CloudEvents Go SDK Kafka Sarama" and .version == "protocol/kafka_sarama/v2.16.2" and .revision == "bee0ebe38fde4cecb92dc51aab7acddc951cbd70")] | length == 1) and
    ($spec.files | length == 19) and
    ([$spec.files[].path] | length == (unique | length)) and
    ([$spec.files[].sha256] | all(test("^[0-9a-f]{64}$"))) and
    ($spec.surfaces | length == 15) and
    ([$spec.surfaces[].id] | sort == [
        "binding-amqp", "binding-http", "binding-kafka", "binding-mqtt",
        "binding-nats", "binding-websockets", "extension-dataref",
        "extension-distributed-tracing", "extension-partitioning",
        "extension-registry", "extension-sampledrate", "extension-sequence",
        "format-avro", "format-json", "format-protobuf"
    ]) and
    ([$spec.files[].path | select(test("^cloudevents/(formats|bindings|extensions)/") or . == "cloudevents/documented-extensions.md")] | sort) as $surface_documents |
    ([$spec.surfaces[].documents[]] | sort == $surface_documents) and
    (all($spec.surfaces[]; .kind | IN("format", "binding", "extension", "registry"))) and
    (all($spec.surfaces[]; .status | IN("supported", "supported-subset", "unsupported", "inventory-only"))) and
    ([$conformance.files[] | select(has("vendored_path"))] | length == 2) and
    ($javascript.lockfile_sha256 | test("^[0-9a-f]{64}$")) and
    ($javascript.edge_fixture_path == "testdata/interoperability/javascript-edge-batch.json") and
    ($javascript.edge_fixture_sha256 | test("^[0-9a-f]{64}$")) and
    ($node.version == "v24.19.0") and
    ($node.archives | keys | sort == ["darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64"]) and
    ($node.archives | map_values(.filename)) == {
        "darwin-arm64": "node-v24.19.0-darwin-arm64.tar.gz",
        "darwin-x64": "node-v24.19.0-darwin-x64.tar.gz",
        "linux-arm64": "node-v24.19.0-linux-arm64.tar.xz",
        "linux-x64": "node-v24.19.0-linux-x64.tar.xz"
    } and
    ([$node.archives[].sha256] | all(test("^[0-9a-f]{64}$")))
' "${manifest}" >/dev/null

jq -e --slurpfile manifest "${manifest}" '
    . as $matrix |
    ($manifest[0].sources[] | select(.name == "CloudEvents specification")) as $spec |
    .schema == 1 and
    .authority == {
        "repository": "https://github.com/cloudevents/spec",
        "version": $spec.version,
        "revision": $spec.revision,
        "manifest": "specification/manifest.json"
    } and
    (.supported_documents | sort == [
        "cloudevents/bindings/http-protocol-binding.md",
        "cloudevents/bindings/kafka-protocol-binding.md",
        "cloudevents/extensions/distributed-tracing.md",
        "cloudevents/extensions/partitioning.md",
        "cloudevents/formats/json-format.md",
        "cloudevents/spec.md"
    ]) and
    (.requirements | type == "array" and length == 140) and
    ([.requirements[] | select(.id | startswith("CE-CORE-"))] | length == 64) and
    ([.requirements[] | select(.id | startswith("CE-JSON-"))] | length == 26) and
    ([.requirements[] | select(.id | startswith("CE-HTTP-"))] | length == 28) and
    ([.requirements[] | select(.id | startswith("CE-KAFKA-"))] | length == 15) and
    ([.requirements[] | select(.id | startswith("CE-TRACE-"))] | length == 5) and
    ([.requirements[] | select(.id | startswith("CE-PARTITION-"))] | length == 2) and
    ([.requirements[].id] | length == (unique | length)) and
    all(.requirements[];
        . as $requirement |
        (.id | test("^CE-(CORE|JSON|HTTP|KAFKA|TRACE|PARTITION)-[0-9]{3}$")) and
        (.source.path as $path | $matrix.supported_documents | index($path) != null) and
        (.source.section | type == "string" and length > 0) and
        (.source.line | type == "number" and . > 0 and floor == .) and
        (.source.keyword_lines | type == "array" and length > 0 and
            all(.[]; type == "number" and . > 0 and floor == .)) and
        (.source.keyword_lines | index($requirement.source.line) != null) and
        (.text | type == "string" and test("\\b(MUST|REQUIRED|SHALL|SHOULD|RECOMMENDED|MAY|OPTIONAL)\\b")) and
        (.applicability | type == "string" and length > 0) and
        (.status | IN("supported", "unsupported", "not-applicable")) and
        (.implementation | type == "array" and all(.[]; type == "string" and length > 0)) and
        (.artifacts | type == "array" and all(.[]; type == "string" and length > 0)) and
        (if .status == "supported" then (.artifacts | length > 0)
         else (.limitation | type == "string" and length > 0)
         end)
    ) and
    ([.unsupported_surfaces[].id] | sort == [
        "binding-amqp", "binding-mqtt", "binding-nats", "binding-websockets",
        "extension-dataref", "extension-sampledrate", "extension-sequence",
        "format-avro", "format-protobuf"
    ]) and
    all(.unsupported_surfaces[];
        .status == "unsupported" and
        (.limitation | type == "string" and length > 0) and
        (.documents | type == "array" and length > 0) and
        . as $surface |
        any($spec.surfaces[];
            .id == $surface.id and .status == "unsupported" and
            (.documents | sort) == ($surface.documents | sort)
        )
    )
' "${requirements}" >/dev/null

jq -r '.requirements[] | .implementation[], .artifacts[]' "${requirements}" |
    sort -u |
    while IFS= read -r path; do
        if [[ ! -f "${module_root}/${path}" ]]; then
            printf 'normative matrix references missing artifact: %s\n' "${path}" >&2
            exit 1
        fi
    done

spec_revision="$(jq -er '.sources[] | select(.name == "CloudEvents specification") | .revision' "${manifest}")"
spec_file_count="$(jq -r '.sources[] | select(.name == "CloudEvents specification") | .files | length' "${manifest}")"
for ((index = 0; index < spec_file_count; index++)); do
    path="$(jq -er --argjson index "${index}" '.sources[] | select(.name == "CloudEvents specification") | .files[$index].path' "${manifest}")"
    expected="$(jq -er --argjson index "${index}" '.sources[] | select(.name == "CloudEvents specification") | .files[$index].sha256' "${manifest}")"
    source_file="${work}/spec-${index}"
    curl --fail --silent --show-error --location \
        "https://raw.githubusercontent.com/cloudevents/spec/${spec_revision}/${path}" \
        --output "${source_file}"
    actual="$(shasum -a 256 "${source_file}" | awk '{print $1}')"
    if [[ "${actual}" != "${expected}" ]]; then
        printf 'pinned specification source changed: %s\nexpected %s\nactual   %s\n' \
            "${path}" "${expected}" "${actual}" >&2
        exit 1
    fi
done

actual_lines="${work}/actual-normative-lines"
expected_lines="${work}/expected-normative-lines"
: >"${actual_lines}"
supported_count="$(jq -r '.supported_documents | length' "${requirements}")"
for ((index = 0; index < supported_count; index++)); do
    path="$(jq -er --argjson index "${index}" '.supported_documents[$index]' "${requirements}")"
    source_index="$(jq -er --arg path "${path}" '.sources[] | select(.name == "CloudEvents specification") | .files | to_entries[] | select(.value.path == $path) | .key' "${manifest}")"
    excluded_lines=""
    case "${path}" in
        cloudevents/spec.md) excluded_lines="44 45" ;;
        cloudevents/formats/json-format.md) excluded_lines="31 32" ;;
        cloudevents/bindings/http-protocol-binding.md) excluded_lines="40 41" ;;
        cloudevents/bindings/kafka-protocol-binding.md) excluded_lines="39 40" ;;
    esac
    SOURCE_PATH="${path}" EXCLUDED_LINES="${excluded_lines}" perl -ne '
        BEGIN {
            %excluded = map { $_ => 1 } split / /, $ENV{"EXCLUDED_LINES"};
        }
        next if $excluded{$.} || /^#/;
        if (/\b(?:MUST(?: NOT)?|REQUIRED|SHALL(?: NOT)?|SHOULD(?: NOT)?|RECOMMENDED|NOT RECOMMENDED|MAY|OPTIONAL)\b/) {
            print "$ENV{SOURCE_PATH}:$.\n";
        }
    ' "${work}/spec-${source_index}" >>"${actual_lines}"
done
sort -u "${actual_lines}" -o "${actual_lines}"
jq -r '.requirements[] | .source.path as $path | .source.keyword_lines[] | "\($path):\(.)"' "${requirements}" |
    sort -u >"${expected_lines}"
if ! diff -u "${actual_lines}" "${expected_lines}"; then
    printf 'normative requirement keyword-line inventory is incomplete\n' >&2
    exit 1
fi

conformance_name="CloudEvents conformance"
fixture_count="$(jq -r --arg name "${conformance_name}" '.sources[] | select(.name == $name) | [.files[] | select(has("vendored_path"))] | length' "${manifest}")"
for ((index = 0; index < fixture_count; index++)); do
    path="$(jq -er --arg name "${conformance_name}" --argjson index "${index}" '.sources[] | select(.name == $name) | [.files[] | select(has("vendored_path"))][$index].vendored_path' "${manifest}")"
    expected="$(jq -er --arg name "${conformance_name}" --argjson index "${index}" '.sources[] | select(.name == $name) | [.files[] | select(has("vendored_path"))][$index].vendored_sha256' "${manifest}")"
    actual="$(shasum -a 256 "${module_root}/${path}" | awk '{print $1}')"
    if [[ "${actual}" != "${expected}" ]]; then
        printf 'vendored conformance fixture digest mismatch: %s\n' "${path}" >&2
        exit 1
    fi
done

lock_path="$(jq -er '.sources[] | select(.name == "CloudEvents JavaScript SDK") | .lockfile_path' "${manifest}")"
lock_expected="$(jq -er '.sources[] | select(.name == "CloudEvents JavaScript SDK") | .lockfile_sha256' "${manifest}")"
lock_actual="$(shasum -a 256 "${module_root}/${lock_path}" | awk '{print $1}')"
if [[ "${lock_actual}" != "${lock_expected}" ]]; then
    printf 'JavaScript lockfile digest mismatch: %s\n' "${lock_path}" >&2
    exit 1
fi

edge_path="$(jq -er '.sources[] | select(.name == "CloudEvents JavaScript SDK") | .edge_fixture_path' "${manifest}")"
edge_expected="$(jq -er '.sources[] | select(.name == "CloudEvents JavaScript SDK") | .edge_fixture_sha256' "${manifest}")"
edge_actual="$(shasum -a 256 "${module_root}/${edge_path}" | awk '{print $1}')"
if [[ "${edge_actual}" != "${edge_expected}" ]]; then
    printf 'JavaScript edge fixture digest mismatch: %s\n' "${edge_path}" >&2
    exit 1
fi

printf 'verified 19 pinned specification sources, 140 normative requirements, and %s local pinned artifacts\n' "$((fixture_count + 2))"

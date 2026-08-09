#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${root}/specification/manifest.tsv"
register="${root}/docs/specification-decisions.md"

[[ -f "${manifest}" && -f "${register}" ]] || {
	printf 'missing wire specification evidence\n' >&2
	exit 1
}
expected_header=$'id\tversion\tsections\trole\turl\tsha256\tstatus'
[[ "$(head -n 1 "${manifest}")" == "${expected_header}" ]] || {
	printf 'invalid wire specification manifest header\n' >&2
	exit 1
}

seen='|'
count=0
while IFS=$'\t' read -r id version sections role url digest status; do
	[[ -n "${id}" ]] || continue
	[[ -n "${version}" && -n "${sections}" && -n "${role}" && -n "${url}" ]] || exit 1
	[[ "${seen}" != *"|${id}|"* ]] || { printf 'duplicate wire source: %s\n' "${id}" >&2; exit 1; }
	seen+="${id}|"
	[[ "${url}" == https://* && "${digest}" =~ ^[0-9a-f]{64}$ && "${status}" == pinned ]] || {
		printf 'invalid wire source row: %s\n' "${id}" >&2
		exit 1
	}
	count=$((count + 1))
done < <(tail -n +2 "${manifest}")

required=(go-encoding rfc8259 xml10 xml-names soap11 soap12 yaml122 toml110 msgpack rfc7049 rfc8949 ctap22 bson11)
[[ "${count}" -eq "${#required[@]}" ]] || exit 1
for id in "${required[@]}"; do
	[[ "${seen}" == *"|${id}|"* ]] || { printf 'missing wire source: %s\n' "${id}" >&2; exit 1; }
done
[[ "$(grep -Ec '^## WIRE-DEC-[0-9]{3}:' "${register}")" -eq 16 ]] || {
	printf 'wire decision register must contain 16 decisions\n' >&2
	exit 1
}
for field in \
	'Status, owner, and classification' \
	'Source and issue' \
	'Interpretations and peer behavior' \
	'Selected behavior and consequences' \
	'Evidence, public surface, upstream, and reconsideration'; do
	[[ "$(grep -Fc -- "- **${field}:**" "${register}")" -eq 16 ]] || {
		printf 'wire decision field must occur once per decision: %s\n' "${field}" >&2
		exit 1
	}
done

cd "${root}"
GOWORK=off go test ./... -run '^(TestSharedRepositoryContract|TestEveryDecoderDefinesEmptyWhitespaceTruncatedAndConcatenatedInput|TestEveryReaderStopsAtOneByteBeyondLimit|TestDetectFormat|TestDuplicateNameValidationDefinesEveryJSONShape|TestDecodeNamespaceAwareFixture|TestParseRejectsInvalidEnvelopeStructure|TestDecodeDefinesAliasAnchorAndMergeBehavior|TestDecodeRejectsMalformedDuplicateAndTrailingData|TestDecodeEnforcesDefaultStructuralLimits|TestEncodeUsesExplicitDeterministicProfiles|TestDecodeRejectsMalformedTrailingDuplicateAndScalarData|TestEncodeOrderedDocumentsAreDeterministic|TestErrorKindsMatchTheirSentinels|TestAllEncodePathsRejectCyclicValues|TestSharedDocumentationConventions)$' -count=1

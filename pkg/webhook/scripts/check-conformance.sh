#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${root}/specification/manifest.tsv"
register="${root}/docs/specification-decisions.md"

[[ -f "${manifest}" && -f "${register}" ]] || {
	printf 'missing webhook specification evidence\n' >&2
	exit 1
}
expected_header=$'id\tversion\tsections\trole\turl\tsha256\tstatus'
[[ "$(head -n 1 "${manifest}")" == "${expected_header}" ]] || {
	printf 'invalid webhook specification manifest header\n' >&2
	exit 1
}

seen='|'
count=0
while IFS=$'\t' read -r id version sections role url digest status; do
	[[ -n "${id}" ]] || continue
	[[ -n "${version}" && -n "${sections}" && -n "${role}" && -n "${url}" ]] || exit 1
	[[ "${seen}" != *"|${id}|"* ]] || {
		printf 'duplicate webhook source: %s\n' "${id}" >&2
		exit 1
	}
	seen+="${id}|"
	[[ "${url}" == https://* && "${digest}" =~ ^[0-9a-f]{64}$ && "${status}" == pinned ]] || {
		printf 'invalid webhook source row: %s\n' "${id}" >&2
		exit 1
	}
	count=$((count + 1))
done < <(tail -n +2 "${manifest}")

required=(
	go-webhook rfc2104 rfc4231 rfc4648 rfc3986 rfc3339 rfc8259 rfc8941 rfc9110
	rfc9421 iana-ipv4 iana-ipv6 cloudevents trace-context
)
[[ "${count}" -eq "${#required[@]}" ]] || exit 1
for id in "${required[@]}"; do
	[[ "${seen}" == *"|${id}|"* ]] || {
		printf 'missing webhook source: %s\n' "${id}" >&2
		exit 1
	}
done
[[ "$(grep -Ec '^## WEBHOOK-DEC-[0-9]{3}:' "${register}")" -eq 26 ]] || {
	printf 'webhook decision register must contain 26 decisions\n' >&2
	exit 1
}
for marker in \
	'Status, owner, and classification' \
	'Source and issue' \
	'Interpretations and peer behavior' \
	'Selected behavior and consequences' \
	'Evidence, public surface, upstream, and reconsideration'
do
	[[ "$(grep -Fc -- "- **${marker}:**" "${register}")" -eq 26 ]] || {
		printf 'webhook decision register has incomplete %s fields\n' "${marker}" >&2
		exit 1
	}
done
[[ "$(grep -E '^## WEBHOOK-DEC-[0-9]{3}:' "${register}" | cut -d: -f1 | sort -u | wc -l | tr -d ' ')" -eq 26 ]] || {
	printf 'webhook decision identifiers must be unique\n' >&2
	exit 1
}

cd "${root}"
python3 scripts/check_interoperability.py
tests=(
	TestCanonicalizeProducesStableVersionedBytes
	TestSignerAndVerifierSupportSHA256AndSHA512
	TestVerifierRejectsMutationOfEverySignedComponent
	TestParseSignatureHeadersRejectsMalformedOrAmbiguousInput
	TestCaptureBodyPreservesExactBytesAndRestoresRequest
	TestVerifyAndRecordAtomicallyRejectsReplay
	TestEnvelopeMarshalIsDeterministicAndPreservesData
	TestDeliverRetriesRetryableStatusAndHonorsRetryAfter
	TestDeliverOnceDisablesInternalRetries
	TestSSRFPolicyRejectsUnsafeURLsAndAddresses
	TestSecureHTTPClientRevalidatesDNSAtDialTime
	TestSecureHTTPClientRejectsRedirectWithoutContactingTarget
	TestFanOutBoundsConcurrencyAndPreservesResultOrder
	TestVerifierObservesVerificationAndReplayWithoutSensitiveData
	TestDeliveryRequestWireRoundTripIsDeterministic
)
pattern="$(IFS='|'; printf '^(%s)$' "${tests[*]}")"
GOWORK=off go test . -run "${pattern}" -count=1

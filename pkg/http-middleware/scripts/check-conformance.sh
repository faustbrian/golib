#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${root}/specification/manifest.tsv"
register="${root}/docs/specification-decisions.md"

[[ -f "${manifest}" ]] || {
	printf 'missing HTTP middleware specification manifest\n' >&2
	exit 1
}
[[ -f "${register}" ]] || {
	printf 'missing HTTP middleware specification decision register\n' >&2
	exit 1
}

expected_header=$'id\tversion\tsections\trole\turl\tsha256\tstatus'
[[ "$(head -n 1 "${manifest}")" == "${expected_header}" ]] || {
	printf 'invalid HTTP middleware specification manifest header\n' >&2
	exit 1
}

seen='|'
count=0
while IFS=$'\t' read -r id version sections role url digest status; do
	[[ -n "${id}" ]] || continue
	[[ -n "${version}" && -n "${sections}" && -n "${role}" && -n "${url}" ]] || {
		printf 'incomplete HTTP middleware specification row\n' >&2
		exit 1
	}
	[[ "${seen}" != *"|${id}|"* ]] || {
		printf 'duplicate HTTP middleware specification row: %s\n' "${id}" >&2
		exit 1
	}
	seen+="${id}|"
	[[ "${url}" == https://* ]] || {
		printf 'non-HTTPS HTTP middleware specification source: %s\n' "${id}" >&2
		exit 1
	}
	[[ "${digest}" =~ ^[0-9a-f]{64}$ ]] || {
		printf 'invalid HTTP middleware specification digest: %s\n' "${id}" >&2
		exit 1
	}
	[[ "${status}" == "pinned" ]] || {
		printf 'unrecognized HTTP middleware specification status: %s\n' "${id}" >&2
		exit 1
	}
	count=$((count + 1))
done < <(tail -n +2 "${manifest}")

required=(
	go-net-http rfc9110 rfc9111 rfc7239 rfc6797 rfc7034 whatwg-fetch
	whatwg-url w3c-referrer-policy
)
[[ "${count}" -eq "${#required[@]}" ]] || {
	printf 'HTTP middleware specification row count = %s, want %s\n' "${count}" "${#required[@]}" >&2
	exit 1
}
for id in "${required[@]}"; do
	[[ "${seen}" == *"|${id}|"* ]] || {
		printf 'missing HTTP middleware specification row: %s\n' "${id}" >&2
		exit 1
	}
done

decision_count="$(grep -Ec '^## HTTPMIDDLEWARE-DEC-[0-9]{3}:' "${register}")"
[[ "${decision_count}" -eq 15 ]] || {
	printf 'HTTP middleware decision count = %s, want 15\n' "${decision_count}" >&2
	exit 1
}

cd "${root}"
GOWORK=off go test ./... -run '^(TestChainExecutesInDeclaredOrderAndUnwindsInReverse|TestUntrustedInboundIdentifierIsReplacedAndPropagated|TestRecoveryDoesNotRewriteCommittedResponse|TestLimitCountsEncodedAndMultipartTransportBytes|TestDeadlineNeverExtendsParent|TestTrustedProxySelectsFirstUntrustedClient|TestCredentialedWildcardOriginIsRejected|TestHSTSRequiresDeploymentAcknowledgement|TestGzipNegotiationHonorsQualityAndMergesVary|TestDuplicateContentTypeIsRejected|TestTrackingWrappersPreserveExactOptionalInterfaces|TestObserverReceivesOneBoundedCompletionEvent|TestImmediateAdmissionRejectsAboveLimitAndReleasesPermit|TestNoStoreAppliesToEveryDownstreamStatus|TestGoServiceOwnershipRejectsDuplicateCoreMiddleware|TestRepresentativeJSONRPCProfile|TestRepresentativeWebhookPreservesRawSignedBody|TestRealListenerHTTP1AndHTTP2PreserveFlushAndTrailers)$' -count=1

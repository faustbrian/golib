#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${root}/specification/manifest.tsv"
register="${root}/docs/specification-decisions.md"

[[ -f "${manifest}" ]] || {
	printf 'missing HTTP client specification manifest\n' >&2
	exit 1
}
[[ -f "${register}" ]] || {
	printf 'missing HTTP client specification decision register\n' >&2
	exit 1
}

expected_header=$'id\tversion\tsections\trole\turl\tsha256\tstatus'
[[ "$(head -n 1 "${manifest}")" == "${expected_header}" ]] || {
	printf 'invalid HTTP client specification manifest header\n' >&2
	exit 1
}

seen='|'
count=0
while IFS=$'\t' read -r id version sections role url digest status; do
	[[ -n "${id}" ]] || continue
	[[ -n "${id}" && -n "${version}" && -n "${sections}" && -n "${role}" && -n "${url}" ]] || {
		printf 'incomplete HTTP client specification row\n' >&2
		exit 1
	}
	[[ "${seen}" != *"|${id}|"* ]] || {
		printf 'duplicate HTTP client specification row: %s\n' "${id}" >&2
		exit 1
	}
	seen+="${id}|"
	[[ "${url}" == https://* ]] || {
		printf 'non-HTTPS HTTP client specification source: %s\n' "${id}" >&2
		exit 1
	}
	[[ "${digest}" =~ ^[0-9a-f]{64}$ ]] || {
		printf 'invalid HTTP client specification digest: %s\n' "${id}" >&2
		exit 1
	}
	[[ "${status}" == "pinned" ]] || {
		printf 'unrecognized HTTP client specification status: %s\n' "${id}" >&2
		exit 1
	}
	count=$((count + 1))
done < <(tail -n +2 "${manifest}")

required=(
	rfc3986 rfc9110 rfc9111 rfc8288 rfc7617 rfc6750 rfc6749 rfc6265
	rfc8259 rfc8470 rfc6585 w3c-trace-context-1
)
[[ "${count}" -eq "${#required[@]}" ]] || {
	printf 'HTTP client specification row count = %s, want %s\n' "${count}" "${#required[@]}" >&2
	exit 1
}
for id in "${required[@]}"; do
	[[ "${seen}" == *"|${id}|"* ]] || {
		printf 'missing HTTP client specification row: %s\n' "${id}" >&2
		exit 1
	}
done

decision_count="$(grep -Ec '^## HTTPCLIENT-DEC-[0-9]{3}:' "${register}")"
[[ "${decision_count}" -eq 18 ]] || {
	printf 'HTTP client decision count = %s, want 18\n' "${decision_count}" >&2
	exit 1
}

cd "${root}"
GOWORK=off go test ./... -run '^(TestNewRequestSpecResolvesSameOriginReference|TestRequestSpecTrailersReachStandardHTTPServer|TestAuthenticationMiddlewareReappliesOnlyWithinTrustedOrigin|TestCredentialEditorsApplyBasicBearerAndAPIKeys|TestClientCredentialsTokenSourceSupportsExplicitParameterAuthentication|TestRetryReplaysSafeRequestsWithDeterministicBackoff|TestRetryUsesBoundedRetryAfterAndClosesDiscardedResponses|TestRateLimitHeaderObservationMatrix|TestSharedCacheDoesNotReuseAuthorizationWithoutExplicitPermission|TestCacheMiddlewareHonorsOnlyIfCachedMaxStaleAndStaleIfError|TestCompressionMiddlewareDecodesGzipWithExplicitMetadata|TestRangePolicyRejectsUnsafeRequestsAndMismatchedResponses|TestLinkPaginatorParsesAndResolvesRFCLinks|TestSessionRedirectPolicyControlsCrossOriginCookies|TestW3CTraceContextIsValidatedAndInjectedOnTrustedAttempts|TestDecodeJSONResponseRejectsMediaTypeLimitAndTrailingData|TestIdempotencyKeyIsStableAcrossRetriesAndDistinctAcrossOperations|TestCursorPaginatorPreservesOpaqueCursorExactly|TestPipelineRunsAttemptScopeForEveryAttempt|TestRequestSpecStreamingBodyIsExplicitlyOneShot|TestClassifyResponseLeavesAcceptedBodyCallerOwned)$' -count=1

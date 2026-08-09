#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${root}/specification/manifest.tsv"
register="${root}/docs/specification-decisions.md"

[[ -f "${manifest}" && -f "${register}" ]] || {
	printf 'missing router specification evidence\n' >&2
	exit 1
}
expected_header=$'id\tversion\tsections\trole\turl\tsha256\tstatus'
[[ "$(head -n 1 "${manifest}")" == "${expected_header}" ]] || {
	printf 'invalid router specification manifest header\n' >&2
	exit 1
}

seen='|'
count=0
while IFS=$'\t' read -r id version sections role url digest status; do
	[[ -n "${id}" ]] || continue
	[[ -n "${version}" && -n "${sections}" && -n "${role}" && -n "${url}" ]] || exit 1
	[[ "${seen}" != *"|${id}|"* ]] || { printf 'duplicate router source: %s\n' "${id}" >&2; exit 1; }
	seen+="${id}|"
	[[ "${url}" == https://* && "${digest}" =~ ^[0-9a-f]{64}$ && "${status}" == pinned ]] || {
		printf 'invalid router source row: %s\n' "${id}" >&2
		exit 1
	}
	count=$((count + 1))
done < <(tail -n +2 "${manifest}")

required=(go-net-http rfc3986 rfc9110 rfc9112)
[[ "${count}" -eq "${#required[@]}" ]] || exit 1
for id in "${required[@]}"; do
	[[ "${seen}" == *"|${id}|"* ]] || { printf 'missing router source: %s\n' "${id}" >&2; exit 1; }
done
[[ "$(grep -Ec '^## ROUTER-DEC-[0-9]{3}:' "${register}")" -eq 13 ]] || {
	printf 'router decision register must contain 13 decisions\n' >&2
	exit 1
}

cd "${root}"
GOWORK=off go test ./... -run '^(TestSupportedMatchingIsDifferentialWithServeMux|TestCompileReturnsTypedConflictsAndFreezesOnlyOnSuccess|TestCompiledRouterPreservesHTTPMethodSemantics|TestAsteriskOptionsAndMalformedAuthority|TestCanonicalRedirectsPrecedeRouteAndMethodSelection|TestHostPatternsMatchPortsAndSingleLabels|TestNestedGroupsFlattenComposition|TestMiddlewareOrderAndIntrospectionAreStableAndImmutable|TestMountStripsPathOnCloneAndPreservesRequestTarget|TestCompiledRouterDispatchesWithPathValuesAndMatchedRoute|TestNamedPathGenerationEscapesSegmentsAndRoundTrips|TestAbsoluteURLGenerationValidatesBaseHostAndQuery|TestRegisterEnforcesLimitsAndBoundsDiagnostics)$' -count=1

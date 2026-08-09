#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${root}/specification/manifest.tsv"
expected_header=$'id\tversion\trole\tstatus\tsha256\tbytes\turl\tvalue'

IFS= read -r header <"${manifest}"
if [[ "${header}" != "${expected_header}" ]]; then
	printf '%s\n' 'invalid authentication specification manifest header' >&2
	exit 1
fi

count=0
seen='|'
while IFS=$'\t' read -r id version role status digest bytes url value; do
	[[ -n "${id}" && -n "${version}" && -n "${role}" ]] || {
		printf 'incomplete authentication specification row: %s\n' "${id}" >&2
		exit 1
	}
	if [[ "${seen}" == *"|${id}|"* ]]; then
		printf 'duplicate authentication specification row: %s\n' "${id}" >&2
		exit 1
	fi
	seen+="${id}|"
	case "${id}" in
		rfc7617-aladdin)
			expected_version='RFC-7617'
			expected_role='basic-aladdin-credential'
			expected_url='https://www.rfc-editor.org/rfc/rfc7617.html#section-2'
			;;
		rfc7617-utf8-pound)
			expected_version='RFC-7617'
			expected_role='basic-utf8-credential'
			expected_url='https://www.rfc-editor.org/rfc/rfc7617.html#section-2.1'
			;;
		rfc6750-bearer-token)
			expected_version='RFC-6750'
			expected_role='bearer-b64token'
			expected_url='https://www.rfc-editor.org/rfc/rfc6750.html#section-2.1'
			;;
		*)
			printf 'unknown authentication specification row: %s\n' "${id}" >&2
			exit 1
			;;
	esac
	if [[ "${version}" != "${expected_version}" ||
		"${role}" != "${expected_role}" ||
		"${url}" != "${expected_url}" ]]; then
		printf 'changed authentication specification identity: %s\n' "${id}" >&2
		exit 1
	fi
	[[ "${status}" == "pinned" ]] || {
		printf 'unrecognized authentication specification status: %s\n' "${id}" >&2
		exit 1
	}
	actual_digest="$(printf '%s' "${value}" | shasum -a 256 | awk '{print $1}')"
	actual_bytes="$(printf '%s' "${value}" | wc -c | tr -d ' ')"
	if [[ "${actual_digest}" != "${digest}" || "${actual_bytes}" != "${bytes}" ]]; then
		printf 'stale authentication specification vector: %s\n' "${id}" >&2
		exit 1
	fi
	count=$((count + 1))
done < <(tail -n +2 "${manifest}")

if [[ "${count}" -ne 3 ]]; then
	printf 'authentication specification vector count = %s, want 3\n' "${count}" >&2
	exit 1
fi

cd "${root}"
GOWORK=off go test ./authhttp \
	-run '^(TestRFC7617BasicCredentialVectors|TestRFC6750BearerHeaderVector)$' \
	-count=1

#!/bin/sh
set -eu

repository_root=${1:?repository root is required}

if [ "${API_BASE_REF+x}" = x ]; then
	base_ref=$API_BASE_REF
	if ! git -C "$repository_root" cat-file -e "$base_ref^{commit}" 2>/dev/null; then
		echo "API baseline is unavailable: $base_ref" >&2
		exit 1
	fi
	printf '%s\n' "$base_ref"
	exit 0
fi

if git -C "$repository_root" cat-file -e 'HEAD^^{commit}' 2>/dev/null; then
	printf '%s\n' 'HEAD^'
	exit 0
fi

if git -C "$repository_root" cat-file -e 'HEAD^{commit}' 2>/dev/null; then
	printf '%s\n' HEAD
	exit 0
fi

echo 'API baseline is unavailable: HEAD' >&2
exit 1

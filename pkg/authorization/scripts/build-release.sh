#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -lt 1 || "$#" -gt 2 ]]; then
    echo "usage: $0 vMAJOR.MINOR.PATCH [GIT_REF]" >&2
    exit 2
fi

version="$1"
ref="${2:-$version}"
if [[ ! "${version}" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    echo "invalid semantic version: ${version}" >&2
    exit 2
fi

git rev-parse --verify "${ref}^{commit}" >/dev/null
mkdir -p dist
archive="dist/authorization-${version}.tar.gz"

git archive --format=tar --prefix="authorization-${version}/" "${ref}" |
    gzip -n -9 >"${archive}"
shasum -a 256 "${archive}" >"${archive}.sha256"

printf '%s\n' "${archive}" "${archive}.sha256"

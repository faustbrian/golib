#!/usr/bin/env bash
set -euo pipefail

version="${1:?release version is required}"
output="${2:?output directory is required}"

if [[ ! "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "release version must be a v-prefixed semantic version" >&2
  exit 1
fi
if [[ -e "${output}" ]]; then
  echo "release output already exists: ${output}" >&2
  exit 1
fi

mkdir -p "${output}"
name="cli-${version}"
repository="$(git rev-parse --show-toplevel)"
git -C "${repository}" archive --format=tar --prefix="${name}/" HEAD:cli |
  gzip -n -9 >"${output}/${name}.tar.gz"

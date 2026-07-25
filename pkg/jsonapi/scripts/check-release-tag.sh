#!/usr/bin/env bash
set -euo pipefail

tag="${1:-}"
core='(0|[1-9][0-9]*)'
identifier='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
pattern="^v${core}\\.${core}\\.${core}(-${identifier}(\\.${identifier})*)?$"

if [[ ! "$tag" =~ $pattern ]]; then
  echo "invalid release tag: $tag" >&2
  echo "expected vMAJOR.MINOR.PATCH with an optional SemVer prerelease" >&2
  exit 1
fi

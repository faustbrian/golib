#!/usr/bin/env bash
set -euo pipefail

valid=(
  v0.0.0
  v1.2.3
  v1.2.3-alpha
  v1.2.3-alpha.1
  v1.2.3-0.3.7
  v1.2.3-x.7.z.92
)

invalid=(
  ''
  1.2.3
  v01.2.3
  v1.02.3
  v1.2.03
  v1.2
  v1.2.3-
  v1.2.3.foo
  v1.2.3-01
  v1.2.3-alpha..1
  v1.2.3+build
)

for tag in "${valid[@]}"; do
  ./scripts/check-release-tag.sh "$tag"
done

for tag in "${invalid[@]}"; do
  if ./scripts/check-release-tag.sh "$tag" >/dev/null 2>&1; then
    echo "accepted invalid release tag: $tag" >&2
    exit 1
  fi
done

echo "release tag validation accepts the documented SemVer subset"

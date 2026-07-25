#!/usr/bin/env bash
set -euo pipefail

directory="$(mktemp -d)"
trap 'rm -rf "${directory}"' EXIT

for package in . ./internal/engine; do
  name="${package//\//-}"
  profile="${directory}/${name}.out"
  GOWORK=off go test "${package}" -covermode=atomic -coverprofile="${profile}"
  coverage="$(go tool cover -func="${profile}" | awk '/^total:/ {sub(/%/, "", $3); print $3}')"
  if [[ "${coverage}" != "100.0" ]]; then
    echo "production statement coverage for ${package} is ${coverage}%; required: 100.0%" >&2
    exit 1
  fi
done

# clitest and reference generation are test/development support rather than
# production command execution packages; their own behavior tests still run.
GOWORK=off go test ./clitest ./internal/referenceapp ./cmd/generate-reference

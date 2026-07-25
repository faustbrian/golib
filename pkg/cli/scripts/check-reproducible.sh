#!/usr/bin/env bash
set -euo pipefail

first="$(mktemp)"
second="$(mktemp)"
trap 'rm -f "${first}" "${second}"' EXIT

GOWORK=off go build -trimpath -buildvcs=false -o "${first}" ./cmd/generate-reference
GOWORK=off go build -trimpath -buildvcs=false -o "${second}" ./cmd/generate-reference
cmp "${first}" "${second}"

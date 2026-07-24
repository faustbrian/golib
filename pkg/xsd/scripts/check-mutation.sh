#!/usr/bin/env bash
set -euo pipefail

version="v0.6.0"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
root="$(./scripts/prepare-xsts.sh "$work")"

XSTS_ROOT="$root" go run "github.com/go-gremlins/gremlins/cmd/gremlins@${version}" unleash . \
  --integration --coverpkg ./... --workers 8 --test-cpu 1 --timeout-coefficient 10 \
  --threshold-mcover 100 --threshold-efficacy 65 \
  --output mutation-results.json

#!/usr/bin/env bash
set -euo pipefail

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

root="$(./scripts/prepare-xsts.sh "$work")"
XSTS_ROOT="$root" go test ./xsdtest -run '^TestOfficialXSTS$' -count=1

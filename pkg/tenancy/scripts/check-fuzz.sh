#!/usr/bin/env bash
set -euo pipefail

duration="${1:-2s}"
for target in FuzzTenantIDRoundTrip FuzzPropagationExtraction; do
    GOWORK=off go test . -run '^$' -fuzz "^${target}$" -fuzztime="${duration}"
done

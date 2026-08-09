#!/usr/bin/env bash
set -euo pipefail

duration="${1:-2s}"
for package_target in \
    '.:FuzzTenantIDRoundTrip' \
    '.:FuzzPropagationExtraction' \
    './http:FuzzHTTPHeaderExtraction' \
    './jsonrpc:FuzzJSONRPCMetadata'; do
    package="${package_target%%:*}"
    target="${package_target#*:}"
    GOWORK=off go test "${package}" -run '^$' -fuzz "^${target}$" -fuzztime="${duration}"
done

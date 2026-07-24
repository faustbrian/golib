#!/usr/bin/env bash
set -euo pipefail

: "${TEST_REDIS_ADDRESS:?TEST_REDIS_ADDRESS is required}"
: "${TEST_VALKEY_ADDRESS:?TEST_VALKEY_ADDRESS is required}"

go test \
    -tags=integration \
    -count=1 \
    -timeout=5m \
    -run '^TestRealGoQueueBackendsThroughManagementHTTP$' \
    ./dataplane

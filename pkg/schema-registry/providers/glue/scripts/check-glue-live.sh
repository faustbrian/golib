#!/usr/bin/env bash
set -euo pipefail

: "${SCHEMA_REGISTRY_GLUE_INTEGRATION_REGION:?SCHEMA_REGISTRY_GLUE_INTEGRATION_REGION is required}"
: "${SCHEMA_REGISTRY_GLUE_INTEGRATION_REGISTRY:?SCHEMA_REGISTRY_GLUE_INTEGRATION_REGISTRY is required}"
: "${SCHEMA_REGISTRY_GLUE_INTEGRATION_SCHEMA:?SCHEMA_REGISTRY_GLUE_INTEGRATION_SCHEMA is required}"

../../scripts/with-provider-gocache.sh go test -tags=liveintegration . -run '^TestProviderAgainstLiveAWSGlueService$' -count=1

#!/usr/bin/env bash
set -euo pipefail

tool_dir="$(mktemp -d)"
trap 'rm -rf "${tool_dir}"' EXIT

GOBIN="${tool_dir}" go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
"${tool_dir}/gremlins" unleash \
    --exclude-files '^(_browser|apihttp|cli|client|cmd|fleet|history|kubernetes|postgres|server|ui)/' \
    --workers "${MUTATION_WORKERS:-4}" \
    --test-cpu 1 \
    --timeout-coefficient 5 \
    --threshold-efficacy 100 \
    --threshold-mcover 100 \
    --output-statuses lctv

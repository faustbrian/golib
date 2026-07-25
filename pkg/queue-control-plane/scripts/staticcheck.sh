#!/usr/bin/env bash
set -euo pipefail

tool_dir="$(mktemp -d)"
trap 'rm -rf "${tool_dir}"' EXIT

GOBIN="${tool_dir}" go install honnef.co/go/tools/cmd/staticcheck@v0.7.0
"${tool_dir}/staticcheck" ./...

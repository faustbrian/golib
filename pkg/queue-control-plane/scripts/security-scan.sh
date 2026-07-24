#!/usr/bin/env bash
set -euo pipefail

tool_dir="$(mktemp -d)"
trap 'rm -rf "${tool_dir}"' EXIT

GOBIN="${tool_dir}" go install golang.org/x/vuln/cmd/govulncheck@v1.6.0
"${tool_dir}/govulncheck" ./...

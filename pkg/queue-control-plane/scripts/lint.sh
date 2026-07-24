#!/usr/bin/env bash
set -euo pipefail

tool_dir="$(mktemp -d)"
trap 'rm -rf "${tool_dir}"' EXIT

GOBIN="${tool_dir}" go install \
    github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
"${tool_dir}/golangci-lint" config verify
"${tool_dir}/golangci-lint" run ./...

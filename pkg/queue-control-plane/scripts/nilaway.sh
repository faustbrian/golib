#!/usr/bin/env bash
set -euo pipefail

tool_dir="$(mktemp -d)"
trap 'rm -rf "${tool_dir}"' EXIT

GOBIN="${tool_dir}" go install \
    go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260716185712-44c4bc80d5d2

set +e
"${tool_dir}/nilaway" \
    -include-pkgs='github.com/faustbrian/golib/pkg/queue-control-plane' \
    ./...
status=$?
set -e

if (( status != 0 )); then
    printf 'NilAway reported advisory findings (exit %d).\n' "${status}" >&2
fi

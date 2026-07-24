#!/usr/bin/env bash
set -euo pipefail

unformatted="$(gofmt -l .)"
if [[ -n "${unformatted}" ]]; then
    echo "The following Go files are not formatted:"
    echo "${unformatted}"
    exit 1
fi

go mod tidy -diff

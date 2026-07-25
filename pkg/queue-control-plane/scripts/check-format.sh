#!/usr/bin/env bash
set -euo pipefail

unformatted="$(gofmt -l .)"
if [[ -n "${unformatted}" ]]; then
    printf 'gofmt is required for:\n%s\n' "${unformatted}" >&2
    exit 1
fi

#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
    printf 'usage: %s <module-directory>\n' "$0" >&2
    exit 2
fi

module="$1"
root="$(git rev-parse --show-toplevel)"
cd "${root}"
exec go run ./cmd/golib safety -module "${module}"

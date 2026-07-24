#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
    printf 'usage: %s <state-file>\n' "$0" >&2
    exit 2
fi

state_file="$1"
[[ -f "${state_file}" ]] || exit 0
while IFS= read -r container; do
    [[ -n "${container}" ]] || continue
    docker rm --force "${container}" >/dev/null 2>&1 || true
done <"${state_file}"

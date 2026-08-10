#!/usr/bin/env bash
set -euo pipefail

lock_file="spec/sources.lock.json"
while IFS=$'\t' read -r name url expected; do
    actual="$(curl --fail --silent --show-error --location "$url" | shasum -a 256 | awk '{print $1}')"
    if [[ "$actual" != "$expected" ]]; then
        printf 'pinned source changed: %s\nexpected %s\nactual   %s\n' "$name" "$expected" "$actual" >&2
        exit 1
    fi
    printf 'verified %s\n' "$name"
done < <(jq -r '.sources[] | [.name, .url, .sha256] | @tsv' "$lock_file")

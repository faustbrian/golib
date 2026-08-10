#!/usr/bin/env bash
set -euo pipefail

task_gocache="$(mktemp -d)"
cleanup() {
    chmod -R u+w "$task_gocache" 2>/dev/null || true
    find "$task_gocache" -mindepth 1 -delete
    rmdir "$task_gocache"
}
trap cleanup EXIT

GOCACHE="$task_gocache" "$@"

#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary_root="$(mktemp -d -t cloudevents-javascript-interop.XXXXXX)"

cleanup() {
    find "${temporary_root}" -type f -delete
    find "${temporary_root}" -type l -delete
    find "${temporary_root}" -depth -type d -empty -delete
}
trap cleanup EXIT

export npm_config_cache="${temporary_root}/npm-cache"
runtime=()
if [[ "$(node --version 2>/dev/null || true)" != "v24.13.0" ]]; then
    if ! command -v fnm >/dev/null 2>&1; then
        printf 'Node.js v24.13.0 or fnm with that runtime is required\n' >&2
        exit 1
    fi
    runtime=(fnm exec --using=24.13.0)
fi

mkdir -p "${temporary_root}/project"
cp "${module_root}/scripts/javascript/package.json" "${temporary_root}/project/package.json"
cp "${module_root}/scripts/javascript/package-lock.json" "${temporary_root}/project/package-lock.json"
(
    cd "${temporary_root}/project"
    "${runtime[@]}" npm ci \
        --ignore-scripts \
        --no-audit \
        --no-fund \
        >/dev/null
)

NODE_PATH="${temporary_root}/project/node_modules" \
    "${runtime[@]}" node \
    "${module_root}/scripts/javascript-interop.cjs" \
    "${module_root}/testdata/interoperability"

"${GO:-go}" test . -run '^TestJavaScriptSDK' -count=1

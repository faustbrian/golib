#!/usr/bin/env bash
set -euo pipefail

module_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scratch="$(mktemp -d)"

cleanup() {
    chmod -R u+w "$scratch" 2>/dev/null || true
    find "$scratch" -mindepth 1 -delete
    rmdir "$scratch"
}
trap cleanup EXIT

mkdir -p "$scratch/gocache" "$scratch/gomodcache"
(
    cd "$scratch"
    GOWORK=off go work init "$module_dir" "$module_dir/../.."
    GOWORK="$scratch/go.work" go work edit \
        -replace="github.com/faustbrian/golib/pkg/http-signature@v0.0.0=$module_dir/../.."
)
cd "$module_dir"
GOWORK="$scratch/go.work" GOCACHE="$scratch/gocache" \
    GOMODCACHE="$scratch/gomodcache" go test -mod=readonly ./... -count=1

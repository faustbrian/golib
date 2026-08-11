#!/usr/bin/env bash
set -euo pipefail

mode="${1:-test}"
module_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
core_dir="$(cd "$module_dir/../.." && pwd)"
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
    GOWORK=off go work init "$module_dir" "$core_dir"
    GOWORK="$scratch/go.work" go work edit \
        -replace="github.com/faustbrian/golib/pkg/http-signature@v0.0.0=$core_dir"
)

cd "$module_dir"
common=(env "GOWORK=$scratch/go.work" "GOCACHE=$scratch/gocache" "GOMODCACHE=$scratch/gomodcache" go test -mod=readonly ./...)
case "$mode" in
test)
    "${common[@]}" -count=1
    ;;
benchmark)
    go version
    go env GOOS GOARCH GOAMD64 GOARM64
    uname -mrs
    sysctl -n machdep.cpu.brand_string 2>/dev/null || true
    printf 'GOMAXPROCS=%s\n' "${GOMAXPROCS:-default}"
    "${common[@]}" -run '^$' -bench . -benchmem \
        -benchtime="${BENCH_TIME:-1s}" -count="${BENCH_COUNT:-10}"
    ;;
*)
    printf 'unknown comparison mode: %s\n' "$mode" >&2
    exit 2
    ;;
esac

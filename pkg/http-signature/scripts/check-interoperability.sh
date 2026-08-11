#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scratch="$(mktemp -d)"
cleanup() {
    chmod -R u+w "$scratch" 2>/dev/null || true
    find "$scratch" -mindepth 1 -delete
    rmdir "$scratch"
}
trap cleanup EXIT

run_peer() {
    local name="$1"
    local repository="$2"
    local revision="$3"
    local tests="$4"
    local peer="$scratch/$name"
    local task_gocache="$scratch/${name}-gocache"
    local task_gomodcache="$scratch/${name}-gomodcache"

    mkdir -p "$peer" "$task_gocache" "$task_gomodcache"
    git -C "$peer" init -q
    git -C "$peer" remote add origin "$repository"
    git -C "$peer" fetch -q --depth 1 origin "$revision"
    git -C "$peer" checkout -q --detach FETCH_HEAD
    (
        cd "$peer"
        GOCACHE="$task_gocache" GOMODCACHE="$task_gomodcache" GOWORK=off \
            go test -mod=readonly ./... -run "$tests" -count=1
    )
    chmod -R u+w "$task_gocache" "$task_gomodcache" 2>/dev/null || true
    find "$task_gocache" "$task_gomodcache" -mindepth 1 -delete
    rmdir "$task_gocache" "$task_gomodcache"
    printf 'verified peer %s at %s\n' "$name" "$revision"
}

run_peer \
    yaronf-httpsign \
    https://github.com/yaronf/httpsign.git \
    de382d35c1add89cc09b9355161d61471fb7f632 \
    '^(TestVerifyRequest|TestVerifyResponse)$'

run_peer \
    dadrus-httpsig \
    https://github.com/dadrus/httpsig.git \
    0f24bf7dd9b76727af985d9a6f7ce87207a18387 \
    '^TestVerifierVerify$'

"${module_root}/differential/shared-corpus/check.sh"

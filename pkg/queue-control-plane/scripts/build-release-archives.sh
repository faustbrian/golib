#!/usr/bin/env bash
set -euo pipefail

: "${VERSION:?VERSION is required}"
: "${COMMIT:?COMMIT is required}"
: "${BUILT_AT:?BUILT_AT is required}"
: "${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required}"

if ! tar --version | head -1 | grep -q 'GNU tar'; then
    printf 'GNU tar is required for reproducible release archives\n' >&2
    exit 1
fi

dist="${DIST_DIR:-dist}"
mkdir -p "${dist}"
if find "${dist}" -mindepth 1 -print -quit | grep -q .; then
    printf 'release output directory is not empty: %s\n' "${dist}" >&2
    exit 1
fi

workspace="$(mktemp -d)"
trap 'rm -rf "${workspace}"' EXIT

build_archive() {
    local binary="$1"
    local package="$2"
    local goos="$3"
    local goarch="$4"
    local suffix=""
    local archive_root="${binary}_${VERSION}_${goos}_${goarch}"
    local stage="${workspace}/${archive_root}"
    local ldflags="-buildid= -s -w"

    if [[ "${goos}" == "windows" ]]; then
        suffix=".exe"
    fi
    if [[ "${binary}" == "queue-control-plane" ]]; then
        ldflags+=" -X main.buildVersion=${VERSION}"
        ldflags+=" -X main.buildCommit=${COMMIT}"
        ldflags+=" -X main.buildTime=${BUILT_AT}"
    fi

    mkdir -p "${stage}"
    CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build \
        -trimpath \
        -ldflags="${ldflags}" \
        -o "${stage}/${binary}${suffix}" \
        "${package}"
    cp LICENSE README.md "${stage}/"
    tar \
        --sort=name \
        --mtime="@${SOURCE_DATE_EPOCH}" \
        --owner=0 \
        --group=0 \
        --numeric-owner \
        --format=gnu \
        -C "${workspace}" \
        -czf "${dist}/${archive_root}.tar.gz" \
        "${archive_root}"
    rm -rf "${stage}"
}

for target in \
    linux/amd64 linux/arm64 \
    darwin/amd64 darwin/arm64 \
    windows/amd64 windows/arm64; do
    goos="${target%/*}"
    goarch="${target#*/}"
    build_archive \
        queue-control-plane \
        ./cmd/queue-control-plane \
        "${goos}" \
        "${goarch}"
    build_archive \
        queue-control \
        ./cmd/queue-control \
        "${goos}" \
        "${goarch}"
done

(
    cd "${dist}"
    shasum -a 256 ./*.tar.gz | LC_ALL=C sort -k2 >SHA256SUMS
)

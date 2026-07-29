#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
release="v5.4.0"
release_commit="88e9fb8f10ed89805aa3110d0a2cd5dcadc19689"
archive_sha256="92cf1b47ad12fb27163261fc3c1cea5df72439cab507983d06b56c94f8741909"
license_sha256="311a5b206cfa2f48af8084c8fb96b417e6b0034362a1319fc3757826d6042427"
target="${module_root}/testdata/execution-spec-tests"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/mpt-execution-spec-tests.XXXXXX")"

cleanup() {
    rm -rf "${temporary}"
}
trap cleanup EXIT HUP INT TERM

archive="${temporary}/fixtures_stable.tar.gz"
curl --fail --location --silent --show-error \
    --output "${archive}" \
    "https://github.com/ethereum/execution-spec-tests/releases/download/${release}/fixtures_stable.tar.gz"

actual_archive_sha256="$(shasum -a 256 "${archive}" | awk '{print $1}')"
[[ "${actual_archive_sha256}" == "${archive_sha256}" ]] || {
    printf 'archive checksum mismatch: %s\n' "${actual_archive_sha256}" >&2
    exit 1
}

fixtures=(
    "blockchain_tests/frontier/examples/test_block_intermediate_state.json"
    "blockchain_tests/berlin/eip2930_access_list/test_eip2930_tx_validity.json"
    "blockchain_tests/london/eip1559_fee_market_change/test_eip1559_tx_validity.json"
    "blockchain_tests/cancun/eip4844_blobs/test_blobhash_multiple_txs_in_block.json"
    "blockchain_tests/prague/eip7702_set_code_tx/test_eip_7702.json"
)

for fixture in "${fixtures[@]}"; do
    tar -xzf "${archive}" -C "${temporary}" "fixtures/${fixture}"
    mkdir -p "${target}/$(dirname "${fixture}")"
    install -m 0644 \
        "${temporary}/fixtures/${fixture}" \
        "${target}/${fixture}"
done

curl --fail --location --silent --show-error \
    --output "${temporary}/LICENSE" \
    "https://raw.githubusercontent.com/ethereum/execution-spec-tests/${release_commit}/LICENSE"
actual_license_sha256="$(shasum -a 256 "${temporary}/LICENSE" | awk '{print $1}')"
[[ "${actual_license_sha256}" == "${license_sha256}" ]] || {
    printf 'license checksum mismatch: %s\n' "${actual_license_sha256}" >&2
    exit 1
}
install -m 0644 "${temporary}/LICENSE" "${target}/LICENSE"

checksums=(
    "411035cccfee534648135073873e94aba2e58c2d2eb09aa41a377305b3da1c2f  blockchain_tests/frontier/examples/test_block_intermediate_state.json"
    "860c386fcb930553b7a6487e805bcfbd65d430ca5548e3d8bcddf05c62220670  blockchain_tests/berlin/eip2930_access_list/test_eip2930_tx_validity.json"
    "d12a301b9cd3aa4a1978a336e24874dbc70b4ae4d3ff1fc3e8f4944457571b91  blockchain_tests/london/eip1559_fee_market_change/test_eip1559_tx_validity.json"
    "0910707d823e54042a6f8be1dd87822856390172d1f2ff7924caf6dabfc9cf9e  blockchain_tests/cancun/eip4844_blobs/test_blobhash_multiple_txs_in_block.json"
    "7e1b668d91043606afedba88775e825f0fded12f0c4ba98d8c452a023221750e  blockchain_tests/prague/eip7702_set_code_tx/test_eip_7702.json"
)

for expected in "${checksums[@]}"; do
    checksum="${expected%%  *}"
    fixture="${expected#*  }"
    actual="$(shasum -a 256 "${target}/${fixture}" | awk '{print $1}')"
    [[ "${actual}" == "${checksum}" ]] || {
        printf 'fixture checksum mismatch for %s: %s\n' "${fixture}" "${actual}" >&2
        exit 1
    }
done

printf 'updated execution-spec-tests %s (%s) fixtures\n' \
    "${release}" "${release_commit}"

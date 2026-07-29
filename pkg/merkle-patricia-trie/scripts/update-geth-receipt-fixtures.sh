#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
revision="117e067f0f0bae1a17082321f224dedb6765b10f"
target="${module_root}/testdata/go-ethereum"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/mpt-geth-receipts.XXXXXX")"

cleanup() {
    rm -rf "${temporary}"
}
trap cleanup EXIT HUP INT TERM

files=(
    "cmd/evm/testdata/1/exp.json"
    "cmd/evm/testdata/13/exp2.json"
    "cmd/evm/testdata/28/exp.json"
    "cmd/evm/testdata/33/exp.json"
)
checksums=(
    "50296a4d39478d0e2a08db144a53a0d1c33a54fbdd5604f5f3d406e82f991758"
    "b3835420252fd3eb61676728ad83b4c35469d007c14e5e0abb2cfd117c192b58"
    "d4547eeaa055b7e3c90a1cddbfb616fc193be356034a2a239731fc74cc5c678e"
    "8aa6a6530afca88899c105908e032387c51a2fa6019f4002726320a7c97270f4"
)

for index in "${!files[@]}"; do
    file="${files[index]}"
    destination="${target}/${file}"
    mkdir -p "$(dirname "${destination}")"
    curl --fail --location --silent --show-error \
        --output "${temporary}/fixture" \
        "https://raw.githubusercontent.com/ethereum/go-ethereum/${revision}/${file}"
    actual="$(shasum -a 256 "${temporary}/fixture" | awk '{print $1}')"
    [[ "${actual}" == "${checksums[index]}" ]] || {
        printf 'fixture checksum mismatch for %s: %s\n' "${file}" "${actual}" >&2
        exit 1
    }
    install -m 0644 "${temporary}/fixture" "${destination}"
done

curl --fail --location --silent --show-error \
    --output "${temporary}/COPYING" \
    "https://raw.githubusercontent.com/ethereum/go-ethereum/${revision}/COPYING"
license_checksum="$(shasum -a 256 "${temporary}/COPYING" | awk '{print $1}')"
[[ "${license_checksum}" == \
    "3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986" ]] || {
    printf 'license checksum mismatch: %s\n' "${license_checksum}" >&2
    exit 1
}
install -m 0644 "${temporary}/COPYING" "${target}/COPYING"

printf 'updated go-ethereum receipt fixtures at %s\n' "${revision}"

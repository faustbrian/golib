#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
harness="$root/interoperability/rust-verkle"
fixture="$root/internal/backend/testdata/rust-verkle-encoding.tsv"
sources="$root/specification/sources.json"
fixture_id=rust-verkle-banderwagon-encoding-vectors

for tool in cargo diff git jq rustc shasum; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        printf '%s\n' "$tool is required for rust-verkle interoperability verification" >&2
        exit 1
    fi
done

temporary=$(mktemp -d "${TMPDIR:-/tmp}/verkle-tree-interop.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

verify_generator_file() {
    field=$1
    path=$2
    expected=$(jq -er --arg id "$fixture_id" --arg field "$field" '
        .test_fixtures[]
        | select(.id == $id)
        | .generator[$field]
    ' "$sources")
    actual=$(shasum -a 256 "$path" | awk '{print $1}')
    if [ "$actual" != "$expected" ]; then
        printf '%s\n' "$path checksum $actual does not match $expected" >&2
        exit 1
    fi
}

verify_generator_file source_sha256 "$harness/src/main.rs"
verify_generator_file manifest_sha256 "$harness/Cargo.toml"
verify_generator_file lock_sha256 "$harness/Cargo.lock"
verify_generator_file toolchain_sha256 "$harness/rust-toolchain.toml"

expected_fixture=$(jq -er --arg id "$fixture_id" '
    .test_fixtures[]
    | select(.id == $id)
    | .fixture_sha256
' "$sources")
actual_fixture=$(shasum -a 256 "$fixture" | awk '{print $1}')
if [ "$actual_fixture" != "$expected_fixture" ]; then
    printf '%s\n' "$fixture checksum $actual_fixture does not match $expected_fixture" >&2
    exit 1
fi

expected_toolchain=$(sed -n 's/^channel = "\(.*\)"$/\1/p' "$harness/rust-toolchain.toml")
actual_toolchain=$(
    cd "$harness"
    rustc --version | awk '{print $2}'
)
if [ "$actual_toolchain" != "$expected_toolchain" ]; then
    printf '%s\n' "rustc version $actual_toolchain does not match $expected_toolchain" >&2
    exit 1
fi

(
    cd "$harness"
    cargo metadata --locked --format-version 1
) >"$temporary/metadata.json"
banderwagon_manifest=$(jq -er '
    .packages[]
    | select(.name == "banderwagon")
    | .manifest_path
' "$temporary/metadata.json")
checkout_root=$(dirname "$(dirname "$banderwagon_manifest")")
expected_revision=$(jq -er '
    .sources[]
    | select(.id == "crate-crypto-rust-verkle")
    | .revision
' "$sources")
expected_tree=$(jq -er '
    .sources[]
    | select(.id == "crate-crypto-rust-verkle")
    | .tree
' "$sources")
expected_source_path=$(jq -er --arg id "$fixture_id" '
    .test_fixtures[]
    | select(.id == $id)
    | .source_path
' "$sources")
expected_source_sha256=$(jq -er --arg id "$fixture_id" '
    .test_fixtures[]
    | select(.id == $id)
    | .source_sha256
' "$sources")
actual_revision=$(git -C "$checkout_root" rev-parse HEAD)
actual_tree=$(git -C "$checkout_root" rev-parse HEAD^{tree})
if [ "$actual_revision" != "$expected_revision" ] || [ "$actual_tree" != "$expected_tree" ]; then
    printf '%s\n' \
        "rust-verkle source $actual_revision/$actual_tree does not match $expected_revision/$expected_tree" >&2
    exit 1
fi
actual_source_sha256=$(shasum -a 256 "$checkout_root/$expected_source_path" | awk '{print $1}')
if [ "$actual_source_sha256" != "$expected_source_sha256" ]; then
    printf '%s\n' \
        "$expected_source_path checksum $actual_source_sha256 does not match $expected_source_sha256" >&2
    exit 1
fi

(
    cd "$harness"
    CARGO_TARGET_DIR="$temporary/target" cargo run --locked --quiet
) >"$temporary/generated.tsv"

diff -u "$fixture" "$temporary/generated.tsv"

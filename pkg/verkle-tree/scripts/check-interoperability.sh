#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
harness="$root/interoperability/rust-verkle"
encoding_fixture="$root/internal/backend/testdata/rust-verkle-encoding.tsv"
generator_fixture="$root/internal/backend/testdata/rust-verkle-generators.tsv"
multiproof_fixture="$root/internal/backend/testdata/rust-verkle-multiproof.tsv"
sources="$root/specification/sources.json"
encoding_fixture_id=rust-verkle-banderwagon-encoding-vectors
generator_fixture_id=rust-verkle-generator-set
multiproof_fixture_id=rust-verkle-multiproof

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
    expected=$(jq -er --arg id "$encoding_fixture_id" --arg field "$field" '
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

verify_fixture() {
    id=$1
    path=$2
    expected=$(jq -er --arg id "$id" '
        .test_fixtures[]
        | select(.id == $id)
        | .fixture_sha256
    ' "$sources")
    actual=$(shasum -a 256 "$path" | awk '{print $1}')
    if [ "$actual" != "$expected" ]; then
        printf '%s\n' "$path checksum $actual does not match $expected" >&2
        exit 1
    fi
}

verify_fixture "$encoding_fixture_id" "$encoding_fixture"
verify_fixture "$generator_fixture_id" "$generator_fixture"
verify_fixture "$multiproof_fixture_id" "$multiproof_fixture"

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
actual_revision=$(git -C "$checkout_root" rev-parse HEAD)
actual_tree=$(git -C "$checkout_root" rev-parse HEAD^{tree})
if [ "$actual_revision" != "$expected_revision" ] || [ "$actual_tree" != "$expected_tree" ]; then
    printf '%s\n' \
        "rust-verkle source $actual_revision/$actual_tree does not match $expected_revision/$expected_tree" >&2
    exit 1
fi

verify_source_file() {
    id=$1
    expected_path=$(jq -er --arg id "$id" '
        .test_fixtures[]
        | select(.id == $id)
        | .source_path
    ' "$sources")
    expected_sha256=$(jq -er --arg id "$id" '
        .test_fixtures[]
        | select(.id == $id)
        | .source_sha256
    ' "$sources")
    actual_sha256=$(shasum -a 256 "$checkout_root/$expected_path" | awk '{print $1}')
    if [ "$actual_sha256" != "$expected_sha256" ]; then
        printf '%s\n' \
            "$expected_path checksum $actual_sha256 does not match $expected_sha256" >&2
        exit 1
    fi
}

verify_source_file "$encoding_fixture_id"
verify_source_file "$generator_fixture_id"

verify_source_files() {
    id=$1
    source_manifest="$temporary/source-files.tsv"
    jq -er --arg id "$id" '
        .test_fixtures[]
        | select(.id == $id)
        | .source_files[]
        | [.path, .sha256]
        | @tsv
    ' "$sources" >"$source_manifest"
    while IFS="$(printf '\t')" read -r expected_path expected_sha256; do
        actual_sha256=$(shasum -a 256 "$checkout_root/$expected_path" | awk '{print $1}')
        if [ "$actual_sha256" != "$expected_sha256" ]; then
            printf '%s\n' \
                "$expected_path checksum $actual_sha256 does not match $expected_sha256" >&2
            exit 1
        fi
    done <"$source_manifest"
}

verify_source_files "$multiproof_fixture_id"

(
    cd "$harness"
    CARGO_TARGET_DIR="$temporary/target" cargo run --locked --quiet -- encodings
) >"$temporary/generated-encodings.tsv"
diff -u "$encoding_fixture" "$temporary/generated-encodings.tsv"

(
    cd "$harness"
    CARGO_TARGET_DIR="$temporary/target" cargo run --locked --quiet -- generators
) >"$temporary/generated-generators.tsv"
diff -u "$generator_fixture" "$temporary/generated-generators.tsv"

(
    cd "$harness"
    CARGO_TARGET_DIR="$temporary/target" cargo run --locked --quiet -- multiproof
) >"$temporary/generated-multiproof.tsv"
diff -u "$multiproof_fixture" "$temporary/generated-multiproof.tsv"

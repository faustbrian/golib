#!/bin/sh
set -eu

repository=https://raw.githubusercontent.com/open-rpc/spec
commit=3a13c7a8bad248e6edd2d48339cd1c06b57f8f22
destination=specification/openrpc-1.4.1
examples_repository=https://raw.githubusercontent.com/open-rpc/examples
examples_commit=dce69463ba9a3ca2232506b734606fa97f25dd45
examples_destination=specification/examples
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

fetch() {
    source_path=$1
    output_name=$2
    expected=$3

    curl --fail --location --silent --show-error \
        "$repository/$commit/$source_path" \
        --output "$temporary/$output_name"

    actual=$(shasum -a 256 "$temporary/$output_name" | awk '{print $1}')
    if [ "$actual" != "$expected" ]; then
        echo "checksum mismatch for $source_path" >&2
        exit 1
    fi
}

fetch \
    spec/1.4/schema.json \
    schema.json \
    a8a733cd87ef6ba2e1a38e6500090581b0f52b91051a0bad8386004366b11c56
fetch \
    spec/1.4/spec-template.md \
    spec-template.md \
    70efadbae60557578d689f225f1edcb9d0f56b9b4d49c3a3e79a42ba934f46df
fetch \
    LICENSE.md \
    LICENSE.md \
    c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4
fetch \
    CHANGELOG.md \
    CHANGELOG.md \
    4549c0e79358005ae98c30dc41243352f40ccbf6c6de77e9f79f20263ec41d07

mkdir -p "$destination"
cp "$temporary/schema.json" "$destination/schema.json"
cp "$temporary/spec-template.md" "$destination/spec-template.md"
cp "$temporary/LICENSE.md" "$destination/LICENSE.md"
cp "$temporary/CHANGELOG.md" "$destination/CHANGELOG.md"

curl --fail --location --silent --show-error \
    https://meta.json-schema.tools/ \
    --output "$temporary/json-schema-tools-source.json"
actual=$(shasum -a 256 "$temporary/json-schema-tools-source.json" | awk '{print $1}')
if [ "$actual" != "993eb54713cf2d5f26d9872671ffda551c50cba4882cca2914e9cbee84936155" ]; then
    echo "checksum mismatch for https://meta.json-schema.tools/" >&2
    exit 1
fi
jq -S . "$temporary/json-schema-tools-source.json" \
    > "$temporary/json-schema-tools.json"
actual=$(shasum -a 256 "$temporary/json-schema-tools.json" | awk '{print $1}')
if [ "$actual" != "a8369ab8a9dd89ac38bf20ebd78e58310c81cabbf6ec180d208f1955c068cae4" ]; then
    echo "normalized checksum mismatch for meta.json-schema.tools" >&2
    exit 1
fi
cp "$temporary/json-schema-tools.json" "$destination/json-schema-tools.json"

repository=$examples_repository
commit=$examples_commit
fetch LICENSE.md examples-LICENSE.md \
    c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4
fetch service-descriptions/api-with-examples-openrpc.json \
    api-with-examples-openrpc.json \
    2101b99c90d6a397ecf1fce838fd713cd3292ec966cc721ef63906f001ceed79
fetch service-descriptions/empty-openrpc.json empty-openrpc.json \
    ba867abd84205b642d7ba345506c1e907c0055ceee91a575a1075d0e123df526
fetch service-descriptions/link-example-openrpc.json link-example-openrpc.json \
    52c4aba9f7334f973c807ecaaf198dfa94dc17b814b83aaa6f9313f4fcaaa031
fetch service-descriptions/metrics-openrpc.json metrics-openrpc.json \
    3e77e7f6004f2ce53cc1be8bbdc2dd67e64075e045e3458c9adec836f7ba53cb
fetch service-descriptions/params-by-name-petstore-openrpc.json \
    params-by-name-petstore-openrpc.json \
    e3de2289c700804fe8652bcfcc355bc1a876c08835a1e81c0b16bdd7e06644ff
fetch service-descriptions/petstore-expanded-openrpc.json \
    petstore-expanded-openrpc.json \
    50fb8e458e9687b8129e40c5d101cfb09990e4e7a94111da63ce9584476a8189
fetch service-descriptions/petstore-openrpc.json petstore-openrpc.json \
    22380016624581d43ea174a2682686e328b47e9e9d2dcf397535f20778fc4a9d
fetch service-descriptions/simple-math-openrpc.json simple-math-openrpc.json \
    416326e5ecd667223c501950b3cb0fe240bfea5f163959eda3511b361234778b

mkdir -p "$examples_destination"
cp "$temporary/examples-LICENSE.md" "$examples_destination/LICENSE.md"
for example in \
    api-with-examples-openrpc.json \
    empty-openrpc.json \
    link-example-openrpc.json \
    metrics-openrpc.json \
    params-by-name-petstore-openrpc.json \
    petstore-expanded-openrpc.json \
    petstore-openrpc.json \
    simple-math-openrpc.json
do
    cp "$temporary/$example" "$examples_destination/$example"
done

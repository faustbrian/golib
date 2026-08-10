#!/usr/bin/env bash
set -euo pipefail

duration=${1:-2s}
targets=(FuzzCompileDefinitions FuzzReferenceGraphs FuzzBundleLoading)
for target in "${targets[@]}"; do
	./scripts/with-gocache.sh go test . -run '^$' -fuzz "^${target}$" -fuzztime="$duration"
done
./scripts/with-gocache.sh go test ./formats/avro -run '^$' -fuzz '^FuzzAvroSchemas$' -fuzztime="$duration"
./scripts/with-gocache.sh go test ./formats/jsonschema -run '^$' -fuzz '^FuzzJSONSchemas$' -fuzztime="$duration"
./scripts/with-gocache.sh go test ./formats/protobuf -run '^$' -fuzz '^FuzzProtobufSchemas$' -fuzztime="$duration"

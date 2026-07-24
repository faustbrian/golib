#!/usr/bin/env bash
set -euo pipefail

fuzz_time="${1:-${FUZZ_TIME:-2s}}"
targets=(
	.:FuzzStructuredSources
	.:FuzzDotenvInterpolation
	.:FuzzEnvironmentMapping
	.:FuzzDecodeTagsAndDestinationTypes
	./filesystem:FuzzFilesystemBoundary
	./discover:FuzzDiscoveryBoundary
)

for entry in "${targets[@]}"; do
	package="${entry%%:*}"
	target="${entry#*:}"
	go test "$package" -run '^$' -fuzz "^${target}$" -fuzztime "$fuzz_time" -parallel=4
done

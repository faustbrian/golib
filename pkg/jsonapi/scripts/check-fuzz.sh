#!/usr/bin/env bash
set -euo pipefail

fuzz_time="${1:-${FUZZ_TIME:-2s}}"
targets=(
  ".:FuzzUnmarshal"
  ".:FuzzUnmarshalAtomic"
  ".:FuzzParseQuery"
  ".:FuzzCursorPaginationQuery"
  ".:FuzzNegotiation"
  ".:FuzzConstructedDocumentValidation"
  ".:FuzzMemberRegistry"
  ".:FuzzCursorMetadata"
  ".:FuzzMarshalUnmarshalRoundTrip"
)

for target in "${targets[@]}"; do
  package="${target%%:*}"
  name="${target#*:}"
  go test "$package" -run '^$' -fuzz "^${name}$" -fuzztime "$fuzz_time" -parallel=4
done

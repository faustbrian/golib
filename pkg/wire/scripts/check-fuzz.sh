#!/usr/bin/env bash
set -euo pipefail

fuzz_time="${1:-${FUZZ_TIME:-2s}}"
targets=(
  ".:FuzzRoundTrip"
  "./jsonwire:FuzzDecode"
  "./xmlwire:FuzzDecode"
  "./soap:FuzzParse"
  "./yamlwire:FuzzDecode"
  "./tomlwire:FuzzDecode"
  "./msgpackwire:FuzzDecode"
  "./cborwire:FuzzDecode"
  "./bsonwire:FuzzDecode"
)

for target in "${targets[@]}"; do
  package="${target%%:*}"
  name="${target#*:}"
  go test "$package" -run '^$' -fuzz "^${name}$" -fuzztime "$fuzz_time" -parallel=4
done

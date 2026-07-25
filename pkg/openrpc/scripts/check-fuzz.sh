#!/usr/bin/env bash
set -euo pipefail

fuzz_time=${1:-${FUZZ_TIME:-2s}}
targets=(
  ".:FuzzAcceptedDocumentsRoundTripDeterministically"
  "./jsonvalue:FuzzParseStrictJSON"
  "./jsonvalue:FuzzParseNeverPanics"
  "./parse:FuzzDecodeOpenRPCDocument"
  "./jsonschema:FuzzDraft7CompileAndValidateDeterministically"
  "./expression:FuzzTemplateNeverPanics"
  "./reference:FuzzReferenceAndPointerParsing"
  "./reference:FuzzResolverInternalReferences"
  "./reference:FuzzPointerNeverPanics"
  "./compose:FuzzCompositionIsDeterministicAndPanicFree"
  "./diff:FuzzSemanticDiffIsDeterministic"
  "./discovery:FuzzDiscoverySnapshotsAreDeterministic"
)

for target in "${targets[@]}"; do
    package=${target%%:*}
    name=${target#*:}
    go test "$package" -run '^$' -fuzz "^${name}$" \
        -fuzztime "$fuzz_time" -parallel=4
done

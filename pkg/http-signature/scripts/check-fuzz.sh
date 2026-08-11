#!/usr/bin/env bash
set -euo pipefail

fuzz_time="${FUZZ_TIME:-10000x}"
targets=(
    FuzzParseSignatureInputs
    FuzzParseSignatures
    FuzzParseDigestFields
    FuzzCreateSignatureBase
    FuzzParseAcceptSignatures
    FuzzParseDigestPreferences
    FuzzStrictStructuredFieldsMultiline
    FuzzExternalRequestTrailers
    FuzzRawHTTPMessageFields
)
for target in "${targets[@]}"; do
    ./scripts/with-go-cache.sh env GOWORK=off go test -mod=readonly . \
        -run '^$' -fuzz="^${target}$" -fuzztime="$fuzz_time"
done

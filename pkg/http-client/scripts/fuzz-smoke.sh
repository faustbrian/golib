#!/usr/bin/env sh

set -eu

fuzz_time="${FUZZ_TIME:-10000x}"

for target in \
	FuzzRequestSpecURL \
	FuzzHeaderValidation \
	FuzzAuthenticationInputs \
	FuzzAuthenticationChallengeHeaders \
	FuzzErrorPayloadClassification \
	FuzzRedirectCredentialBoundary \
	FuzzRetryPolicy
do
	go test -run '^$' -fuzz="^${target}$" -fuzztime="${fuzz_time}" .
done

#!/usr/bin/env bash
set -euo pipefail

duration=${1:-2s}
targets=(
	FuzzCompileAndValidate
	FuzzValidationOutput
	FuzzRawAndValueValidationAgree
	FuzzReferenceResolution
)
for target in "${targets[@]}"; do
	go test . -run '^$' -fuzz "^${target}$" -fuzztime="$duration"
done

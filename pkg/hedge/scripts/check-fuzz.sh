#!/usr/bin/env bash
set -euo pipefail

duration=${1:-2s}
targets=(
	'FuzzPolicyValidationRejectsUnsafeBounds'
	'FuzzOutstandingBudgetNeverExceedsLimit'
)
for target in "${targets[@]}"; do
	go test . -run '^$' -fuzz "^${target}$" -fuzztime="$duration" -parallel=4
done

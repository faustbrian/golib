#!/usr/bin/env bash
set -euo pipefail

duration=${1:-2s}
targets=(
	'.|FuzzDescriptorNames'
	'./requestid|FuzzInboundIdentifier'
	'./bodylimit|FuzzBodyLimit'
	'./proxy|FuzzForwardedField'
	'./cors|FuzzOriginAndPreflight'
	'./compress|FuzzAcceptEncoding'
	'./content|FuzzAcceptMediaTypes'
	'./secureheader|FuzzConfiguredHeaderValues'
)
for entry in "${targets[@]}"; do
	package=${entry%%|*}
	target=${entry#*|}
	go test "$package" -run '^$' -fuzz "^${target}$" -fuzztime="$duration"
done

#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output=${1:-"$root/specification/official-suite-results.tsv"}

cd "$root"
go run ./internal/cmd/conformance-manifest -output "$output"

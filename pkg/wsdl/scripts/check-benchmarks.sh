#!/usr/bin/env bash
set -euo pipefail

duration="${1:-100ms}"
output="${BENCHMARK_OUTPUT:-benchmark.txt}"

go test ./... -run '^$' -bench . -benchmem -benchtime="$duration" | tee "$output"
for benchmark in BenchmarkParseWSDL BenchmarkMarshalWSDL BenchmarkCompileWSDL; do
  rg -q "^${benchmark}" "$output" || {
    echo "missing benchmark result: $benchmark" >&2
    exit 1
  }
done

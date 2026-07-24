#!/usr/bin/env bash
set -euo pipefail

duration="${1:-100ms}"
output="${BENCHMARK_OUTPUT:-benchmark.txt}"
reference_iterations="${REFERENCE_BENCH_ITERATIONS:-100}"

go test ./... -run '^$' -bench . -benchmem -benchtime="$duration" | tee "$output"

required=(
  BenchmarkParseSchema
  BenchmarkCompileSchema
  BenchmarkValidateInstance
  BenchmarkMarshalSchema
)
for benchmark in "${required[@]}"; do
  rg -q "^${benchmark}" "$output" || {
    echo "missing benchmark result: $benchmark" >&2
    exit 1
  }
done

./scripts/run-java-reference.sh benchmark "$reference_iterations" |
  tee -a "$output"

for benchmark in ReferenceCompileSchemaJAXP ReferenceValidateInstanceJAXP; do
  rg -q "^${benchmark} [0-9]+ ns/op$" "$output" || {
    echo "missing reference benchmark result: $benchmark" >&2
    exit 1
  }
done

#!/usr/bin/env bash
set -euo pipefail

benchtime="${1:-100ms}"
benchstat="${BENCHSTAT:-go run golang.org/x/perf/cmd/benchstat@latest}"
result="$(mktemp)"
trap 'rm -f "${result}"' EXIT

(
  cd benchmarks
  GOWORK=off go test ./... -run '^$' -bench BenchmarkEquivalentDispatch \
    -benchmem -benchtime="${benchtime}" -count=5
) | tee "${result}"

go_ns="$(awk '/BenchmarkEquivalentDispatch\/cli-/ {sum += $3; n++} END {print sum / n}' "${result}")"
cobra_ns="$(awk '/BenchmarkEquivalentDispatch\/cobra-/ {sum += $3; n++} END {print sum / n}' "${result}")"
go_allocs="$(awk '/BenchmarkEquivalentDispatch\/cli-/ {sum += $7; n++} END {print sum / n}' "${result}")"

awk -v actual="${go_ns}" -v baseline="${cobra_ns}" \
  'BEGIN { if (actual > baseline * 4) exit 1 }' || {
  echo "cli dispatch exceeds the 4x prepared Cobra latency budget" >&2
  exit 1
}

awk -v actual="${go_allocs}" 'BEGIN { if (actual > 100) exit 1 }' || {
  echo "cli dispatch exceeds the 100 allocation budget" >&2
  exit 1
}

# Keep the canonical statistical report available in CI logs.
${benchstat} "${result}"

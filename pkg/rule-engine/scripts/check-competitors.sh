#!/usr/bin/env bash
set -euo pipefail

duration="${1:-100ms}"
(
  cd benchmarks/competitors
  test -z "$(gofmt -l .)"
  go vet ./...
  go test ./... -count=1
  go test ./... -run '^$' -bench '^BenchmarkEquivalentDecision$' \
    -benchmem -benchtime="$duration"
)

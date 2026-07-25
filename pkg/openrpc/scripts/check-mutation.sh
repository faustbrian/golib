#!/usr/bin/env bash
set -euo pipefail

: "${GREMLINS_VERSION:?GREMLINS_VERSION is required}"

packages=(
  ./builder ./compose ./diff ./discovery ./expression ./jsonrpc
  ./jsonschema ./jsonvalue ./observe ./parse ./reference
  ./reference/httpstore ./validate ./internal/specification
  ./internal/specification/cmd/specmatrix .
)

for package in "${packages[@]}"; do
    echo "mutation package: $package"
    report=$(mktemp)
    exclude='.*testdata.*'
    if [[ "$package" == "./internal/specification" ]]; then
        exclude='cmd/.*|.*testdata.*'
    fi
    if [[ "$package" == "." ]]; then
        exclude='.*/.*|.*testdata.*'
    fi
    go run "github.com/go-gremlins/gremlins/cmd/gremlins@${GREMLINS_VERSION}" \
        unleash "$package" --workers 2 --timeout-coefficient 30 \
        --threshold-efficacy 100 --threshold-mcover 100 \
        --exclude-files "$exclude" | tee "$report"
    if grep -q '^No results to report\.$' "$report"; then
        rm -f "$report"
        continue
    fi
    efficacy=$(awk '/Test efficacy:/ {gsub(/%/, "", $3); print $3}' "$report")
    coverage=$(awk '/Mutator coverage:/ {gsub(/%/, "", $3); print $3}' "$report")
    rm -f "$report"
    if [[ "$efficacy" != "100.00" || "$coverage" != "100.00" ]]; then
        echo "mutation thresholds failed for $package: efficacy ${efficacy}%, mutator coverage ${coverage}%" >&2
        exit 1
    fi
done

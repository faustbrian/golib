#!/usr/bin/env bash
set -euo pipefail

version="v0.6.0"
go run "github.com/go-gremlins/gremlins/cmd/gremlins@${version}" unleash . \
  --integration --coverpkg ./... --workers 2 --timeout-coefficient 10 \
  --exclude-files '^adapters/' \
  --threshold-mcover 100 --threshold-efficacy 65 \
  --output mutation-results.json

for module in gomath gotemporal gomeasurement; do
  (
    cd "adapters/${module}"
    go run "github.com/go-gremlins/gremlins/cmd/gremlins@${version}" unleash . \
      --integration --coverpkg ./... --workers 1 --timeout-coefficient 10 \
      --threshold-mcover 100 --threshold-efficacy 65 \
      --output "../../mutation-results-${module}.json"
  )
done

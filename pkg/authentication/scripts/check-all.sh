#!/bin/sh
set -eu

./scripts/check-format.sh
./scripts/check-boundaries.sh
for module in . jwt oidc authotel; do
	(cd "$module" && go vet ./... && go test ./... && go test -race ./...)
done
./scripts/check-coverage.sh
./scripts/check-examples.sh
./scripts/check-benchmarks.sh
./scripts/check-api.sh

#!/usr/bin/env bash
set -euo pipefail

directory="$(mktemp -d)"
trap 'rm -rf "$directory"' EXIT
go run ./cmd/international-generate \
  -country-output "$directory/country.go" \
  -currency-output "$directory/currency.go" \
  -subdivision-output "$directory/subdivision.go"
diff -u country/data_generated.go "$directory/country.go"
diff -u currency/data_generated.go "$directory/currency.go"
diff -u subdivision/data_generated.go "$directory/subdivision.go"
go run ./cmd/international-dataset-review -snapshot "$directory/dataset-snapshot.json"
diff -u data/dataset-snapshot.json "$directory/dataset-snapshot.json"

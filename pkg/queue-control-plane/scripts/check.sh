#!/usr/bin/env bash
set -euo pipefail

scripts/check-format.sh
scripts/check-docs.sh
go mod tidy -diff
go mod verify
go vet ./...
scripts/staticcheck.sh
scripts/lint.sh
go test ./...
go test -race ./...
scripts/coverage.sh
go build ./...

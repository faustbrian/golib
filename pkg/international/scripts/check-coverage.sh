#!/usr/bin/env bash
set -euo pipefail

mkdir -p build
coverpkg="$(go list ./... | grep -v '/cmd/' | paste -sd, -)"
go test -coverpkg="$coverpkg" -coverprofile=build/all.cover ./...
{
  head -n 1 build/all.cover
  tail -n +2 build/all.cover | grep -v '/cmd/'
} > build/coverage.out
actual="$(go tool cover -func=build/coverage.out | awk '/^total:/ {print $3}')"
test "$actual" = "100.0%" || {
  echo "production statement coverage is $actual, want 100.0%" >&2
  exit 1
}

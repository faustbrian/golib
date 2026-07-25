#!/usr/bin/env bash
set -euo pipefail

if grep -R -n -E 'float32|float64' --include='*.go' \
  --exclude='*_test.go' --exclude-dir=internal .; then
  echo 'production monetary APIs must not use binary floating point' >&2
  exit 1
fi

if grep -R -n -E 'shopspring/decimal|cockroachdb/apd|ericlagergren/decimal' \
  --include='*.go' --exclude='*_test.go' .; then
  echo 'a competing decimal implementation was found outside math' >&2
  exit 1
fi

#!/usr/bin/env bash
set -euo pipefail

version="v0.6.0"
go run "github.com/go-gremlins/gremlins/cmd/gremlins@${version}" unleash . \
  --integration --workers 2 --timeout-coefficient 10 \
  --arithmetic-base=false \
  --exclude-files '^internal/cmd/' \
  --exclude-files '^unicode_tables_generated\.go$' \
  --threshold-mcover 100 --threshold-efficacy 65 \
  --output mutation-results.json

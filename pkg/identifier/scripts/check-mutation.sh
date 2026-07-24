#!/usr/bin/env bash
set -euo pipefail

version="v0.6.0"
packages=(. ./uuid ./ulid ./typeid ./ksuid ./nanoid)
for package in "${packages[@]}"; do
  echo "mutation scope: ${package}"
  if [[ "$package" == "." ]]; then
    go run "github.com/go-gremlins/gremlins/cmd/gremlins@${version}" unleash \
      "$package" --integration --coverpkg "$package" --workers 2 \
      --timeout-coefficient 10 --threshold-mcover 100 \
      --threshold-efficacy 100 --output-statuses lct \
      --exclude-files '^(idtest|uuid|ulid|typeid|ksuid|nanoid)/'
    continue
  fi
  go run "github.com/go-gremlins/gremlins/cmd/gremlins@${version}" unleash \
    "$package" --coverpkg "$package" --workers 2 \
    --timeout-coefficient 10 --threshold-mcover 100 \
    --threshold-efficacy 100 --output-statuses lct
done

#!/usr/bin/env bash
set -euo pipefail

go test -run '^Example' ./...
go test ./examples/...
go run scripts/doccheck.go

while IFS= read -r package; do
    go doc "${package}" >/dev/null
done < <(go list -f '{{.ImportPath}}' ./...)

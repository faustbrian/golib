#!/usr/bin/env bash
set -euo pipefail

test "$(go env GOVERSION)" = "go1.26.5"
test -z "$(go list -m -f '{{if .Replace}}{{.Path}}{{end}}' all)"
go mod verify
git diff --exit-code -- go.mod go.sum

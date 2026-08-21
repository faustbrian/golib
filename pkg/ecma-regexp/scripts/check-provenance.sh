#!/usr/bin/env bash
set -euo pipefail

test "$(go env GOVERSION)" = "go1.26.6"
test -z "$(go list -m -f '{{if .Replace}}{{.Path}}{{end}}' all)"
go mod verify
git diff --exit-code -- go.mod go.sum unicode_tables_generated.go

test262_root="${TEST262_ROOT:?TEST262_ROOT is required}"
test "$(git -C "$test262_root" rev-parse HEAD)" = \
  "26058a01fdbc8dad9ded0e97133190098ea8c5d8"
test "$(shasum -a 256 "$test262_root/LICENSE" | awk '{print $1}')" = \
  "4dd9244dfe8197c75348c4b24ab53d29d3b1cfad143ac76b5a3d8942aa354ce0"

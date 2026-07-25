#!/bin/sh
set -eu

command -v golangci-lint >/dev/null 2>&1 || {
    printf '%s\n' 'golangci-lint is missing; run make tools' >&2
    exit 1
}
golangci-lint run ./...

#!/bin/sh
set -eu

command -v go-licenses >/dev/null 2>&1 || {
    printf '%s\n' 'go-licenses is missing; run make tools' >&2
    exit 1
}
go-licenses check ./...

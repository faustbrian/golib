#!/bin/sh
set -eu

command -v staticcheck >/dev/null 2>&1 || {
    printf '%s\n' 'staticcheck is missing; run make tools' >&2
    exit 1
}
staticcheck ./...

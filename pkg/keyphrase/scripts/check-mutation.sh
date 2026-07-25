#!/bin/sh
set -eu

command -v gremlins >/dev/null 2>&1 || {
    printf '%s\n' 'gremlins is missing; run make tools' >&2
    exit 1
}
gremlins unleash

#!/usr/bin/env bash
set -euo pipefail

cache="$(mktemp -d "${TMPDIR:-/tmp}/golib-audit-gocache.XXXXXX")"
cleanup() {
    case "${cache}" in
        "${TMPDIR:-/tmp}"/golib-audit-gocache.*)
            find "${cache}" -depth -delete
            ;;
    esac
}
trap cleanup EXIT HUP INT TERM

GOCACHE="${cache}" "$@"

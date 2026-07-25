#!/usr/bin/env bash
set -euo pipefail

files=$(gofmt -l .)
if [[ -n "$files" ]]; then
	printf 'Go files require gofmt:\n%s\n' "$files" >&2
	exit 1
fi

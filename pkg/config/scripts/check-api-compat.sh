#!/usr/bin/env bash
set -euo pipefail

baseline=api/stable.txt
current=$(mktemp)
trap 'rm -f "$current"' EXIT
apidiff=golang.org/x/exp/cmd/apidiff@v0.0.0-20260709172345-9ea1abe57597

go run "$apidiff" -m -w "$current" \
	github.com/faustbrian/golib/pkg/config

if [[ ! -s "$baseline" ]]; then
	echo "API baseline is missing: $baseline" >&2
	exit 1
fi

go run "$apidiff" -m -incompatible "$baseline" "$current"

#!/bin/sh
set -eu

output=$(mktemp -d "${TMPDIR:-/tmp}/password-386.XXXXXX")
trap 'rm -rf "$output"' EXIT HUP INT TERM
GOOS=linux GOARCH=386 go test -c -o "$output" ./...

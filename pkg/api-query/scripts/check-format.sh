#!/bin/sh
set -eu

files=$(find . -type f -name '*.go' -not -path './.git/*' -print)
unformatted=$(gofmt -l $files)
if [ -n "$unformatted" ]; then
	printf '%s\n' "$unformatted"
	exit 1
fi

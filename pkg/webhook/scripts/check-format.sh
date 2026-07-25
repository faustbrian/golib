#!/bin/sh
set -eu

files=$(find . -name '*.go' -not -path './.git/*' -print)
test -n "$files"
unformatted=$(gofmt -l $files)
if test -n "$unformatted"; then
    echo "gofmt required:" >&2
    echo "$unformatted" >&2
    exit 1
fi

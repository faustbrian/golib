#!/bin/sh
set -eu

go list ./... | while IFS= read -r package; do
    go doc "$package" >/dev/null
done
go list -json ./... >/dev/null

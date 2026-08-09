#!/bin/sh
set -eu

root=$(git rev-parse --show-toplevel)
consumer=$(mktemp -d "${TMPDIR:-/tmp}/search-consumer.XXXXXX")
trap 'rm -rf "$consumer"' EXIT HUP INT TERM
cd "$consumer"
go mod init example.com/search-consumer >/dev/null
go mod edit -replace github.com/faustbrian/golib/pkg/search="$root/pkg/search"
go get github.com/faustbrian/golib/pkg/search@v0.0.0 >/dev/null
printf '%s\n' 'package consumer' 'import "github.com/faustbrian/golib/pkg/search"' 'var _ = search.DefaultLimits' > consumer.go
GOWORK=off go test ./...

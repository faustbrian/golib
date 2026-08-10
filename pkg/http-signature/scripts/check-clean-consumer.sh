#!/usr/bin/env bash
set -euo pipefail

consumer="$(mktemp -d)"
cleanup() {
    chmod -R u+w "$consumer" 2>/dev/null || true
    find "$consumer" -mindepth 1 -delete
    rmdir "$consumer"
}
trap cleanup EXIT

module_root="$(pwd)"
cd "$consumer"
go mod init clean-consumer.example/http-signature
go mod edit -require github.com/faustbrian/golib/pkg/http-signature@v0.0.0
go mod edit -replace github.com/faustbrian/golib/pkg/http-signature="$module_root"
printf '%s\n' 'package consumer' 'import _ "github.com/faustbrian/golib/pkg/http-signature"' > consumer.go
"$module_root/scripts/with-go-cache.sh" env GOWORK=off go test -mod=mod ./...

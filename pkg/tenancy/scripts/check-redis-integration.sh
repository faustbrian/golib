#!/usr/bin/env bash
set -euo pipefail

: "${REDIS_ADDR:?REDIS_ADDR is required}"

module_directory="$(cd "$(dirname "$0")/.." && pwd)"
repository_root="$(git -C "${module_directory}" rev-parse --show-toplevel)"
consumer="$(mktemp -d "${TMPDIR:-/tmp}/tenancy-redis.XXXXXX")"
cleanup() {
    find "${consumer}" -depth -delete
}
trap cleanup EXIT HUP INT TERM

cd "${consumer}"
GOWORK=off go mod init example.com/tenancy-redis
GOWORK=off go mod edit \
    -require=github.com/faustbrian/golib/pkg/queue@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/tenancy@v0.0.0 \
    -replace="github.com/faustbrian/golib/pkg/queue=${repository_root}/pkg/queue" \
    -replace="github.com/faustbrian/golib/pkg/tenancy=${module_directory}"
cp "${module_directory}/scripts/redis/consumer_test.go.tmpl" consumer_test.go
GOWORK=off go mod tidy
GOWORK=off go test -race -count=1 ./...

#!/usr/bin/env bash
set -euo pipefail

: "${POSTGRES_URL:?POSTGRES_URL is required}"

module_directory="$(cd "$(dirname "$0")/.." && pwd)"
repository_root="$(git -C "${module_directory}" rev-parse --show-toplevel)"
consumer="$(mktemp -d "${TMPDIR:-/tmp}/tenancy-postgres-consumer.XXXXXX")"
cleanup() {
    find "${consumer}" -depth -delete
}
trap cleanup EXIT HUP INT TERM

cd "${consumer}"
GOWORK=off go mod init example.com/tenancy-postgres-consumer
GOWORK=off go mod edit \
    -require=github.com/faustbrian/golib/pkg/audit@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/audit/postgres@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/tenancy@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/workflow@v0.0.0 \
    -require=github.com/jackc/pgx/v5@v5.10.0 \
    -replace="github.com/faustbrian/golib/pkg/audit=${repository_root}/pkg/audit" \
    -replace="github.com/faustbrian/golib/pkg/audit/postgres=${repository_root}/pkg/audit/postgres" \
    -replace="github.com/faustbrian/golib/pkg/postgres=${repository_root}/pkg/postgres" \
    -replace="github.com/faustbrian/golib/pkg/tenancy=${module_directory}" \
    -replace="github.com/faustbrian/golib/pkg/workflow=${repository_root}/pkg/workflow"
cp "${module_directory}/scripts/postgres/consumer_test.go.tmpl" consumer_test.go
GOWORK=off go mod tidy
GOWORK=off go test -race -count=1 ./...

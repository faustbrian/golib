#!/usr/bin/env bash
set -euo pipefail

module_directory="$(cd "$(dirname "$0")/.." && pwd)"
consumer="$(mktemp -d "${TMPDIR:-/tmp}/tenancy-consumer.XXXXXX")"
cleanup() {
    chmod -R u+w "${consumer}" 2>/dev/null || true
    rm -rf "${consumer}"
}
trap cleanup EXIT HUP INT TERM

cd "${consumer}"
GOWORK=off go mod init example.com/tenancy-consumer
GOWORK=off go mod edit \
    -require=github.com/faustbrian/golib/pkg/tenancy@v0.0.0 \
    -replace="github.com/faustbrian/golib/pkg/tenancy=${module_directory}"
mkdir consumer
printf '%s\n' 'package consumer' \
    'import (' \
    '  "context"' \
    '  "github.com/faustbrian/golib/pkg/tenancy"' \
    '  tenancyhttp "github.com/faustbrian/golib/pkg/tenancy/http"' \
    '  tenancyjsonrpc "github.com/faustbrian/golib/pkg/tenancy/jsonrpc"' \
    '  tenancypostgres "github.com/faustbrian/golib/pkg/tenancy/postgres"' \
    ')' \
    'var _ = context.Background' \
    'var _ = tenancy.ParseTenantID' \
    'var _ = tenancyhttp.New' \
    'var _ = tenancyjsonrpc.New' \
    'var _ = tenancypostgres.NewManager' > consumer/consumer.go
GOWORK=off go mod tidy
GOWORK=off go test ./...

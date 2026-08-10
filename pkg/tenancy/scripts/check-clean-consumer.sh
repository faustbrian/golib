#!/usr/bin/env bash
set -euo pipefail

module_directory="$(cd "$(dirname "$0")/.." && pwd)"
repository_root="$(git -C "${module_directory}" rev-parse --show-toplevel)"
consumer="$(mktemp -d "${TMPDIR:-/tmp}/tenancy-consumer.XXXXXX")"
cleanup() {
    chmod -R u+w "${consumer}" 2>/dev/null || true
    rm -rf "${consumer}"
}
trap cleanup EXIT HUP INT TERM

cd "${consumer}"
GOWORK=off go mod init example.com/tenancy-consumer
GOWORK=off go mod edit \
    -require=github.com/faustbrian/golib/pkg/audit@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/cache@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/cloudevents/adapters/golib@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/queue@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/search@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/telemetry@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/tenancy@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/workflow@v0.0.0 \
    -require=go.opentelemetry.io/otel/sdk/metric@v1.44.0 \
    -replace="github.com/faustbrian/golib/pkg/audit=${repository_root}/pkg/audit" \
    -replace="github.com/faustbrian/golib/pkg/cache=${repository_root}/pkg/cache" \
    -replace="github.com/faustbrian/golib/pkg/cloudevents=${repository_root}/pkg/cloudevents" \
    -replace="github.com/faustbrian/golib/pkg/cloudevents/adapters/golib=${repository_root}/pkg/cloudevents/adapters/golib" \
    -replace="github.com/faustbrian/golib/pkg/correlation=${repository_root}/pkg/correlation" \
    -replace="github.com/faustbrian/golib/pkg/event-sourcing=${repository_root}/pkg/event-sourcing" \
    -replace="github.com/faustbrian/golib/pkg/identifier=${repository_root}/pkg/identifier" \
    -replace="github.com/faustbrian/golib/pkg/json-schema=${repository_root}/pkg/json-schema" \
    -replace="github.com/faustbrian/golib/pkg/kafka=${repository_root}/pkg/kafka" \
    -replace="github.com/faustbrian/golib/pkg/outbox=${repository_root}/pkg/outbox" \
    -replace="github.com/faustbrian/golib/pkg/queue=${repository_root}/pkg/queue" \
    -replace="github.com/faustbrian/golib/pkg/schema-registry=${repository_root}/pkg/schema-registry" \
    -replace="github.com/faustbrian/golib/pkg/search=${repository_root}/pkg/search" \
    -replace="github.com/faustbrian/golib/pkg/telemetry=${repository_root}/pkg/telemetry" \
    -replace="github.com/faustbrian/golib/pkg/tenancy=${module_directory}" \
    -replace="github.com/faustbrian/golib/pkg/workflow=${repository_root}/pkg/workflow"
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
cp "${module_directory}/scripts/clean-consumer/consumer_test.go.tmpl" consumer/consumer_test.go
cp "${module_directory}/scripts/clean-consumer/providers_test.go.tmpl" consumer/providers_test.go
cp "${module_directory}/scripts/clean-consumer/administration_test.go.tmpl" consumer/administration_test.go
GOWORK=off go mod tidy
GOWORK=off go test -race ./...

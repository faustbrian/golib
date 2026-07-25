#!/usr/bin/env bash
set -euo pipefail

export POSTGRES_TEST_IMAGE="${POSTGRES_TEST_IMAGE:-postgres:18-alpine}"
export QUEUE_CONTROL_SHARED_INTEGRATION_DATABASE=true

container=""
retention_policy="$(mktemp)"
cleanup() {
    rm -f "${retention_policy}"
    if [[ -n "${container}" ]]; then
        docker rm --force "${container}" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
    container="queue-control-plane-postgres-${PPID}-$$"
    docker run \
        --detach \
        --rm \
        --name "${container}" \
        --env POSTGRES_DB=control_plane_test \
        --env POSTGRES_USER=control_plane_test \
        --env POSTGRES_PASSWORD=control_plane_test \
        --publish 127.0.0.1::5432 \
        "${POSTGRES_TEST_IMAGE}" >/dev/null
    for _ in {1..60}; do
        if docker exec "${container}" pg_isready \
            --username control_plane_test \
            --dbname control_plane_test >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done
    if ! docker exec "${container}" pg_isready \
        --username control_plane_test \
        --dbname control_plane_test >/dev/null 2>&1; then
        printf 'PostgreSQL test container did not become ready\n' >&2
        exit 1
    fi
    port="$(docker port "${container}" 5432/tcp | awk -F: 'NR == 1 { print $NF }')"
    export TEST_DATABASE_URL="postgres://control_plane_test:control_plane_test@127.0.0.1:${port}/control_plane_test?sslmode=disable"
fi

go test \
    -race \
    -tags=integration \
    -count=1 \
    ./postgres \
    -run '^TestPostgresRuntimeIntegration$'

go test \
    -race \
    -tags=integration \
    -count=1 \
    ./postgres \
    -run '^TestPostgresProcessDeathRollbackIntegration$'

printf '%s\n' \
    '{"tenants":[{"id":"tenant-retention","retention_seconds":3600,"batch_size":1,"max_batches":20,"legal_hold":false},{"id":"tenant-command-retention","retention_seconds":3600,"batch_size":1,"max_batches":20,"legal_hold":false}]}' \
    >"${retention_policy}"
DATABASE_URL="${TEST_DATABASE_URL}" \
QUEUE_CONTROL_RETENTION_ONLY=true \
QUEUE_CONTROL_RETENTION_FILE="${retention_policy}" \
    go run ./cmd/queue-control-plane

QUEUE_CONTROL_EXPECT_RETENTION_RESULT=true go test \
    -race \
    -tags=integration \
    -count=1 \
    ./postgres \
    -run '^TestPostgresRetentionJobResultIntegration$'

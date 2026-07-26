#!/usr/bin/env bash
set -euo pipefail

image="${POSTGRES_TEST_IMAGE:-postgres:18-alpine}"
source_container="goqcp-dr-source-${PPID}-$$"
restored_container="goqcp-dr-restored-${PPID}-$$"
artifact_dir="$(mktemp -d)"

cleanup() {
    docker rm --force "${source_container}" "${restored_container}" \
        >/dev/null 2>&1 || true
    rm -rf "${artifact_dir}"
}
trap cleanup EXIT

start_database() {
    local container="$1"
    docker run \
        --detach \
        --rm \
        --name "${container}" \
        --env POSTGRES_DB=control_plane_test \
        --env POSTGRES_USER=control_plane_test \
        --env POSTGRES_PASSWORD=control_plane_test \
        --publish 127.0.0.1::5432 \
        "${image}" >/dev/null

    for _ in {1..60}; do
        if docker exec "${container}" pg_isready \
            --username control_plane_test \
            --dbname control_plane_test >/dev/null 2>&1; then
            return
        fi
        sleep 1
    done

    printf 'PostgreSQL disaster-recovery container did not become ready\n' >&2
    exit 1
}

database_url() {
    local container="$1"
    local port
    port="$(docker port "${container}" 5432/tcp | awk -F: 'NR == 1 { print $NF }')"
    printf 'postgres://control_plane_test:control_plane_test@127.0.0.1:%s/control_plane_test?sslmode=disable' \
        "${port}"
}

start_database "${source_container}"
start_database "${restored_container}"

TEST_DATABASE_URL="$(database_url "${source_container}")" \
    go test \
        -race \
        -tags=integration \
        -count=1 \
        ./postgres \
        -run '^TestPostgresRuntimeIntegration$'

docker exec "${source_container}" pg_dump \
    --username control_plane_test \
    --dbname control_plane_test \
    --format custom \
    --file /tmp/control-plane.dump
docker cp \
    "${source_container}:/tmp/control-plane.dump" \
    "${artifact_dir}/control-plane.dump" >/dev/null
docker cp \
    "${artifact_dir}/control-plane.dump" \
    "${restored_container}:/tmp/control-plane.dump" >/dev/null
docker exec "${restored_container}" pg_restore \
    --exit-on-error \
    --no-owner \
    --no-privileges \
    --username control_plane_test \
    --dbname control_plane_test \
    /tmp/control-plane.dump

RESTORED_DATABASE_URL="$(database_url "${restored_container}")" \
    go test \
        -race \
        -tags=disasterrecovery \
        -count=1 \
        ./postgres \
        -run '^TestPostgresDisasterRecoveryIntegration$'

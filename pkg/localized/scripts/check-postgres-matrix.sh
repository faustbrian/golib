#!/usr/bin/env bash
set -euo pipefail

versions=(14 15 16 17 18)
container=''

cleanup() {
    if [[ -n "$container" ]]; then
        docker rm -f "$container" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

for version in "${versions[@]}"; do
    container="localized-postgres-${version}-$$"
    docker run --rm --detach --name "$container" \
        --env POSTGRES_PASSWORD=postgres \
        --env POSTGRES_DB=localized \
        --publish 127.0.0.1::5432 \
        "postgres:${version}" >/dev/null

    port="$(docker port "$container" 5432/tcp | awk -F: 'NR == 1 {print $NF}')"
    ready=false
    for _ in {1..30}; do
        if pg_isready --host 127.0.0.1 --port "$port" \
            --username postgres --dbname localized >/dev/null 2>&1; then
            ready=true
            break
        fi
        sleep 1
    done
    if [[ "$ready" != true ]]; then
        echo "PostgreSQL ${version} did not become ready" >&2
        docker logs "$container" >&2
        exit 1
    fi

    url="postgres://postgres:postgres@127.0.0.1:${port}/localized?sslmode=disable"
    POSTGRES_URL="$url" go test -count=1 -tags=integration ./postgres
    docker rm -f "$container" >/dev/null
    container=''
done

echo 'PostgreSQL 14-18 JSONB matrix passed'

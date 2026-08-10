#!/usr/bin/env bash
set -euo pipefail

module_directory="$(cd "$(dirname "$0")/.." && pwd)"
suffix="$$"
network="golib-tenancy-postgres-failover-${suffix}"
primary="golib-tenancy-postgres-primary-${suffix}"
secondary="golib-tenancy-postgres-secondary-${suffix}"
image="postgres:18.4-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15"
secondary_data="/tmp/tenancy-postgres-replica"

cleanup() {
    docker rm -f "${primary}" "${secondary}" >/dev/null 2>&1 || true
    docker network rm "${network}" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

docker network create "${network}" >/dev/null
docker run -d --name "${primary}" --network "${network}" \
    -p 127.0.0.1::5432 -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=tenancy \
    --cpus=1 --memory=512m --pids-limit=256 --ulimit nofile=1024:1024 \
    "${image}" >/dev/null
for _ in $(seq 1 60); do
    docker exec "${primary}" pg_isready -h 127.0.0.1 -U postgres -d tenancy >/dev/null 2>&1 && break
    sleep 1
done
docker exec "${primary}" pg_isready -h 127.0.0.1 -U postgres -d tenancy >/dev/null
docker exec "${primary}" psql -U postgres -d tenancy -v ON_ERROR_STOP=1 \
    -c "CREATE ROLE tenancy_replica WITH REPLICATION LOGIN PASSWORD 'tenancy-replica-password'" \
    >/dev/null
docker exec "${primary}" sh -c \
    'printf "%s\n" "host replication tenancy_replica samenet scram-sha-256" >> "$PGDATA/pg_hba.conf"'
docker exec "${primary}" psql -U postgres -d tenancy -v ON_ERROR_STOP=1 \
    -c 'SELECT pg_reload_conf()' >/dev/null

docker run -d --name "${secondary}" --network "${network}" \
    -p 127.0.0.1::5432 -e PGDATA="${secondary_data}" --user postgres \
    --entrypoint sh --cpus=1 --memory=512m --pids-limit=256 --ulimit nofile=1024:1024 \
    "${image}" -c '
set -eu
mkdir -p "$PGDATA"
printf "%s\n" "golib-tenancy-postgres-primary-'"${suffix}"':5432:*:tenancy_replica:tenancy-replica-password" > /tmp/tenancy-replica.pgpass
chmod 600 /tmp/tenancy-replica.pgpass
pg_basebackup --dbname="host=golib-tenancy-postgres-primary-'"${suffix}"' port=5432 user=tenancy_replica passfile=/tmp/tenancy-replica.pgpass sslmode=disable" --pgdata="$PGDATA" --format=plain --wal-method=stream --write-recovery-conf
chmod 700 "$PGDATA"
exec postgres -D "$PGDATA"' >/dev/null
for _ in $(seq 1 60); do
    docker exec "${secondary}" pg_isready -h 127.0.0.1 -U postgres -d tenancy >/dev/null 2>&1 && break
    sleep 1
done
docker exec "${secondary}" pg_isready -h 127.0.0.1 -U postgres -d tenancy >/dev/null

primary_port="$(docker port "${primary}" 5432/tcp | sed -n 's/.*://p')"
secondary_port="$(docker port "${secondary}" 5432/tcp | sed -n 's/.*://p')"
POSTGRES_FAILOVER_PRIMARY_URL="postgres://postgres:postgres@127.0.0.1:${primary_port}/tenancy?sslmode=disable" \
POSTGRES_FAILOVER_SECONDARY_URL="postgres://postgres:postgres@127.0.0.1:${secondary_port}/tenancy?sslmode=disable" \
POSTGRES_FAILOVER_PRIMARY_CONTAINER="${primary}" \
POSTGRES_FAILOVER_SECONDARY_CONTAINER="${secondary}" \
POSTGRES_FAILOVER_SECONDARY_DATA="${secondary_data}" \
    GOWORK=off go test -tags=integration -race ./postgres \
        -run '^TestPostgreSQLProxyFailoverIsolation$' -count=1 -timeout=3m

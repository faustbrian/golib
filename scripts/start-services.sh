#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
    printf 'usage: %s <module-directory> <environment-file> <state-file>\n' "$0" >&2
    exit 2
fi

root="$(git rev-parse --show-toplevel)"
module="$1"
environment_file="$2"
state_file="$3"
slug="$(printf '%s' "${module}" | tr '/.' '--')-${RANDOM}"

# shellcheck source=/dev/null
source "${root}/.golib/versions.env"
: >"${environment_file}"
: >"${state_file}"

services="$(jq -r --arg directory "${module}" \
    '.modules[] | select(.directory == $directory) | .required_services[]' \
    "${root}/modules.json")"
[[ -n "${services}" ]] || exit 0
command -v docker >/dev/null || {
    printf 'Docker is required by %s for: %s\n' "${module}" "${services}" >&2
    exit 1
}

record() {
    printf '%s\n' "$1" >>"${state_file}"
}

wait_for() {
    local container="$1"
    shift
    for _ in {1..90}; do
        if docker exec "${container}" "$@" >/dev/null 2>&1; then
            return 0
        fi
        if [[ "$(docker inspect --format '{{.State.Running}}' "${container}" 2>/dev/null || true)" != "true" ]]; then
            break
        fi
        sleep 1
    done
    docker logs "${container}" >&2 || true
    printf 'service did not become healthy: %s\n' "${container}" >&2
    exit 1
}

published_port() {
    docker port "$1" "$2/tcp" | tail -1 | sed 's/.*://'
}

while IFS= read -r service; do
    case "${service}" in
        postgresql)
            container="golib-postgres-${slug}"
            postgres_version="${POSTGRES_IMAGE#postgres:}"
            postgres_version="${postgres_version%-alpine}"
            docker run --detach --name "${container}" -p 127.0.0.1::5432 \
                -e POSTGRES_USER=golib -e POSTGRES_PASSWORD=golib \
                -e POSTGRES_DB=golib "${POSTGRES_IMAGE}" >/dev/null
            record "${container}"
            wait_for "${container}" pg_isready -U golib -d golib
            port="$(published_port "${container}" 5432)"
            cat >>"${environment_file}" <<EOF
POSTGRES_URL=postgres://golib:golib@127.0.0.1:${port}/golib?sslmode=disable
POSTGRES_VERSION=${postgres_version}
OUTBOX_POSTGRES_VERSION=${postgres_version}
STATE_MACHINE_POSTGRES_VERSION=${postgres_version}
FEATURE_FLAGS_POSTGRES_DSN=postgres://golib:golib@127.0.0.1:${port}/golib?sslmode=disable
TEST_DATABASE_URL=postgres://golib:golib@127.0.0.1:${port}/golib?sslmode=disable
DATABASE_URL=postgres://golib:golib@127.0.0.1:${port}/golib?sslmode=disable
TEMPORAL_POSTGRES_DSN=postgres://golib:golib@127.0.0.1:${port}/golib?sslmode=disable
EOF
            ;;
        valkey)
            container="golib-valkey-${slug}"
            docker run --detach --name "${container}" -p 127.0.0.1::6379 \
                "${VALKEY_IMAGE}" >/dev/null
            record "${container}"
            wait_for "${container}" valkey-cli ping
            port="$(published_port "${container}" 6379)"
            cat >>"${environment_file}" <<EOF
VALKEY_ADDR=127.0.0.1:${port}
VALKEY_ADDRESS=127.0.0.1:${port}
FEATURE_FLAGS_VALKEY_ADDRESS=127.0.0.1:${port}
TEST_VALKEY_ADDRESS=127.0.0.1:${port}
CACHE_VALKEY_IMAGE=${VALKEY_IMAGE}
EOF
            ;;
        redis)
            container="golib-redis-${slug}"
            docker run --detach --name "${container}" -p 127.0.0.1::6379 \
                "${REDIS_IMAGE}" >/dev/null
            record "${container}"
            wait_for "${container}" redis-cli ping
            port="$(published_port "${container}" 6379)"
            cat >>"${environment_file}" <<EOF
REDIS_ADDR=127.0.0.1:${port}
TEST_REDIS_ADDRESS=127.0.0.1:${port}
CACHE_REDIS_IMAGE=${REDIS_IMAGE}
EOF
            ;;
        nats)
            container="golib-nats-${slug}"
            docker run --detach --name "${container}" -p 127.0.0.1::4222 \
                "${NATS_IMAGE}" >/dev/null
            record "${container}"
            sleep 2
            port="$(published_port "${container}" 4222)"
            printf 'NATS_URL=nats://127.0.0.1:%s\n' "${port}" >>"${environment_file}"
            ;;
        nsq)
            container="golib-nsq-${slug}"
            docker run --detach --name "${container}" -p 127.0.0.1::4150 \
                "${NSQ_IMAGE}" /nsqd --broadcast-address=127.0.0.1 >/dev/null
            record "${container}"
            sleep 2
            port="$(published_port "${container}" 4150)"
            printf 'NSQD_TCP_ADDRESS=127.0.0.1:%s\n' "${port}" >>"${environment_file}"
            ;;
        rabbitmq)
            container="golib-rabbitmq-${slug}"
            docker run --detach --name "${container}" --hostname "${container}" \
                --user rabbitmq \
                -p 127.0.0.1::5672 \
                "${RABBITMQ_IMAGE}" >/dev/null
            record "${container}"
            wait_for "${container}" rabbitmq-diagnostics -q ping
            port="$(published_port "${container}" 5672)"
            printf 'RABBITMQ_URL=amqp://guest:guest@127.0.0.1:%s/\n' \
                "${port}" >>"${environment_file}"
            ;;
        *)
            printf 'unsupported required service %s for %s\n' \
                "${service}" "${module}" >&2
            exit 1
            ;;
    esac
done <<<"${services}"

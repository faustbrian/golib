#!/usr/bin/env bash
set -euo pipefail

if [[ -n "${POSTGRES_URL:-}" ]]; then
	go test -tags=integration -race -count=1 -timeout=15m ./postgres
	exit
fi

name="calendar-postgres-$$"
image=${POSTGRES_IMAGE:-postgres:18@sha256:32ca0af8e77bfb8c6610c488e4691f83f972a3e9e64d3b02facf3ab111ad5500}
cleanup() {
	docker rm --force "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT
if ! docker run --detach --name "$name" \
	-e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=calendar \
	-p 127.0.0.1::5432 "$image" >/dev/null; then
	printf 'failed to start disposable PostgreSQL image %s\n' "$image" >&2
	exit 1
fi

for _ in {1..60}; do
	if docker exec "$name" pg_isready -U postgres -d calendar >/dev/null 2>&1; then
		break
	fi
	if [[ "$(docker inspect --format '{{.State.Running}}' "$name")" != true ]]; then
		printf 'disposable PostgreSQL exited during startup\n' >&2
		docker logs "$name" >&2
		exit 1
	fi
	sleep 1
done
if ! docker exec "$name" pg_isready -U postgres -d calendar >/dev/null 2>&1; then
	printf 'disposable PostgreSQL did not become ready\n' >&2
	docker logs "$name" >&2
	exit 1
fi
port=$(docker port "$name" 5432/tcp | awk -F: '{print $NF}')
POSTGRES_URL="postgres://postgres:postgres@127.0.0.1:${port}/calendar?sslmode=disable" \
	go test -tags=integration -race -count=1 -timeout=15m ./postgres

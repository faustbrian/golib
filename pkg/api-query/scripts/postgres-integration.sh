#!/bin/sh
set -eu

if [ -n "${APIQUERY_TEST_DATABASE_URL:-}" ]; then
	go test -count=1 -run '^TestPostgresInjectionResistanceAndStableCursorOrder$' ./apiquerypgx
	exit 0
fi

image=${POSTGRES_IMAGE:-postgres@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15}
name="apiquery-postgres-$$"
container=$(docker run --rm -d --name "$name" -e POSTGRES_PASSWORD=postgres \
	-e POSTGRES_DB=apiquery_test -P "$image")
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

port=$(docker port "$container" 5432/tcp | sed -n 's/.*://p' | head -n 1)
ready=0
attempt=0
while [ "$attempt" -lt 30 ]; do
	if pg_isready -h 127.0.0.1 -p "$port" -U postgres >/dev/null 2>&1; then
		ready=1
		break
	fi
	attempt=$((attempt + 1))
	sleep 1
done
if [ "$ready" -ne 1 ]; then
	docker logs "$container"
	exit 1
fi

APIQUERY_TEST_DATABASE_URL="postgres://postgres:postgres@127.0.0.1:$port/apiquery_test?sslmode=disable" \
	go test -count=1 -run '^TestPostgresInjectionResistanceAndStableCursorOrder$' ./apiquerypgx

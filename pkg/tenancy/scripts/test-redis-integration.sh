#!/usr/bin/env bash
set -euo pipefail

module_directory="$(cd "$(dirname "$0")/.." && pwd)"
container="golib-tenancy-redis-$$"
cleanup() {
    docker rm -f "${container}" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

docker run -d --name "${container}" -p 127.0.0.1::6379 \
    --cpus=1 --memory=256m --pids-limit=128 --ulimit nofile=1024:1024 \
    redis:6.2.22@sha256:3b477db2f54035771360d023c9aff4c6255ba833834511b8eedc5ba8c10d0bce >/dev/null
port="$(docker port "${container}" 6379/tcp | sed -n 's/.*://p')"
ready=0
for _ in $(seq 1 60); do
    if docker exec "${container}" redis-cli ping 2>/dev/null | grep -q PONG; then
        ready=1
        break
    fi
    sleep 1
done
test "${ready}" -eq 1
REDIS_ADDR="127.0.0.1:${port}" "${module_directory}/scripts/check-redis-integration.sh"

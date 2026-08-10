#!/usr/bin/env bash
set -euo pipefail

module_directory="$(cd "$(dirname "$0")/.." && pwd)"
container=""
cleanup() {
    if [[ -n "${container}" ]]; then
        docker rm -f "${container}" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT HUP INT TERM

for version in 2.19.6 3.8.0; do
    container="golib-tenancy-opensearch-${version//./-}-$$"
    docker run -d --name "${container}" -p 127.0.0.1::9200 \
        --cpus=1 --memory=1g --pids-limit=512 --ulimit nofile=1024:1024 \
        -e discovery.type=single-node \
        -e DISABLE_SECURITY_PLUGIN=true \
        -e OPENSEARCH_JAVA_OPTS='-Xms512m -Xmx512m' \
        "opensearchproject/opensearch:${version}" >/dev/null
    port="$(docker port "${container}" 9200/tcp | sed -n 's/.*://p')"
    ready=0
    for _ in $(seq 1 120); do
        if curl --fail --silent "http://127.0.0.1:${port}/" >/dev/null; then
            ready=1
            break
        fi
        sleep 1
    done
    if [[ "${ready}" -ne 1 ]]; then
        docker logs "${container}" >&2
        exit 1
    fi
    OPENSEARCH_URL="http://127.0.0.1:${port}" \
    OPENSEARCH_EXPECTED_VERSION="${version}" \
        "${module_directory}/scripts/check-opensearch-integration.sh"
    docker rm -f "${container}" >/dev/null
    container=""
done

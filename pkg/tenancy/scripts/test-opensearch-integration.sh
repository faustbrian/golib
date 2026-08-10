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

for image in \
    '2.19.6@sha256:8690b204fe914c60ca76d451ac73bc0481e034d32d3779944c8caca56a2b003f' \
    '3.8.0@sha256:bcc1797519726ceb6d651d4a3e60b7c30da91793914a8dfe75fd441d4f641509'; do
    version="${image%@*}"
    container="golib-tenancy-opensearch-${version//./-}-$$"
    docker run -d --name "${container}" -p 127.0.0.1::9200 \
        --cpus=1 --memory=1g --pids-limit=512 --ulimit nofile=1024:1024 \
        -e discovery.type=single-node \
        -e DISABLE_SECURITY_PLUGIN=true \
        -e OPENSEARCH_JAVA_OPTS='-Xms512m -Xmx512m' \
        "opensearchproject/opensearch:${image}" >/dev/null
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

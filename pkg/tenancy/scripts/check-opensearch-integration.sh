#!/usr/bin/env bash
set -euo pipefail

: "${OPENSEARCH_URL:?OPENSEARCH_URL is required}"
: "${OPENSEARCH_EXPECTED_VERSION:?OPENSEARCH_EXPECTED_VERSION is required}"

module_directory="$(cd "$(dirname "$0")/.." && pwd)"
repository_root="$(git -C "${module_directory}" rev-parse --show-toplevel)"
consumer="$(mktemp -d "${TMPDIR:-/tmp}/tenancy-opensearch.XXXXXX")"
cleanup() {
    find "${consumer}" -depth -delete
}
trap cleanup EXIT HUP INT TERM

cd "${consumer}"
GOWORK=off go mod init example.com/tenancy-opensearch
GOWORK=off go mod edit \
    -require=github.com/faustbrian/golib/pkg/search@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/search/adapters/opensearch@v0.0.0 \
    -require=github.com/faustbrian/golib/pkg/tenancy@v0.0.0 \
    -replace="github.com/faustbrian/golib/pkg/search=${repository_root}/pkg/search" \
    -replace="github.com/faustbrian/golib/pkg/search/adapters/opensearch=${repository_root}/pkg/search/adapters/opensearch" \
    -replace="github.com/faustbrian/golib/pkg/tenancy=${module_directory}"
cp "${module_directory}/scripts/opensearch/consumer_test.go.tmpl" consumer_test.go
GOWORK=off go mod tidy
GOWORK=off go test -race -count=1 ./...

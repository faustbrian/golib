#!/usr/bin/env bash
set -euo pipefail

module_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repository_root="$(cd "${module_root}/../.." && pwd)"
task_root="$(mktemp -d)"
task_gocache="$(mktemp -d)"
task_modcache="$(mktemp -d)"

cleanup() {
    chmod -R u+w "${task_root}" "${task_gocache}" "${task_modcache}" 2>/dev/null || true
    find "${task_root}" -depth -delete
    find "${task_gocache}" -depth -delete
    find "${task_modcache}" -depth -delete
}
trap cleanup EXIT

cp "${module_root}/testdata/interoperability/interoperability_test.go.txt" \
    "${task_root}/interoperability_test.go"
cd "${task_root}"
export GOCACHE="${task_gocache}"
export GOMODCACHE="${task_modcache}"
export GOWORK=off

go mod init workflow-interoperability.invalid/test
go mod edit -go=1.26.5
go mod edit -require=github.com/faustbrian/golib/pkg/workflow@v0.0.0
go mod edit -require=github.com/faustbrian/golib/pkg/outbox@v0.0.0
go mod edit -replace="github.com/faustbrian/golib/pkg/workflow=${module_root}"
go mod edit -replace="github.com/faustbrian/golib/pkg/outbox=${repository_root}/pkg/outbox"
go mod tidy
go test ./... -count=1

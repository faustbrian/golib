#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

(
    cd "$root/interoperability"
    go mod tidy -diff
    go mod verify >/dev/null
)

cp "$root/interoperability/go.mod" "$temporary/go.mod"
cp "$root/interoperability/go.sum" "$temporary/go.sum"
cp "$root/interoperability/runner.go" "$temporary/runner.go"
cd "$temporary"
go mod edit -replace \
    "github.com/faustbrian/golib/pkg/openapi=$root"
go mod verify >/dev/null

report="$temporary/report.tsv"
go run -mod=readonly -tags interop . \
    "$root"/interoperability/fixtures/* \
    "$root"/specification/independent/swagger-petstore/openapi.yaml \
    "$root"/specification/independent/github-rest-api/api.github.com.2022-11-28.json \
    >"$report"

if [ "${INTEROP_UPDATE:-false}" = true ]; then
    cp "$report" "$root/interoperability/expected.tsv"
    exit 0
fi

diff -u "$root/interoperability/expected.tsv" "$report"

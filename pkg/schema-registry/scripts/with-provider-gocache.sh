#!/usr/bin/env bash
set -euo pipefail

module_root=$(pwd -P)
core_root=$(cd ../.. && pwd -P)
temporary=$(mktemp -d)
cache=$(mktemp -d)
cleanup() {
	chmod -R u+w "$temporary" "$cache" 2>/dev/null || true
	find "$temporary" -depth -delete
	find "$cache" -depth -delete
}
trap cleanup EXIT

cp "$module_root/go.mod" "$temporary/provider.mod"
test ! -e "$module_root/go.sum" || cp "$module_root/go.sum" "$temporary/provider.sum"
GOWORK=off GOCACHE="$cache" go mod edit -modfile="$temporary/provider.mod" \
	-replace="github.com/faustbrian/golib/pkg/schema-registry=${core_root}"
GOWORK=off GOCACHE="$cache" GOFLAGS="-modfile=$temporary/provider.mod" "$@"

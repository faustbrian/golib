#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
	echo 'usage: run-provider-tool.sh <tool-package@version> <binary> [args...]' >&2
	exit 2
fi

tool=$1
binary=$2
shift 2

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

mkdir "$temporary/module"
cp -R "$module_root/." "$temporary/module/"
GOWORK=off GOCACHE="$cache" go mod edit -C "$temporary/module" \
	-replace="github.com/faustbrian/golib/pkg/schema-registry=${core_root}"

GOWORK=off GOCACHE="$cache" GOBIN="$temporary/bin" go install "$tool"
(
	cd "$temporary/module"
	GOWORK=off GOCACHE="$cache" "$temporary/bin/$binary" "$@"
)

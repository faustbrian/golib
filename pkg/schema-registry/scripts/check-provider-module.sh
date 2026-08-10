#!/usr/bin/env bash
set -euo pipefail

mode=${1:-check}
module_root=$(pwd -P)
core_root=$(cd ../.. && pwd -P)
temporary=$(mktemp -d)
cleanup() {
	find "$temporary" -depth -delete
}
trap cleanup EXIT

test ! -e go.sum || cp go.sum "$temporary/module.sum"
cp go.mod "$temporary/module.mod"
GOWORK=off go mod edit -modfile="$temporary/module.mod" \
	-replace="github.com/faustbrian/golib/pkg/schema-registry=${core_root}"
(
	cd "$module_root"
	GOWORK=off go mod tidy -modfile="$temporary/module.mod"
)
GOWORK=off go mod edit -modfile="$temporary/module.mod" \
	-dropreplace=github.com/faustbrian/golib/pkg/schema-registry

if [[ "$mode" == update ]]; then
	cp "$temporary/module.mod" go.mod
	cp "$temporary/module.sum" go.sum
	exit 0
fi
if [[ "$mode" != check ]]; then
	echo 'usage: check-provider-module.sh [check|update]' >&2
	exit 1
fi
diff -u go.mod "$temporary/module.mod"
test -f go.sum || { echo 'go.sum is missing' >&2; exit 1; }
diff -u go.sum "$temporary/module.sum"
GOWORK=off go mod edit -modfile="$temporary/module.mod" \
	-replace="github.com/faustbrian/golib/pkg/schema-registry=${core_root}"
GOWORK=off go mod verify -modfile="$temporary/module.mod"
GOWORK=off go list -modfile="$temporary/module.mod" -deps ./... >/dev/null

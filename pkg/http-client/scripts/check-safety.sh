#!/usr/bin/env sh

set -eu

if find . -name '*.go' ! -name '*_test.go' \
	-exec grep -El '(^|[[:space:]])import[[:space:]]+"C"|"unsafe"|//go:linkname' {} + | \
	grep -q .; then
	echo "GO-SAFETY-1 violation: production cgo, unsafe, or go:linkname" >&2
	exit 1
fi

go mod verify
govulncheck ./...

echo "GO-SAFETY-1: passed"

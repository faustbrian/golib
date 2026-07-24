#!/bin/sh
set -eu

patterns='import[[:space:]]+"C"|"unsafe"|go:linkname|runtime\.SetFinalizer'
matches=$(rg -n --glob '*.go' --glob '!*_test.go' "$patterns" . || true)

if [ -n "$matches" ]; then
	echo "GO-SAFETY-1 violation:" >&2
	echo "$matches" >&2
	exit 1
fi

echo "GO-SAFETY-1: no production unsafe, cgo, go:linkname, or finalizers"

#!/bin/sh
set -eu

CGO_ENABLED=0
export CGO_ENABLED

production=$(find . -name '*.go' ! -name '*_test.go' ! -path './vendor/*' -print)
if grep -nE '(^|[[:space:]])(import[[:space:]]+"C"|"unsafe"|//go:linkname)' $production; then
    echo 'GO-SAFETY-1: unsafe, cgo, or go:linkname found in production' >&2
    exit 1
fi
if go list -deps -f '{{if .CgoFiles}}{{.ImportPath}} {{.CgoFiles}}{{end}}' ./... | grep .; then
    echo 'GO-SAFETY-1: cgo dependency found' >&2
    exit 1
fi
echo 'GO-SAFETY-1: passed'

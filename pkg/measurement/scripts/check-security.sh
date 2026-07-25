#!/usr/bin/env bash
set -euo pipefail

if rg -n --glob '*.go' --glob '!**/*_test.go' \
	'go:linkname|import "C"|(^|[^[:alnum:]_])unsafe([^[:alnum:]_]|$)' .; then
	printf 'forbidden unsafe or cgo mechanism found\n' >&2
	exit 1
fi
rg -q 'MaxTextBytes' parse.go
rg -q 'MaxSerializedBytes' encoding.go
rg -q 'MaxPackageQuantity' dimensions.go
printf 'measurement trust-boundary guards verified\n'

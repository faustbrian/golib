#!/usr/bin/env bash
set -euo pipefail

if rg -n --glob '*.go' --glob '!doc.go' --glob '!**/*_test.go' \
	'github.com/faustbrian/golib/(money|geo)|(^|[^[:alnum:]_])(float32|float64)([^[:alnum:]_]|$)' .; then
	printf 'forbidden ownership or binary-float dependency found\n' >&2
	exit 1
fi
imports=$(go list -f '{{join .Imports "\n"}}' ./...)
if printf '%s\n' "$imports" | rg '^unsafe$'; then
	printf 'unsafe is forbidden\n' >&2
	exit 1
fi

#!/bin/sh
set -eu

dependencies=$(go list -deps ./...)
for forbidden in \
	github.com/faustbrian/golib/pkg/authentication \
	github.com/faustbrian/golib/pkg/service \
	github.com/faustbrian/golib/pkg/log \
	github.com/faustbrian/golib/pkg/telemetry \
	github.com/faustbrian/golib/pkg/postgres
do
	if printf '%s\n' "$dependencies" | grep -Eq "^${forbidden}(/|$)"; then
		printf 'forbidden reverse dependency: %s\n' "$forbidden" >&2
		exit 1
	fi
done

if rg -n '(^|[[:space:]])"unsafe"|import "C"' --glob '*.go' --glob '!*_test.go' .; then
	printf '%s\n' 'unsafe or cgo is forbidden' >&2
	exit 1
fi

if rg --files --glob '*.s' --glob '!.git/**' | grep -q .; then
	printf '%s\n' 'custom assembly is forbidden' >&2
	exit 1
fi

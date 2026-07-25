#!/usr/bin/env bash
set -euo pipefail

temporary="$(mktemp -d)"
trap 'rm -rf "${temporary}"' EXIT

go test -covermode=atomic -coverpkg=./... \
	-coverprofile="${temporary}/coverage.out" ./...
coverage="$(
	go tool cover -func="${temporary}/coverage.out" |
		awk '/^total:/ { print $3 }'
)"
if [[ "${coverage}" != "100.0%" ]]; then
	echo "production statement coverage is ${coverage}, want 100.0%" >&2
	exit 1
fi

echo "production statement coverage: ${coverage}"

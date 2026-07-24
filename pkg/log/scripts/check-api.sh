#!/bin/sh
set -eu

: "${APIDIFF_VERSION:?APIDIFF_VERSION is required}"

module=github.com/faustbrian/golib/pkg/log
baseline=api/log.txt
current=$(mktemp "${TMPDIR:-/tmp}/log-api.XXXXXX")
report=$(mktemp "${TMPDIR:-/tmp}/log-api-report.XXXXXX")
trap 'rm -f "$current" "$report"' EXIT HUP INT TERM

if [ ! -f "$baseline" ]; then
	printf 'missing API baseline: %s\n' "$baseline" >&2
	exit 1
fi

go run "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" \
	-m -w "$current" "$module"
go run "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" \
	-m -incompatible "$baseline" "$current" >"$report"

if [ -s "$report" ]; then
	printf '%s\n' 'incompatible exported API changes:' >&2
	cat "$report" >&2
	exit 1
fi

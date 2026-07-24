#!/bin/sh
set -eu
# shellcheck disable=SC1091 # Repository-local pinned version manifest.
. ./tools/versions.env

baseline=api/root.txt
current=$(mktemp "${TMPDIR:-/tmp}/password-api.XXXXXX")
report=$(mktemp "${TMPDIR:-/tmp}/password-api-report.XXXXXX")
trap 'rm -f "$current" "$report"' EXIT HUP INT TERM

if [ ! -s "$baseline" ]; then
	printf 'missing API baseline: %s\n' "$baseline" >&2
	exit 1
fi

go run "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" -m -w "$current" github.com/faustbrian/golib/pkg/password
go run "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" -m -incompatible "$baseline" "$current" >"$report"
if [ -s "$report" ]; then
	printf '%s\n' 'incompatible exported API changes:' >&2
	cat "$report" >&2
	exit 1
fi

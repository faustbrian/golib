#!/bin/sh
set -eu
# shellcheck disable=SC1091 # Repository-local pinned version manifest.
. ./tools/versions.env

run_apidiff() {
	if [ -n "${GOLIB_APIDIFF:-}" ]; then
		go exec-tool "$GOLIB_APIDIFF" "$@"
	else
		go run "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" "$@"
	fi
}

baseline=api/root.txt
current=$(mktemp "${TMPDIR:-/tmp}/password-api.XXXXXX")
report=$(mktemp "${TMPDIR:-/tmp}/password-api-report.XXXXXX")
trap 'rm -f "$current" "$report"' EXIT HUP INT TERM

if [ ! -s "$baseline" ]; then
	printf 'missing API baseline: %s\n' "$baseline" >&2
	exit 1
fi

run_apidiff -m -w "$current" github.com/faustbrian/golib/pkg/password
run_apidiff -m -incompatible "$baseline" "$current" >"$report"
if [ -s "$report" ]; then
	printf '%s\n' 'incompatible exported API changes:' >&2
	cat "$report" >&2
	exit 1
fi

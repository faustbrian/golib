#!/bin/sh
set -eu

: "${APIDIFF_VERSION:?APIDIFF_VERSION is required}"

root=$(pwd)

check() {
	directory=$1
	module=$2
	baseline=$3
	current=$(mktemp "${TMPDIR:-/tmp}/go-auth-api.XXXXXX")
	report=$(mktemp "${TMPDIR:-/tmp}/go-auth-api-report.XXXXXX")
	trap 'rm -f "$current" "$report"' EXIT HUP INT TERM
	if [ ! -f "$root/$baseline" ]; then
		printf 'missing API baseline: %s\n' "$baseline" >&2
		exit 1
	fi
	(
		cd "$directory"
		go run "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" -m -w "$current" "$module"
		go run "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" \
			-m -incompatible "$root/$baseline" "$current" >"$report"
	)
	if [ -s "$report" ]; then
		printf 'incompatible exported API changes in %s:\n' "$module" >&2
		cat "$report" >&2
		exit 1
	fi
	rm -f "$current" "$report"
	trap - EXIT HUP INT TERM
}

check . github.com/faustbrian/golib/pkg/authentication api/root.txt

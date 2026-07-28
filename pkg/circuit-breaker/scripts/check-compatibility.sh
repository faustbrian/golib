#!/bin/sh
set -eu

: "${APIDIFF_VERSION:?APIDIFF_VERSION is required}"

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
baseline="$root/api/baseline.txt"
current=$(mktemp "${TMPDIR:-/tmp}/circuit-breaker-api.XXXXXX")
report=$(mktemp "${TMPDIR:-/tmp}/circuit-breaker-api-report.XXXXXX")
trap 'rm -f "$current" "$report"' EXIT HUP INT TERM

if [ ! -s "$baseline" ]; then
	echo "API compatibility baseline is missing: $baseline" >&2
	exit 1
fi

run_apidiff() {
	if [ -n "${GOLIB_APIDIFF:-}" ]; then
		go exec-tool "$GOLIB_APIDIFF" "$@"
	else
		go run "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" "$@"
	fi
}

cd "$root"
GOWORK=off run_apidiff -m -w "$current" \
	github.com/faustbrian/golib/pkg/circuit-breaker
GOWORK=off run_apidiff -m -incompatible "$baseline" "$current" >"$report"
test ! -s "$report" || {
	cat "$report" >&2
	exit 1
}

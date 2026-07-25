#!/bin/sh
set -eu

for module in . jwt oidc authotel; do
	name=$(basename "$(cd "$module" && pwd)")
	profile=$(mktemp "${TMPDIR:-/tmp}/${name}-coverage.XXXXXX")
	report=$(mktemp "${TMPDIR:-/tmp}/${name}-coverage-report.XXXXXX")
	trap 'rm -f "$profile" "$report"' EXIT HUP INT TERM
	(
		cd "$module"
		go test -covermode=atomic -coverpkg=./... -coverprofile="$profile" ./...
		go tool cover -func="$profile"
	) | tee "$report"
	awk -v module="$module" '
	$1 == "total:" {
		gsub(/%/, "", $3)
		found = 1
		if (($3 + 0) != 100) {
			printf "%s statement coverage is %s%%, want 100%%\n", module, $3 > "/dev/stderr"
			exit 1
		}
	}
	END {
		if (!found) {
			printf "%s coverage report has no total\n", module > "/dev/stderr"
			exit 1
		}
	}
	' "$report"
	rm -f "$profile" "$report"
	trap - EXIT HUP INT TERM
done

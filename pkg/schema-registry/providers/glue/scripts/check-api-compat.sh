#!/usr/bin/env bash
set -euo pipefail

current=$(mktemp)
cleanup() {
	find "$current" -delete
}
trap cleanup EXIT
../../scripts/with-provider-gocache.sh go doc -all . > "$current"
perl -0pi -e 's/\n+\z/\n/' "$current"
if [[ "${1:-}" == "--update" ]]; then
	mkdir -p api
	cp "$current" api/baseline.txt
	exit 0
fi
test -f api/baseline.txt || { echo 'api/baseline.txt is missing; run make api-update' >&2; exit 1; }
diff -u api/baseline.txt "$current"

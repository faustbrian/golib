#!/usr/bin/env bash
set -euo pipefail

current=$(mktemp)
cache=$(mktemp -d)
cleanup() {
	find "$current" -delete
	find "$cache" -depth -delete
}
trap cleanup EXIT
while IFS= read -r package; do
	printf 'PACKAGE %s\n' "$package" >> "$current"
	GOWORK=off GOCACHE="$cache" go doc -all "$package" >> "$current"
done < <(GOWORK=off GOCACHE="$cache" go list ./...)
perl -0pi -e 's/\n+\z/\n/' "$current"
if [[ "${1:-}" == "--update" ]]; then
	mkdir -p api
	cp "$current" api/baseline.txt
	exit 0
fi
test -f api/baseline.txt || { echo 'api/baseline.txt is missing; run make api-update' >&2; exit 1; }
diff -u api/baseline.txt "$current"

#!/bin/sh
set -eu

temporary=$(mktemp)
trap 'rm -f "$temporary"' EXIT
for package in $(go list ./...); do
	printf '## %s\n' "$package" >>"$temporary"
	go doc -short "$package" >>"$temporary"
done
if [ "${1:-}" = "--update" ]; then
	cp "$temporary" api/baseline.txt
	exit 0
fi
diff -u api/baseline.txt "$temporary"


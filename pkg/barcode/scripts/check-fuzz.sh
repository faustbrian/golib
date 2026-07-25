#!/bin/sh
set -eu

duration="${1:-2s}"
found=0

for package in $(go list ./...); do
	for target in $(go test "$package" -list '^Fuzz' | grep '^Fuzz' || true); do
		found=1
		go test "$package" -run '^$' -fuzz="^${target}$" -fuzztime="$duration"
	done
done

if [ "$found" -eq 0 ]; then
	printf '%s\n' 'no fuzz targets found' >&2
	exit 1
fi

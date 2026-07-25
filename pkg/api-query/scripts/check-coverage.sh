#!/bin/sh
set -eu

directory=$(mktemp -d)
trap 'rm -rf "$directory"' EXIT INT TERM

packages='.
./apiqueryhttp
./apiqueryjsonapi
./apiquerypgx
./apiqueryrpc
./apiqueryvalidation
./cursor
./internal/strictjson'

for package in $packages; do
	name=$(printf '%s' "$package" | tr '/.' '__')
	profile="$directory/$name.out"
	go test -covermode=atomic -coverprofile="$profile" "$package"
	total=$(go tool cover -func="$profile" | awk '/^total:/ {print $3}')
	if [ "$total" != '100.0%' ]; then
		printf '%s coverage is %s, want 100.0%%\n' "$package" "$total"
		exit 1
	fi
done

printf '%s\n' 'All production runtime packages have 100.0% statement coverage.'

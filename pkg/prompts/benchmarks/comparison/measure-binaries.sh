#!/bin/sh
set -eu

output="$(mktemp -d)"
trap 'rm -rf "$output"' EXIT HUP INT TERM

for engine in prompts huh survey promptui bubbles; do
	CGO_ENABLED=0 GOWORK=off go build -trimpath \
		-ldflags '-s -w -buildid=' -o "$output/$engine" "./cmd/$engine"
	bytes="$(wc -c < "$output/$engine" | tr -d ' ')"
	printf '%s\t%s\n' "$engine" "$bytes"
	if test "$engine" = prompts && test "$bytes" -gt 2500000; then
		printf 'prompts binary exceeds 2500000-byte budget\n' >&2
		exit 1
	fi
done

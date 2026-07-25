#!/bin/sh

set -eu

: "${GREMLINS_VERSION:?GREMLINS_VERSION is required}"

run_mutation() {
	package=$1
	if output=$(go run \
		"github.com/go-gremlins/gremlins/cmd/gremlins@${GREMLINS_VERSION}" \
		unleash "$package" \
		--exclude-files '.*testdata.*' \
		--workers 2 \
		--timeout-coefficient 30 \
		--threshold-efficacy 100 \
		--threshold-mcover 100 2>&1); then
		printf '%s\n' "$output"
	else
		status=$?
		printf '%s\n' "$output" >&2
		exit "$status"
	fi

	case "$output" in
		*"Lived: 0, Not covered: 0"*) ;;
		*)
			echo "$package left surviving or uncovered mutants" >&2
			exit 1
			;;
	esac

	case "$output" in
		*"Timed out: 0"*) ;;
		*)
			echo "$package mutation run timed out" >&2
			exit 1
			;;
	esac

	case "$output" in
		*"Test efficacy: 100.00%"*) ;;
		*)
			echo "$package mutation test efficacy is below 100%" >&2
			exit 1
			;;
	esac

	case "$output" in
		*"Mutator coverage: 100.00%"*) ;;
		*)
			echo "$package mutator coverage is below 100%" >&2
			exit 1
			;;
	esac
}

run_mutation ./analysis
run_mutation ./internal/driver
run_mutation ./policy
for package in ./analyzers/*; do
	set -- "$package"/*.go
	if [ ! -f "$1" ]; then
		continue
	fi
	run_mutation "$package"
done

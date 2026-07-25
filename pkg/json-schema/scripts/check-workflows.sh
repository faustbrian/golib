#!/bin/sh
set -eu

actionlint_version=${ACTIONLINT_VERSION:-v1.7.12}
workflows=../.github/workflows/json-schema\*.yml

# shellcheck disable=SC2086 # The glob deliberately expands to owned workflows.
go run "github.com/rhysd/actionlint/cmd/actionlint@${actionlint_version}" $workflows

# shellcheck disable=SC2086 # The glob deliberately expands to owned workflows.
for workflow in $workflows; do
	grep -q '^permissions:' "$workflow" || {
		printf '%s: missing top-level permissions\n' "$workflow" >&2
		exit 1
	}
	if grep -q 'pull_request_target:' "$workflow"; then
		printf '%s: pull_request_target is forbidden\n' "$workflow" >&2
		exit 1
	fi
	if ! awk '
		/^[[:space:]]*- uses:/ {
			ref = $3
			sub(/^.*@/, "", ref)
			if (ref !~ /^[0-9a-f]{40}$/) {
				print FILENAME ": action is not pinned by SHA: " $3 > "/dev/stderr"
				exit 1
			}
		}
	' "$workflow"; then
		exit 1
	fi
done

#!/usr/bin/env bash
set -euo pipefail

profile=$(mktemp)
trap 'rm -f "$profile"' EXIT

packages=$(go list ./... | grep -v '/calendartest$')
cover_packages=$(printf '%s\n' "$packages" | paste -sd, -)
# shellcheck disable=SC2086
go test -coverpkg="$cover_packages" -coverprofile="$profile" $packages
coverage=$(go tool cover -func="$profile" | awk '/^total:/ {gsub("%", "", $3); print $3}')

printf 'meaningful production statement coverage: %s%%\n' "$coverage"
if [[ "$coverage" != "100.0" ]]; then
	printf 'meaningful production coverage must remain 100.0%%\n' >&2
	exit 1
fi

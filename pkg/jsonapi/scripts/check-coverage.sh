#!/usr/bin/env bash
set -euo pipefail

profile="${1:-$(mktemp)}"
remove_profile=false
if [[ $# -eq 0 ]]; then
  remove_profile=true
fi
cleanup() {
  if [[ "$remove_profile" == true ]]; then
    rm -f "$profile"
  fi
}
trap cleanup EXIT

go test . -coverprofile="$profile"
go tool cover -func="$profile"

if awk -F'[: ,]+' '$NF == 0 { print; missing = 1 } END { exit missing ? 0 : 1 }' "$profile"; then
  echo "coverage profile contains zero-count production statements" >&2
  exit 1
fi

total="$(go tool cover -func="$profile" | awk '/^total:/ { print $3 }')"
if [[ "$total" != "100.0%" ]]; then
  echo "production statement coverage is $total, want 100.0%" >&2
  exit 1
fi

echo "production statement coverage is exactly 100.0%"

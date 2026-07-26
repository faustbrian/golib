#!/usr/bin/env bash
set -euo pipefail

baseline='api/stable.txt'
current="$(mktemp)"
trap 'rm -f "$current"' EXIT

packages=(
  . ./encoding ./http ./localizedconfig ./localizedhttpclient
  ./localizedquery ./localizedtest
  ./localizedvalidation ./localizedwire ./match ./postgres
)
for package in "${packages[@]}"; do
  printf '## %s\n' "$package" >> "$current"
  go doc -short "$package" >> "$current"
done

if [[ ! -s "$baseline" ]]; then
  echo "API baseline is missing or empty: $baseline" >&2
  exit 1
fi

diff -u "$baseline" "$current"

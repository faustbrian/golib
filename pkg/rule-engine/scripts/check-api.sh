#!/usr/bin/env bash
set -euo pipefail

short="$(mktemp)"
full="$(mktemp)"
trap 'rm -f "$short" "$full"' EXIT
go doc -short . >"$short"
diff -u api.txt "$short"

{
  go doc -all .
  go doc -all ./jsonast
} >"$full"
expected="$(cut -d' ' -f1 api-all.sha256)"
actual="$(shasum -a 256 "$full" | cut -d' ' -f1)"
test "$actual" = "$expected" || {
  echo "exported API fingerprint changed: $actual" >&2
  exit 1
}

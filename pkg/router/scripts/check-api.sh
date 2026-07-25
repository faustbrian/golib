#!/usr/bin/env bash
set -euo pipefail

temporary="$(mktemp)"
full="$(mktemp)"
trap 'rm -f "$temporary" "$full"' EXIT
go doc -short . >"$temporary"
diff -u api.txt "$temporary"

{
  go doc -all .
  go doc -all ./routertest
} >"$full"
expected="$(cut -d' ' -f1 api-all.sha256)"
actual="$(shasum -a 256 "$full" | cut -d' ' -f1)"
test "$actual" = "$expected" || {
  echo "exported API fingerprint changed: $actual" >&2
  exit 1
}

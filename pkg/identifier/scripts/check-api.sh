#!/usr/bin/env bash
set -euo pipefail

temporary="$(mktemp)"
trap 'rm -f "$temporary"' EXIT
for package in . ./uuid ./ulid ./typeid ./ksuid ./nanoid ./idtest; do
  go doc -all "$package"
done >"$temporary"

expected="$(cut -d' ' -f1 api.sha256)"
actual="$(shasum -a 256 "$temporary" | cut -d' ' -f1)"
test "$actual" = "$expected" || {
  echo "exported API fingerprint changed: $actual" >&2
  exit 1
}

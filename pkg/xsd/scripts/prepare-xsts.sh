#!/usr/bin/env bash
set -euo pipefail

work="$1"
url="https://www.w3.org/XML/2004/xml-schema-test-suite/xmlschema2006-11-06/xsts-2007-06-20.tar.gz"
expected="902176b25e4111cf96b08663107521a4992e8ea67aad6b815592a6a5b4b9ea06"
archive="${XSTS_ARCHIVE:-$work/xsts.tar.gz}"

if [[ -z "${XSTS_ARCHIVE:-}" ]]; then
  curl -fsSL "$url" -o "$archive"
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$archive" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
fi
[[ "$actual" == "$expected" ]] || {
  echo "XSTS digest mismatch: $actual" >&2
  exit 1
}

tar -xzf "$archive" -C "$work"
printf '%s\n' "$work/xmlschema2006-11-06"

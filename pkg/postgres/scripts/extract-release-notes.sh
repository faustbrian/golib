#!/usr/bin/env bash
set -euo pipefail

version="${1:?version is required}"
changelog="${2:-CHANGELOG.md}"

awk -v version="$version" '
  $0 ~ "^## \\[" version "\\]( - .*)?$" { found = 1; capture = 1; next }
  capture && (/^## \[/ || /^\[[^]]+\]:/) { exit }
  capture { print }
  END { if (!found) exit 1 }
' "$changelog"

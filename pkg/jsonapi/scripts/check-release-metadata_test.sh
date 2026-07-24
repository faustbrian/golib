#!/usr/bin/env bash
set -euo pipefail

directory="$(mktemp -d)"
cleanup() {
  rm -rf "$directory"
}
trap cleanup EXIT

license="$directory/LICENSE"
changelog="$directory/CHANGELOG.md"
touch "$license"

cat > "$changelog" <<'EOF'
# Changelog

## [1.2.3] - 2025-12-31
EOF
./scripts/check-release-metadata.sh v1.2.3 "$changelog" "$license"

expect_failure() {
  if "$@" >/dev/null 2>&1; then
    echo "release metadata validation unexpectedly passed: $*" >&2
    exit 1
  fi
}

expect_failure ./scripts/check-release-metadata.sh v1.2.4 "$changelog" "$license"
expect_failure ./scripts/check-release-metadata.sh v1.2.3 "$changelog" "$directory/MISSING"

cat > "$changelog" <<'EOF'
## [1.2.3] - 2025-02-30
EOF
expect_failure ./scripts/check-release-metadata.sh v1.2.3 "$changelog" "$license"

cat > "$changelog" <<'EOF'
## [1.2.3] - 2025-12-31
## [1.2.3] - 2026-01-01
EOF
expect_failure ./scripts/check-release-metadata.sh v1.2.3 "$changelog" "$license"

echo "release metadata validation requires a license and one valid dated heading"

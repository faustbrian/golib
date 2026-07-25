#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
go_migrations="${GO_MIGRATIONS_DIR:-$root/../migrations}"

test -f "$go_migrations/go.mod" || {
  echo "migrations checkout not found at $go_migrations" >&2
  exit 1
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

(
  cd "$tmp"
  go work init "$root" "$go_migrations"
)

cd "$root"
GOWORK="$tmp/go.work" OUTBOX_POSTGRES_VERSION="${POSTGRES_VERSION:-18}" \
  go test -count=1 -v \
  ./postgres/testdata/gomigrations/source \
  ./postgres/testdata/gomigrations/runner

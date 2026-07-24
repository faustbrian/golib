#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

cd "$root"
OUTBOX_POSTGRES_VERSION="${POSTGRES_VERSION:-18}" \
  go test -tags=integration -coverprofile="$tmp/core.out" ./...
core="$(go tool cover -func="$tmp/core.out" | awk '/^total:/ {print $3}')"
if [[ "$core" != "100.0%" ]]; then
  echo "core production coverage is $core, want 100.0%" >&2
  exit 1
fi

cd "$root/adapters/goqueue"
go test -coverprofile="$tmp/goqueue.out" ./...
adapter="$(go tool cover -func="$tmp/goqueue.out" | awk '/^total:/ {print $3}')"
if [[ "$adapter" != "100.0%" ]]; then
  echo "queue adapter production coverage is $adapter, want 100.0%" >&2
  exit 1
fi

cd "$root/adapters/gotelemetry"
go test -coverprofile="$tmp/gotelemetry.out" ./...
telemetry="$(go tool cover -func="$tmp/gotelemetry.out" | awk '/^total:/ {print $3}')"
if [[ "$telemetry" != "100.0%" ]]; then
  echo "telemetry adapter production coverage is $telemetry, want 100.0%" >&2
  exit 1
fi

echo "meaningful production coverage: core=$core goqueue=$adapter gotelemetry=$telemetry"

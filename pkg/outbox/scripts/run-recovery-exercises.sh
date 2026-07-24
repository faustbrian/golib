#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
postgres_version="${POSTGRES_VERSION:-18}"

cd "$root"
OUTBOX_POSTGRES_VERSION="$postgres_version" \
  go test -tags=integration -timeout=15m -count=1 -v ./postgres \
  -run '^(TestApplicationWriteAndOutboxRecordAreAtomic|TestHardeningPersistenceContracts)$'

go test -race -count=1 ./relay

cd "$root/adapters/goqueue"
GOWORK=off go test -race -count=1 ./...

echo "recovery exercises passed for PostgreSQL $postgres_version"

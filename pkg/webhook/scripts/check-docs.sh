#!/bin/sh
set -eu

for file in README.md SECURITY.md CONTRIBUTING.md CHANGELOG.md LICENSE THIRD_PARTY_NOTICES.md docs/api.md docs/audit-matrices.md docs/inbound.md docs/signatures.md docs/replay.md docs/delivery.md docs/security.md docs/integrations.md docs/providers.md docs/operations.md docs/migration.md docs/faq.md docs/findings.md docs/release-verdict.md
do
    test -s "$file" || { echo "missing documentation: $file" >&2; exit 1; }
done
go doc -all . >/dev/null
for package in ./adapters/goidempotency ./adapters/golog ./adapters/gotelemetry ./adapters/goqueue ./adapters/gooutbox ./webhooktest
do
    go doc -all "$package" >/dev/null
done
go test -run '^Example' ./...

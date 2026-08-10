#!/usr/bin/env bash
set -euo pipefail

required=(README.md CHANGELOG.md CONTRIBUTING.md RELEASING.md SECURITY.md LICENSE NOTICE \
	docs/api.md docs/architecture.md docs/providers.md docs/evolution.md docs/caching.md \
	docs/wire-formats.md docs/authentication.md docs/operations.md docs/migrations.md \
	docs/examples.md docs/security.md docs/faq.md docs/provenance.md docs/conformance.md)
for file in "${required[@]}"; do
	test -s "$file" || { echo "required documentation is missing: $file" >&2; exit 1; }
done
./scripts/with-gocache.sh go test ./... -run '^Example'

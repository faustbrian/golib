#!/bin/sh
set -eu

required='README.md
CHANGELOG.md
SECURITY.md
docs/api.md
docs/authorization.md
docs/compatibility.md
docs/cursor.md
docs/faq.md
docs/hardening.md
docs/http.md
docs/jsonapi.md
docs/migration-laravel-cline.md
docs/openrpc.md
docs/performance.md
docs/sqlc.md
docs/troubleshooting.md
docs/versioning.md'

for file in $required; do
	test -s "$file"
done

for package in $(go list ./...); do
	go doc "$package" >/dev/null
done

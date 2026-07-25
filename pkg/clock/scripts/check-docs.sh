#!/bin/sh
set -eu

required='README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md docs/api.md docs/compatibility.md docs/concurrency.md docs/faq.md docs/hardening.md docs/integration.md docs/migration.md docs/observations.md docs/performance.md docs/security-model.md docs/state-machines.md docs/synctest.md docs/troubleshooting.md docs/wall-and-monotonic.md'
for file in $required; do
    if [ ! -s "$file" ]; then
        echo "missing required documentation: $file" >&2
        exit 1
    fi
done

if rg -n 'TODO|TBD|FIXME' README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md docs; then
    echo "unfinished documentation marker found" >&2
    exit 1
fi

go test ./... -run '^Example'

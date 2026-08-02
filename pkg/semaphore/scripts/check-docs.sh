#!/bin/sh
set -eu

required='README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md docs/api.md docs/design.md docs/faq.md docs/kubernetes.md docs/migration.md docs/operations.md docs/performance.md'
for file in $required; do
    if [ ! -s "$file" ]; then
        echo "missing required documentation: $file" >&2
        exit 1
    fi
done
if rg -n 'TODO|TBD|FIXME' README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md docs; then
    echo 'unfinished documentation marker found' >&2
    exit 1
fi
GOWORK=off go test ./... -run '^Example' -count=1

#!/bin/sh
set -eu

required='README.md
SECURITY.md
CONTRIBUTING.md
CHANGELOG.md
ROADMAP.md
docs/api.md
docs/compatibility.md
docs/encoding.md
docs/faq.md
docs/hardening.md
docs/integrations.md
docs/migration.md
docs/operations.md
docs/performance.md
docs/postgres.md
docs/quickstart.md
docs/relations.md
docs/security.md
docs/semantics.md
docs/testing.md
docs/troubleshooting.md'

echo "$required" | while IFS= read -r file; do
    test -s "$file" || { echo "missing documentation: $file"; exit 1; }
done

grep -q 'Charting is intentionally unsupported' README.md
grep -q 'Unsupported charting gap' docs/migration.md
grep -q 'Chart' docs/compatibility.md

go list ./... | while IFS= read -r package; do
    go doc "$package" >/dev/null
done

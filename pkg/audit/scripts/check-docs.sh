#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
required=(
    README.md CHANGELOG.md CONTRIBUTING.md LICENSE SECURITY.md SUPPORT.md
    docs/README.md docs/api.md docs/adoption.md docs/delivery.md
    docs/threat-model.md docs/privacy.md docs/integrity.md
    docs/query-export.md docs/postgresql.md docs/retention.md
    docs/incident-use.md docs/faq.md
    postgres/README.md postgres/CHANGELOG.md postgres/LICENSE
    scripts/check-clean-consumer.sh
)

cd "${root}"
for path in "${required[@]}"; do
    test -s "${path}" || {
        printf 'missing required documentation: %s\n' "${path}" >&2
        exit 1
    }
done

packages="$(./scripts/with-gocache.sh go list ./...)"
while IFS= read -r package; do
    ./scripts/with-gocache.sh go doc "${package}" >/dev/null
done <<< "${packages}"

if grep -RniE 'automatically compliant|provides non.repudiation|event store is (a |an )?compliant audit' \
    --include='*.md' --include='*.go' .; then
    printf 'forbidden compliance or integrity claim detected\n' >&2
    exit 1
fi

printf 'required audit documentation and package docs are present\n'

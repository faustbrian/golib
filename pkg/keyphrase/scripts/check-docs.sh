#!/bin/sh
set -eu

for file in README.md SECURITY.md THIRD_PARTY_NOTICES.md docs/api.md docs/threat-model.md docs/entropy.md docs/bip39.md docs/wordlists.md docs/errors-and-limits.md docs/secret-lifetime.md docs/policies.md docs/adoption.md docs/testing.md docs/faq.md docs/security-review.md; do
    test -s "$file"
done

go list ./... | while IFS= read -r package; do
    go doc "$package" >/dev/null
done

if grep -RInE '(^|[^!])\[[^]]+\]\([^):#]+\.md\)' README.md docs | grep -vE '\((\.\./)?[^)]+\.md\)' >/dev/null; then
    printf '%s\n' 'documentation contains an invalid local Markdown link' >&2
    exit 1
fi

#!/bin/sh
set -eu

for file in AGENTS.md CLAUDE.md README.md LICENSE SECURITY.md CONTRIBUTING.md \
	CHANGELOG.md CODE_OF_CONDUCT.md SUPPORT.md GOVERNANCE.md doc.go \
	docs/api.md docs/architecture.md docs/benchmarks.md docs/capabilities.md \
	docs/migration.md docs/security.md docs/usage.md docs/model.md \
	docs/algorithms.md docs/recipes.md docs/adoption.md docs/faq.md \
	docs/release.md docs/benchmarks/2026-07-24-darwin-arm64.md \
	docs/benchmarks/raw/2026-07-24-darwin-arm64-rss.tsv \
	docs/benchmarks/raw/2026-07-24-boxpacker-runtime.json \
	example_test.go; do
	test -s "$file" || { printf 'missing documentation: %s\n' "$file" >&2; exit 1; }
done
go test ./... -run '^Example' -count=1

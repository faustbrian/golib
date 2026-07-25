#!/bin/sh
set -eu

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

printf '%s\n' 'MIT License' > "$temporary/LICENSE"
license_hash="$(shasum -a 256 "$temporary/LICENSE" | awk '{ print $1 }')"
printf 'example.invalid/dependency\tv1.0.0\t-\n' \
	> "$temporary/actual-ready.tsv"
{
	printf 'module\tversion\treplacement\tlicense_file\tspdx\tsha256\tlicense_status\trelease_status\n'
	printf 'example.invalid/dependency\tv1.0.0\t-\t%s\tMIT\t%s\tapproved\tready\n' \
		"$temporary/LICENSE" "$license_hash"
} > "$temporary/approved.tsv"

./scripts/check-dependency-review.sh --validate \
	"$temporary/approved.tsv" "$temporary/actual-ready.tsv"
./scripts/check-dependency-review.sh --validate-publish \
	"$temporary/approved.tsv" "$temporary/actual-ready.tsv"

printf 'example.invalid/dependency\tv1.0.0\t../dependency\n' \
	> "$temporary/actual-local.tsv"
{
	printf 'module\tversion\treplacement\tlicense_file\tspdx\tsha256\tlicense_status\trelease_status\n'
	printf 'example.invalid/dependency\tv1.0.0\t../dependency\t%s\tMIT\t%s\tapproved\tready\n' \
		"$temporary/LICENSE" "$license_hash"
} > "$temporary/invalid-ready-local.tsv"

if ./scripts/check-dependency-review.sh --validate \
	"$temporary/invalid-ready-local.tsv" "$temporary/actual-local.tsv" \
	> /dev/null 2> "$temporary/invalid-ready-local.err"; then
	printf '%s\n' 'release-ready dependencies must not use local replacements' >&2
	exit 1
fi

grep -F 'release-ready dependency has replacement: example.invalid/dependency' \
	"$temporary/invalid-ready-local.err" > /dev/null

printf 'example.invalid/dependency\tv0.0.0\t-\n' \
	> "$temporary/actual-placeholder.tsv"
{
	printf 'module\tversion\treplacement\tlicense_file\tspdx\tsha256\tlicense_status\trelease_status\n'
	printf 'example.invalid/dependency\tv0.0.0\t-\t%s\tMIT\t%s\tapproved\tready\n' \
		"$temporary/LICENSE" "$license_hash"
} > "$temporary/invalid-ready-placeholder.tsv"

if ./scripts/check-dependency-review.sh --validate \
	"$temporary/invalid-ready-placeholder.tsv" "$temporary/actual-placeholder.tsv" \
	> /dev/null 2> "$temporary/invalid-ready-placeholder.err"; then
	printf '%s\n' 'release-ready dependencies must use real versions' >&2
	exit 1
fi

grep -F 'release-ready dependency has placeholder version: example.invalid/dependency' \
	"$temporary/invalid-ready-placeholder.err" > /dev/null

printf 'example.invalid/dependency\tv0.0.0\t../dependency\n' \
	> "$temporary/actual-workspace.tsv"

{
	printf 'module\tversion\treplacement\tlicense_file\tspdx\tsha256\tlicense_status\trelease_status\n'
	printf 'example.invalid/dependency\tv0.0.0\t../dependency\t-\tUNKNOWN\t-\tmissing_license\tworkspace_only\n'
} > "$temporary/missing.tsv"

if ./scripts/check-dependency-review.sh --validate \
	"$temporary/missing.tsv" "$temporary/actual-workspace.tsv" \
	> /dev/null 2> "$temporary/missing.err"; then
	printf '%s\n' 'missing dependency licenses must fail closed' >&2
	exit 1
fi

grep -F 'dependency license is missing: example.invalid/dependency' \
	"$temporary/missing.err" > /dev/null
{
	printf 'module\tversion\treplacement\tlicense_file\tspdx\tsha256\tlicense_status\trelease_status\n'
	printf 'example.invalid/dependency\tv0.0.0\t../dependency\t%s\tMIT\t%s\tapproved\tworkspace_only\n' \
		"$temporary/LICENSE" "$license_hash"
} > "$temporary/workspace.tsv"

./scripts/check-dependency-review.sh --validate \
	"$temporary/workspace.tsv" "$temporary/actual-workspace.tsv"

if ./scripts/check-dependency-review.sh --validate-publish \
	"$temporary/workspace.tsv" "$temporary/actual-workspace.tsv" \
	> /dev/null 2> "$temporary/publish.err"; then
	printf '%s\n' 'workspace-only replacements must fail publication' >&2
	exit 1
fi

grep -F 'dependency is workspace-only: example.invalid/dependency v0.0.0 via ../dependency' \
	"$temporary/publish.err" > /dev/null

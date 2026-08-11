#!/bin/sh
set -eu

file=${1:?benchmark transcript is required}
expected=${2:?expected sample count is required}

case "$expected" in
'' | *[!0-9]*)
	printf 'invalid benchmark sample count: %s\n' "$expected" >&2
	exit 1
	;;
esac
if [ "$expected" -le 0 ] || [ "$expected" -gt 100 ]; then
	printf 'benchmark sample count must be between 1 and 100: %s\n' "$expected" >&2
	exit 1
fi
if [ ! -f "$file" ]; then
	printf 'benchmark transcript is missing: %s\n' "$file" >&2
	exit 1
fi
if grep -E '(^|[[:space:]])--- FAIL:|^FAIL([[:space:]]|$)' "$file" >/dev/null; then
	printf 'benchmark transcript contains failed samples: %s\n' "$file" >&2
	exit 1
fi
if ! grep -x 'PASS' "$file" >/dev/null; then
	printf 'benchmark transcript has no terminal PASS: %s\n' "$file" >&2
	exit 1
fi

names='fake/indexing
fake/query
fake/bulk_indexing
fake/pagination
opensearch-adapter/indexing
opensearch-adapter/query
opensearch-adapter/bulk_indexing
opensearch-adapter/pagination
opensearch-adapter/cursor_pagination
direct-official-client/indexing
direct-official-client/query
direct-official-client/bulk_indexing
direct-official-client/pagination
direct-official-client/cursor_pagination'

printf '%s\n' "$names" | while IFS= read -r name; do
	count=$(awk -v prefix="BenchmarkSharedSearchSemantics/$name-" '
		index($1, prefix) == 1 { count++ }
		END { print count + 0 }
	' "$file")
	if [ "$count" -ne "$expected" ]; then
		printf 'benchmark %s has %s samples, want %s: %s\n' "$name" "$count" "$expected" "$file" >&2
		exit 1
	fi
done

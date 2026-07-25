#!/bin/sh
set -eu

required='README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md LICENSE NOTICE.md
docs/api.md docs/gs1.md docs/rendering-and-scanning.md
docs/error-correction.md docs/interoperability.md docs/security.md
docs/adoption.md docs/comparison.md docs/faq.md specification/README.md
specification/manifest.json specification/normative.tsv
specification/evidence.tsv specification/render-fixtures.tsv'

for path in $required; do
	test -s "$path" || {
		printf 'required documentation is missing or empty: %s\n' "$path" >&2
		exit 1
	}
done

normative_ids="$(mktemp)"
evidence_ids="$(mktemp)"
trap 'rm -f "$normative_ids" "$evidence_ids"' EXIT
tail -n +2 specification/normative.tsv | cut -f 1 | sort >"$normative_ids"
tail -n +2 specification/evidence.tsv | cut -f 1 | sort >"$evidence_ids"
if ! cmp -s "$normative_ids" "$evidence_ids"; then
	printf '%s\n' 'normative and evidence inventory IDs differ' >&2
	diff -u "$normative_ids" "$evidence_ids" >&2 || true
	exit 1
fi
if test "$(uniq -d "$normative_ids" | wc -l | tr -d ' ')" != 0; then
	printf '%s\n' 'normative inventory contains duplicate IDs' >&2
	exit 1
fi

go test . -run '^Example' -count=1
./scripts/check-render-fixtures.sh

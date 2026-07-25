#!/bin/sh

set -eu

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-corpus-test.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

for fixture in \
	testdata/coverage/advisory.yml \
	testdata/coverage/blocking.yml \
	testdata/coverage/go.mod \
	testdata/coverage/invalid.yml \
	testdata/coverage/sample/sample.go; do
	if ! git -C "$root" ls-files --error-unmatch "$fixture" >/dev/null 2>&1; then
		printf 'canonical corpus fixture is not tracked: %s\n' "$fixture" >&2
		exit 1
	fi
done

mkdir -p "$temporary/modules/service" "$temporary/modules/dependency" \
	"$temporary/policy/baselines"
printf 'module example.com/service\n\ngo 1.26.0\n\nrequire example.com/dependency v1.0.0\n' \
	> "$temporary/modules/service/go.mod"
printf 'module example.com/dependency\n\ngo 1.26.0\n' \
	> "$temporary/modules/dependency/go.mod"
printf 'version: 1\n' > "$temporary/policy/service.yml"
printf '[]\n' > "$temporary/policy/baselines/service.json"
printf '%s\n' '#!/bin/sh' \
	'[ -f "${GOWORK:-}" ] || exit 1' \
	'grep -F "example.com/dependency =>" "$GOWORK" >/dev/null || exit 1' \
	'grep -F "$EXPECTED_REPLACEMENT" "$GOWORK" >/dev/null || exit 1' \
	'printf "[]\\n"' > "$temporary/analysis"
chmod +x "$temporary/analysis"

tab=$(printf '\t')
manifest="$temporary/policy/manifest.tsv"
printf 'service%sservice%sservice.yml%sbaselines/service.json\n' \
	"$tab" "$tab" "$tab" > "$manifest"

CORPUS_MODULE_ROOT="$temporary/modules" \
	CORPUS_POLICY_ROOT="$temporary/policy" \
	CORPUS_BASELINE_ROOT="$temporary/policy" \
	CORPUS_BINARY="$temporary/analysis" \
	CORPUS_REPLACE_ROOT="$temporary/modules" \
	EXPECTED_REPLACEMENT="$temporary/modules/dependency" \
	"$root/scripts/corpus.sh" check "$manifest" > "$temporary/stdout"

grep -q '^corpus verified: 1 module(s)$' "$temporary/stdout"

printf 'corpus runner tests passed\n'

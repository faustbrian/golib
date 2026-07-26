#!/bin/sh

set -eu

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-performance-test.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

mkdir -p "$temporary/modules/service" "$temporary/policies"
printf 'version: 1\n' > "$temporary/policies/policy.yml"
printf '#!/bin/sh\nprintf "{}\\n"\n' > "$temporary/analysis"
chmod +x "$temporary/analysis"

tab=$(printf '\t')
manifest="$temporary/manifest.tsv"
report="$temporary/report.tsv"
printf 'service%sservice%spolicy.yml%s60000%s60000%s1048576\n' \
	"$tab" "$tab" "$tab" "$tab" "$tab" > "$manifest"

PERFORMANCE_BINARY="$temporary/analysis" \
	CORPUS_MODULE_ROOT="$temporary/modules" \
	CORPUS_POLICY_ROOT="$temporary/policies" \
	PERFORMANCE_REPORT="$report" \
	"$root/scripts/performance.sh" "$manifest" > "$temporary/stdout"

grep -q '^performance verified: 1 module(s)$' "$temporary/stdout"
grep -q '^name[[:space:]]cold_ms[[:space:]]warm_ms[[:space:]]peak_kib$' "$report"
awk -F "$tab" '
	NR == 2 {
		if ($1 != "service" || $2 !~ /^[0-9]+$/ ||
			$3 !~ /^[0-9]+$/ || $4 !~ /^[0-9]+$/ || $4 == 0) {
			exit 1
		}
		found = 1
	}
	END { if (!found) exit 1 }
' "$report"

printf 'service%s../escape%spolicy.yml%s1%s1%s1\n' \
	"$tab" "$tab" "$tab" "$tab" "$tab" > "$manifest"
if PERFORMANCE_BINARY="$temporary/analysis" \
	CORPUS_MODULE_ROOT="$temporary/modules" \
	CORPUS_POLICY_ROOT="$temporary/policies" \
	PERFORMANCE_REPORT="$report" \
	"$root/scripts/performance.sh" "$manifest" > /dev/null 2>&1; then
	printf 'performance runner accepted an escaping module path\n' >&2
	exit 1
fi

printf 'service%sservice%spolicy.yml%s0%s1%s1\n' \
	"$tab" "$tab" "$tab" "$tab" "$tab" > "$manifest"
if PERFORMANCE_BINARY="$temporary/analysis" \
	CORPUS_MODULE_ROOT="$temporary/modules" \
	CORPUS_POLICY_ROOT="$temporary/policies" \
	PERFORMANCE_REPORT="$report" \
	"$root/scripts/performance.sh" "$manifest" > /dev/null 2>&1; then
	printf 'performance runner accepted a zero budget\n' >&2
	exit 1
fi

printf 'performance runner tests passed\n'

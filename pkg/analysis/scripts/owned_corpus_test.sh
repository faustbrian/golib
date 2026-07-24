#!/bin/sh

set -eu

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-owned-corpus-test.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

modules="$temporary/modules"
evidence="$temporary/evidence"
mkdir -p "$modules/go-alpha" "$modules/go-beta" "$temporary/bin"
for repository in "$modules/go-alpha" "$modules/go-beta"; do
	printf 'module example.com/%s\n\ngo 1.26.0\n' "${repository##*/}" \
		> "$repository/go.mod"
	git -C "$repository" init -q
	git -C "$repository" add go.mod
	git -C "$repository" -c user.name=test -c user.email=test@example.com \
		commit -qm 'test: initialize fixture'
done
printf 'version: 1\n' > "$temporary/policy.yml"

runner="$temporary/bin/corpus"
printf '%s\n' '#!/bin/sh' \
	'set -eu' \
	'mode=$1' \
	'manifest=$2' \
	'printf "%s\n" "$mode" >> "$CALL_LOG"' \
	'test "$(grep -c "^[^#]" "$manifest")" -eq 2' \
	'if [ "$mode" = update ]; then' \
	'  while IFS="$(printf "\t")" read -r name module policy baseline; do' \
	'    case "$name" in ""|\#*) continue ;; esac' \
	'    mkdir -p "$(dirname "$CORPUS_BASELINE_ROOT/$baseline")"' \
	'    printf "[]\n" > "$CORPUS_BASELINE_ROOT/$baseline"' \
	'  done < "$manifest"' \
	'fi' \
	'if [ "${CHANGE_TARGET:-0}" = 1 ] && [ "$mode" = update ]; then' \
	'  printf "changed\n" >> "$CORPUS_MODULE_ROOT/go-alpha/go.mod"' \
	'fi' > "$runner"
chmod +x "$runner"

CALL_LOG="$temporary/calls" \
OWNED_CORPUS_ROOT="$modules" \
OWNED_CORPUS_POLICY="$temporary/policy.yml" \
OWNED_CORPUS_EVIDENCE_ROOT="$evidence" \
OWNED_CORPUS_RUNNER="$runner" \
OWNED_CORPUS_SOURCE=worktree \
	"$root/scripts/owned_corpus.sh" > "$temporary/stdout"

test "$(cat "$temporary/calls")" = "$(printf 'update\ncheck')"
test -f "$evidence/manifest.tsv"
test -f "$evidence/reports/go-alpha.json"
test -f "$evidence/reports/go-beta.json"
test -f "$evidence/performance.tsv"
grep -q '^name[[:space:]]cold_ms[[:space:]]warm_ms[[:space:]]peak_kib[[:space:]]max_cold_ms[[:space:]]max_warm_ms[[:space:]]max_peak_kib$' \
	"$evidence/performance.tsv"
awk -F "$(printf '\t')" '
	NR == 2 {
		if ($1 != "full-corpus" || $2 !~ /^[0-9]+$/ ||
			$3 !~ /^[0-9]+$/ || $4 !~ /^[1-9][0-9]*$/ ||
			$5 != 180000 || $6 != 180000 || $7 != 524288) {
			exit 1
		}
		found = 1
	}
	END { if (!found) exit 1 }
' "$evidence/performance.tsv"
grep -q '^owned corpus verified: 2 module(s)$' "$temporary/stdout"

if CALL_LOG="$temporary/budget-calls" \
	OWNED_CORPUS_ROOT="$modules" \
	OWNED_CORPUS_POLICY="$temporary/policy.yml" \
	OWNED_CORPUS_EVIDENCE_ROOT="$temporary/budget-evidence" \
	OWNED_CORPUS_RUNNER="$runner" \
	OWNED_CORPUS_SOURCE=worktree \
	OWNED_CORPUS_MAX_PEAK_KIB=1 \
	"$root/scripts/owned_corpus.sh" > /dev/null \
	2> "$temporary/budget-stderr"; then
	printf 'owned corpus accepted an exceeded performance budget\n' >&2
	exit 1
fi
grep -q '^owned corpus exceeded performance budget:' \
	"$temporary/budget-stderr"

if OWNED_CORPUS_ROOT="$modules" \
	OWNED_CORPUS_POLICY="$temporary/policy.yml" \
	OWNED_CORPUS_EVIDENCE_ROOT="$temporary/invalid-budget-evidence" \
	OWNED_CORPUS_RUNNER="$runner" \
	OWNED_CORPUS_SOURCE=worktree \
	OWNED_CORPUS_MAX_COLD_MS=0 \
	"$root/scripts/owned_corpus.sh" > /dev/null 2>&1; then
	printf 'owned corpus accepted a zero performance budget\n' >&2
	exit 1
fi

if CALL_LOG="$temporary/changed-calls" \
	CHANGE_TARGET=1 \
	OWNED_CORPUS_ROOT="$modules" \
	OWNED_CORPUS_POLICY="$temporary/policy.yml" \
	OWNED_CORPUS_EVIDENCE_ROOT="$temporary/changed-evidence" \
	OWNED_CORPUS_RUNNER="$runner" \
	OWNED_CORPUS_SOURCE=worktree \
	"$root/scripts/owned_corpus.sh" > "$temporary/changed-stdout" 2> "$temporary/stderr"; then
	printf 'owned corpus accepted a changing target repository\n' >&2
	exit 1
fi
grep -q 'owned corpus changed during analysis' "$temporary/stderr"
grep -q '^changed repository: go-alpha$' "$temporary/stderr"

CALL_LOG="$temporary/head-calls" \
OWNED_CORPUS_ROOT="$modules" \
OWNED_CORPUS_POLICY="$temporary/policy.yml" \
OWNED_CORPUS_EVIDENCE_ROOT="$temporary/head-evidence" \
OWNED_CORPUS_RUNNER="$runner" \
OWNED_CORPUS_SOURCE=head \
	"$root/scripts/owned_corpus.sh" > "$temporary/head-stdout"

test "$(cat "$temporary/head-calls")" = "$(printf 'update\ncheck')"
test "$(wc -l < "$temporary/head-evidence/revisions.tsv" | tr -d ' ')" -eq 2
grep -q '^owned corpus verified: 2 module(s)$' "$temporary/head-stdout"

printf 'owned corpus runner tests passed\n'

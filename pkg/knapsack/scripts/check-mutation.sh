#!/bin/sh
set -eu

command -v jq >/dev/null 2>&1 || {
	printf '%s\n' 'jq is required for mutation evidence validation' >&2
	exit 1
}

temporary="$(mktemp -d "${TMPDIR:-/tmp}/knapsack-mutation.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
gremlins='github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0'

go run "$gremlins" unleash . --integration --workers 2 \
	--exclude-files '^(objective/gomoney/|integration/references/)' \
	--output "$temporary/root.json" \
	--threshold-efficacy 100 --threshold-mcover 98 \
	>"$temporary/root.log" 2>&1
(
	cd objective/gomoney
	go run "$gremlins" unleash . --integration --workers 2 \
		--output "$temporary/gomoney.json" \
		--threshold-efficacy 100 --threshold-mcover 100 \
		>"$temporary/gomoney.log" 2>&1
)
(
	cd integration/references
	go run "$gremlins" unleash . --workers 2 \
		--output "$temporary/adapter.json" \
		--threshold-efficacy 100 --threshold-mcover 90 \
		>"$temporary/adapter.log" 2>&1
)

jq -e '.mutants_lived == 0 and .test_efficacy == 100 and .mutants_not_covered == 12' \
	"$temporary/root.json" >/dev/null
jq -e '.mutants_lived == 0 and .test_efficacy == 100 and .mutants_not_covered == 0 and .mutations_coverage == 100' \
	"$temporary/gomoney.json" >/dev/null
jq -e '.mutants_lived == 0 and .test_efficacy == 100 and .mutants_not_covered == 1 and .mutations_coverage == 90' \
	"$temporary/adapter.json" >/dev/null

{
	jq -r '.files[] | .file_name as $file | .mutations[] | select(.status == "NOT COVERED") | [$file, .type, .line, .column] | @tsv' \
		"$temporary/root.json"
	jq -r '.files[] | .file_name as $file | .mutations[] | select(.status == "NOT COVERED") | ["integration/references/" + $file, .type, .line, .column] | @tsv' \
		"$temporary/adapter.json"
} | sort >"$temporary/actual-classifications.tsv"
tail -n +2 specification/mutation-classifications.tsv | cut -f1-4 | sort \
	>"$temporary/expected-classifications.tsv"
diff -u "$temporary/expected-classifications.tsv" "$temporary/actual-classifications.tsv"

if test "${UPDATE_MUTATION_EVIDENCE:-0}" = 1; then
	cp "$temporary/root.json" docs/mutation/raw/root.json
	cp "$temporary/root.log" docs/mutation/raw/root.log
	cp "$temporary/gomoney.json" docs/mutation/raw/gomoney.json
	cp "$temporary/gomoney.log" docs/mutation/raw/gomoney.log
	cp "$temporary/adapter.json" docs/mutation/raw/adapter.json
	cp "$temporary/adapter.log" docs/mutation/raw/adapter.log
else
	for module in root gomoney adapter; do
		projection='{go_module, files: ([.files[] | {file_name, mutations: ([.mutations[] | .status = (if .status == "TIMED OUT" then "KILLED" else .status end)] | sort_by(.line, .column, .type, .status))}] | sort_by(.file_name))}'
		jq -S "$projection" "$temporary/$module.json" >"$temporary/$module.actual.json"
		jq -S "$projection" "docs/mutation/raw/$module.json" >"$temporary/$module.expected.json"
		diff -u "$temporary/$module.expected.json" "$temporary/$module.actual.json"
	done
fi

jq -r '([.files[].mutations[] | select(.status == "TIMED OUT")] | length) as $timeouts | "root mutation: killed=\(.mutants_killed) timed_out=\($timeouts) lived=\(.mutants_lived) classified=\(.mutants_not_covered) efficacy=\(.test_efficacy)%"' "$temporary/root.json"
jq -r '"gomoney mutation: killed=\(.mutants_killed) lived=\(.mutants_lived) coverage=\(.mutations_coverage)% efficacy=\(.test_efficacy)%"' "$temporary/gomoney.json"
jq -r '"adapter mutation: killed=\(.mutants_killed) lived=\(.mutants_lived) classified=\(.mutants_not_covered) coverage=\(.mutations_coverage)% efficacy=\(.test_efficacy)%"' "$temporary/adapter.json"

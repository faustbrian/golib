#!/bin/sh
set -eu

multiplier="${1:-1}"
budgets="${FUZZ_BUDGETS:-specification/fuzz-budgets.tsv}"

case "$multiplier" in
	''|*[!0-9]*)
		printf 'fuzz multiplier must be an integer\n' >&2
		exit 1
		;;
esac
test "$multiplier" -ge 1 && test "$multiplier" -le 100 || {
	printf 'fuzz multiplier must be between 1 and 100\n' >&2
	exit 1
}
test -f "$budgets" || {
	printf 'fuzz budget manifest is missing: %s\n' "$budgets" >&2
	exit 1
}

tab="$(printf '\t')"
line=0
targets=0
while IFS="$tab" read -r package target iterations extra; do
	line=$((line + 1))
	if test "$line" -eq 1; then
		test "$package" = "package" && test "$target" = "target" && \
			test "$iterations" = "iterations" && test -z "$extra" || {
			printf 'invalid fuzz budget header\n' >&2
			exit 1
		}
		continue
	fi
	test -n "$package" && test -n "$target" && test -z "$extra" || {
		printf 'invalid fuzz budget row %d\n' "$line" >&2
		exit 1
	}
	case "$iterations" in
		''|*[!0-9]*)
			printf 'invalid fuzz iterations on row %d\n' "$line" >&2
			exit 1
			;;
	esac
	runs=$((iterations * multiplier))
	GOWORK=off go test "$package" -run '^$' -fuzz "^${target}$" -fuzztime "${runs}x"
	targets=$((targets + 1))
done < "$budgets"

test "$targets" -gt 0 || {
	printf 'fuzz budget manifest contains no targets\n' >&2
	exit 1
}

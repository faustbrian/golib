#!/bin/sh
set -eu

validate_review() {
	manifest="$1"
	actual="$2"
	require_publishable="$3"
	temporary="$(mktemp -d)"
	trap 'rm -rf "$temporary"' EXIT
	expected_header='module	version	replacement	license_file	spdx	sha256	license_status	release_status'
	header="$(sed -n '1p' "$manifest")"
	test "$header" = "$(printf '%b' "$expected_header")" || {
		printf '%s\n' 'dependency review has an invalid schema' >&2
		return 1
	}

	tail -n +2 "$manifest" | cut -f 1-3 | sort > "$temporary/reviewed.tsv"
	sort -u "$actual" > "$temporary/actual.tsv"
	cmp -s "$temporary/reviewed.tsv" "$temporary/actual.tsv" || {
		printf '%s\n' 'dependency review does not exactly match compiled modules' >&2
		diff -u "$temporary/reviewed.tsv" "$temporary/actual.tsv" >&2 || true
		exit 1
	}

	tail -n +2 "$manifest" > "$temporary/entries.tsv"
	: > "$temporary/blockers"
	while IFS="$(printf '\t')" read -r module version replacement \
		license_file spdx checksum license_status release_status; do
		test -n "$module" || continue
		case "$license_status" in
		approved)
			if test "$license_file" = "-" || ! test -s "$license_file"; then
				printf 'dependency license is missing: %s\n' "$module" \
					>> "$temporary/blockers"
			else
				actual_checksum="$(shasum -a 256 "$license_file" | awk '{ print $1 }')"
				test "$checksum" = "$actual_checksum" || \
					printf 'dependency license hash changed: %s\n' "$module" \
						>> "$temporary/blockers"
			fi
			test "$spdx" != "UNKNOWN" && test "$spdx" != "-" || \
				printf 'dependency license is unclassified: %s\n' "$module" \
					>> "$temporary/blockers"
			;;
		missing_license)
			printf 'dependency license is missing: %s %s via %s\n' \
				"$module" "$version" "$replacement" >> "$temporary/blockers"
			;;
		*)
			printf 'invalid dependency review status for %s: %s\n' \
				"$module" "$license_status" >> "$temporary/blockers"
			;;
		esac

		case "$release_status" in
		ready)
			test "$replacement" = "-" || \
				printf 'release-ready dependency has replacement: %s\n' "$module" \
					>> "$temporary/blockers"
			test "$version" != "v0.0.0" || \
				printf 'release-ready dependency has placeholder version: %s\n' "$module" \
					>> "$temporary/blockers"
			;;
		workspace_only)
			test "$replacement" != "-" || \
				printf 'workspace-only dependency has no replacement: %s\n' "$module" \
					>> "$temporary/blockers"
			if test "$require_publishable" = "true"; then
				printf 'dependency is workspace-only: %s %s via %s\n' \
					"$module" "$version" "$replacement" >> "$temporary/blockers"
			fi
			;;
		*)
			printf 'invalid dependency release status for %s: %s\n' \
				"$module" "$release_status" >> "$temporary/blockers"
			;;
		esac
	done < "$temporary/entries.tsv"

	if test -s "$temporary/blockers"; then
		cat "$temporary/blockers" >&2
		return 1
	fi
}

if [ "${1:-}" = "--validate" ]; then
	test "$#" -eq 3
	validate_review "$2" "$3" false
	exit
fi
if [ "${1:-}" = "--validate-publish" ]; then
	test "$#" -eq 3
	validate_review "$2" "$3" true
	exit
fi

require_publishable=false
if [ "${1:-}" = "--publish" ]; then
	test "$#" -eq 1
	require_publishable=true
elif [ "$#" -ne 0 ]; then
	printf '%s\n' 'usage: check-dependency-review.sh [--publish]' >&2
	exit 2
fi

manifest="specification/dependency-licenses.tsv"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
root_module="$(go list -m -f '{{.Path}}')"
go list -deps -f '{{with .Module}}{{.Path}}{{"\t"}}{{.Version}}{{"\t"}}{{with .Replace}}{{.Path}}{{end}}{{end}}' \
	./... | awk -F '\t' -v root="$root_module" \
	'$1 != "" && $1 != root { replacement = $3; if (replacement == "") replacement = "-"; print $1 "\t" $2 "\t" replacement }' \
	> "$temporary/actual.tsv"
validate_review "$manifest" "$temporary/actual.tsv" "$require_publishable"

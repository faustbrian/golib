#!/bin/sh
set -eu

manifest="specification/render-fixtures.tsv"
directory="specification/render-fixtures"
expected="id\tformat\tpayload_class\tmodule_scale\tcorrection\tlogical_sha256\tpng_sha256\tsvg_sha256\tpng\tsvg"

test -s "$manifest" || {
	printf '%s\n' 'render fixture manifest is missing or empty' >&2
	exit 1
}
test -d "$directory" || {
	printf '%s\n' 'render fixture directory is missing' >&2
	exit 1
}
test "$(head -n 1 "$manifest")" = "$(printf '%b' "$expected")" || {
	printf '%s\n' 'render fixture manifest header is invalid' >&2
	exit 1
}

rows=0
{
	IFS= read -r _header
	while IFS="$(printf '\t')" read -r id format payload_class module_scale correction logical_sha png_sha svg_sha png svg; do
		rows=$((rows + 1))
		test -n "$id" && test -n "$format" && test -n "$payload_class" &&
			test -n "$correction" && test "$module_scale" -gt 0 2>/dev/null || {
			printf 'render fixture row %d has invalid metadata\n' "$((rows + 1))" >&2
			exit 1
		}
		for checksum in "$logical_sha" "$png_sha" "$svg_sha"; do
			test "${#checksum}" -eq 64 &&
				case "$checksum" in *[!0-9a-f]*) false ;; *) true ;; esac || {
				printf 'render fixture row %d has an invalid checksum\n' "$((rows + 1))" >&2
				exit 1
			}
		done
		test "$png" = "$id.png" && test "$svg" = "$id.svg" || {
			printf 'render fixture row %d has invalid asset names\n' "$((rows + 1))" >&2
			exit 1
		}
		for asset in "$png" "$svg"; do
			test -f "$directory/$asset" || {
				printf 'render fixture asset is missing: %s\n' "$asset" >&2
				exit 1
			}
		done
		test "$(shasum -a 256 "$directory/$png" | awk '{print $1}')" = "$png_sha" || {
			printf 'render fixture PNG checksum mismatch: %s\n' "$png" >&2
			exit 1
		}
		test "$(shasum -a 256 "$directory/$svg" | awk '{print $1}')" = "$svg_sha" || {
			printf 'render fixture SVG checksum mismatch: %s\n' "$svg" >&2
			exit 1
		}
	done
	test "$rows" -gt 0 || {
		printf '%s\n' 'render fixture manifest has no fixtures' >&2
		exit 1
	}
	files="$(find "$directory" -type f | wc -l | tr -d ' ')"
	test "$files" -eq "$((rows * 2))" || {
		printf 'render fixture file count is %s, want %s\n' "$files" "$((rows * 2))" >&2
		exit 1
	}
} < "$manifest"

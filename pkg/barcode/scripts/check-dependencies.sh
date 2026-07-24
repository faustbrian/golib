#!/bin/sh
set -eu

go mod verify

dependencies="$(mktemp)"
trap 'rm -f "$dependencies"' EXIT
go list -m -f '{{if and (not .Main) (not .Indirect)}}{{.Path}}|{{.Dir}}|{{with .Replace}}{{.Path}}{{end}}{{end}}' all >"$dependencies"

while IFS='|' read -r module directory replacement; do
	test -n "$module" || continue
	if test -n "$replacement"; then
		printf 'direct dependency uses an unreviewed replacement: %s -> %s\n' \
			"$module" "$replacement" >&2
		exit 1
	fi
	grep -Fq "\`$module\`" NOTICE.md || {
		printf 'direct dependency is missing from NOTICE.md: %s\n' "$module" >&2
		exit 1
	}
	find "$directory" -maxdepth 1 -type f \
		\( -iname 'license*' -o -iname 'copying*' \) -print -quit |
		grep -q . || {
			printf 'direct dependency has no module-root license file: %s\n' \
				"$module" >&2
			exit 1
		}
done <"$dependencies"

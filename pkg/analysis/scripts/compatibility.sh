#!/bin/sh

set -eu

mode=${1:-check}
case "$mode" in
	check|update) ;;
	*)
		printf 'usage: %s check|update\n' "$0" >&2
		exit 2
		;;
esac

root=$(CDPATH='' cd "$(dirname "$0")/.." && pwd)
baseline_dir=${COMPAT_BASELINE_DIR:-"$root/compat"}
temporary=$(mktemp -d "${TMPDIR:-/tmp}/analysis-compat.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM

cd "$root"

packages=$(go list ./analysis ./policy ./analysistestkit ./analyzers/... | LC_ALL=C sort)
first=true
for package in $packages; do
	if [ "$first" = false ]; then
		printf '\n' >> "$temporary/public-api.txt"
	fi
	first=false
	printf '## %s\n\n' "$package" >> "$temporary/public-api.txt"
	go doc -all "$package" >> "$temporary/public-api.txt"
done
sed '$d' "$temporary/public-api.txt" > "$temporary/public-api.trimmed"
mv "$temporary/public-api.trimmed" "$temporary/public-api.txt"

go run ./cmd/golib-analysis rules > "$temporary/rules.json"

if [ "$mode" = update ]; then
	mkdir -p "$baseline_dir"
	cp "$temporary/public-api.txt" "$baseline_dir/public-api.txt"
	cp "$temporary/rules.json" "$baseline_dir/rules.json"
	exit 0
fi

for artifact in public-api.txt rules.json; do
	if [ ! -f "$baseline_dir/$artifact" ]; then
		printf 'compatibility baseline missing: compat/%s\n' "$artifact" >&2
		printf 'run make compatibility-update after reviewing the public contract\n' >&2
		exit 1
	fi
	if [ "$artifact" = rules.json ]; then
		if ! cmp -s "$baseline_dir/$artifact" "$temporary/$artifact"; then
			printf 'rule inventory differs from compat/rules.json\n' >&2
			printf 'run make compatibility-update and review the rule metadata change\n' >&2
			exit 1
		fi
		continue
	fi
	diff -u "$baseline_dir/$artifact" "$temporary/$artifact"
done
